# ADR-0024: Usage tracking no proxy plane

- **Status**: proposed
- **Date**: 2026-05-28
- **Decision makers**: Daniel (owner)
- **Consulted**: Claude Opus 4.7
- **Supersedes**: nenhum
- **Roadmap**: Observabilidade nativa (§3.5)

## Context

O proxy plane (`/v1/proxy/{slug}/*`, ADR-0010) é por design um passthrough
genérico — não conhece os conceitos de "tokens", "modelo" ou "custo BRL".
Hoje **só o handler legacy `/v1/chat/completions`** (`internal/api/handlers/chat.go`)
emite `usage_events`.

O Playground (`/ui/playground`) usa o proxy plane: chama
`/v1/proxy/{slug}/chat/completions` com bearer da aplicação. Como o proxy não
emite usage, **nenhuma chamada feita pelo Playground aparece nos dashboards
nem em Observability > Uso**. Operador percebe isso como bug — "playground
não retorna nos dashboards".

A mesma lacuna afeta qualquer consumidor real que use o proxy plane (a
filosofia v2 é que toda integração nova passe por `/v1/proxy/{slug}/*`, e
`/v1/chat/completions` é legacy). Sem usage tracking no proxy, dashboards
de custo / latência / throughput por aplicação são inúteis pra cargas reais.

Restrições e drivers:

- Proxy precisa permanecer **genérico** — não pode forçar todo
  `provider_kind` a falar OpenAI schema.
- `provider_kind` é metadata do endpoint (ADR-0016, ADR-0017): o gateway
  sabe que esse endpoint é Azure OpenAI ou OpenAI ou Anthropic etc.
- Schema OpenAI/Azure OpenAI é praticamente universal entre os providers IA
  do mercado: `{ "model": "...", "usage": { "prompt_tokens": N,
  "completion_tokens": M, "total_tokens": N+M }, ... }`.
- **Streaming SSE** quebra a estratégia de ler response body inteiro em
  `ModifyResponse`: o handler precisaria interceptar chunks `data: {...}`
  e somar tokens reportados no `delta` (Azure stream emite `usage` no
  chunk final só quando `stream_options.include_usage = true`).
- Latência: adicionar deserialização JSON + emit async no caminho de
  resposta custa ~1-2ms — aceitável dado o ADR-0021 (latency breakdown
  inclui bucket `encode`).
- `usage.Writer` é assíncrono (channel buffer 10k — ADR-0005), então o
  emit não bloqueia o response.

## Decision

**Adicionar `usage.Emitter` como dependency do `proxy.Handler`** e emitir
um `UsageEvent` por request, com payload completo quando o `provider_kind`
é IA-aware e o response é JSON parseável.

Caminho de emit por classe de request:

| Classe | provider_kind | Content-Type response | Status | UsageEvent emitido |
|---|---|---|---|---|
| IA não-stream OK | `azure_openai` / `openai` | `application/json` | 2xx | Completo: tokens parseados do body, model do request body, cost calculado |
| IA não-stream erro | `azure_openai` / `openai` | qualquer | ≥ 400 | Minimal: tokens=0, cost=0, model do request body se parseável, status real |
| IA streaming | `azure_openai` / `openai` | `text/event-stream` | 2xx | **Skip em V1** — fica como ADR follow-up; status=200, tokens=0, cost=0, body="stream", anotação `LatProviderMs` mantém latency até primeiro byte |
| Provider não-IA | `custom`, `anthropic`*, etc | qualquer | qualquer | Minimal: model="" (não há campo conhecido), tokens=0, cost=0 |
| Erro de rede (upstream caiu) | qualquer | n/a | n/a (ErrorHandler) | Status=502, tokens=0, cost=0, model do request body se parseável |

\* `anthropic`, `gemini`, `cohere`, etc.: têm schemas próprios; **V2** desta
frente abre adapters por provider. V1 cobre só schema OpenAI-style
(Azure OpenAI + OpenAI), que cobre o caso do Playground hoje.

Extração de campos:

- **`model`**: lido do `request.body.model` (campo padrão do schema OpenAI).
  Lido **uma vez** no Handler antes de invocar o ReverseProxy, e o slice
  de bytes é compartilhado com o `applyTranslator` existente (evita ler
  body duas vezes).
- **Tokens**: lidos do `response.body.usage.{prompt,completion,total}_tokens`.
- **Cost**: lookup no catálogo de modelos (`config.ModelConfig` indexado por
  `PublicName`) usando o `model` extraído do request. Quando não há match
  (modelo não está no catálogo), `EstimatedCostBRL = 0`.

`UsageEvent.Provider`: copiado de `config.ModelConfig.Provider` quando
houve match no catálogo; caso contrário, derivado do `provider_kind` do
endpoint (`azure_openai` → `"azure"`, `openai` → `"openai"`, etc.).

`LatencyMs` no proxy: medido do início do handler até `ModifyResponse`.
Os 5 buckets de breakdown (`LatAuthMs`, etc.) não se aplicam ao proxy
plane no mesmo modelo do handler legacy (a pipeline é diferente).
**V1 deixa todos os Lat\*Ms = 0** (NULL no DB); follow-up pode instrumentar
buckets equivalentes (auth, target_select, upstream, encode).

## Options considered

### Option 1: Status quo (proxy não emite usage)

Mantém o desenho atual. Playground continua invisível.

- **Pros:** zero código novo.
- **Cons:** dashboards inúteis pra qualquer carga que use o proxy plane
  (que é a filosofia v2).
- **Why not:** o problema motivador permanece.

### Option 2: Cliente envia metadata via headers

`X-Gateway-Tokens-Used: N` injetado pelo cliente do Playground, e
genericamente por qualquer integração.

- **Pros:** zero parse no servidor.
- **Cons:** cliente raramente sabe os tokens (vem do provider); shifts
  responsabilidade pra cada integração; trivialmente forjável; não cobre
  o Playground (UI não tem tokens a priori).
- **Why not:** transfere ônus pro consumidor e é inseguro.

### Option 3: Audit-only para o proxy

Cada request proxy emite só `audit_events` com `request_completed`. Dashboards
contam requests via audit em vez de usage.

- **Pros:** já existe no proxy hoje (de forma incompleta).
- **Cons:** não captura tokens nem custo — dashboards de custo continuam
  vazios. Mistura semântica de duas tabelas.
- **Why not:** custo é métrica P1 nos dashboards (§3.5); audit não cobre.

### Option 4: Emit usage no proxy plane com parse do body (CHOSEN)

Conforme descrito em "Decision".

- **Pros:** dashboards passam a refletir realidade do proxy. Reaproveita
  schema OpenAI-style que cobre Azure/OpenAI nativamente. Mantém o proxy
  genérico (skip silencioso pra providers não-IA). Latência marginal.
- **Cons:** adiciona deserialização no caminho de resposta. Streaming SSE
  fica como follow-up. Anthropic/Gemini/etc. caem em "minimal event" até
  V2 abrir adapters.
- **Why chosen:** entrega o que o owner pediu (Playground nos dashboards)
  com escopo enxuto, mantendo a porta aberta pra streaming e providers
  alternativos sem precisar reescrever a base.

### Option 5: Provider inspector interface

Generalizar `applyTranslator` (ADR-0017) pra também ter um `inspectResponse`
que retorna `(tokens, model, cost)` por provider.

- **Pros:** caminho de escala correto a médio prazo.
- **Cons:** escopo grande pra V1; precisa desenhar interface com cuidado
  pra suportar stream + non-stream + schemas diferentes; ADR próprio.
- **Why not in V1:** decisão pragmática — implementar inline na V1 e abrir
  ADR de generalização quando virar incômodo (provavelmente quando o
  segundo provider não-OpenAI virar P1).

## Consequences

### Positive

- **Playground aparece nos dashboards** — problema motivador resolvido.
- **Dashboards de custo / latência / throughput passam a refletir
  carga real** quando consumidores usam o proxy plane.
- **Compatibilidade backward**: rotas existentes (chat legacy) continuam
  funcionando exatamente como antes.
- **Zero acoplamento entre proxy genérico e provider-específico**:
  providers não-IA passam ilesos com event minimal; isso isola a feature
  ao subset onde faz sentido.
- **Catálogo de cost reaproveitado**: a mesma `config.Models` que
  alimenta o chat legacy é consumida aqui, mantendo consistência.

### Negative / Trade-offs

- **Streaming SSE não coberto na V1** — Playground não usa stream, mas
  consumidores reais usam. Follow-up necessário.
- **Anthropic/Gemini/etc. em "minimal mode"** — emit existe (request
  count + latência) mas tokens=0 / cost=0 até adapters serem escritos.
- **Custo de parse no caminho de resposta**: ~1-2ms a mais por request
  IA-aware. Aceitável (já há custo similar no chat legacy).
- **Acoplamento implícito ao schema OpenAI** no proxy — schema é estável
  (Azure e OpenAI mantêm compat) mas vale registrar como "assumption":
  se Azure renomear o objeto `usage` no body, quebramos silenciosamente
  (event vira "minimal"). Mitigação: testes table-driven com payloads
  conhecidos.

### Mitigations

- **Testes unit do extractor**: payload OpenAI completo, payload com
  apenas `usage` parcial, payload sem `usage`, payload com erro, payload
  inválido (JSON malformado). Cada caso retorna o `UsageEvent` esperado.
- **Body size cap**: respeitar `maxProxyBodyBytes = 1 MiB` (já existe pra
  request). Respostas maiores caem em "minimal event" — não vale o
  custo de memória.
- **Skip explícito de SSE**: detectar `Content-Type: text/event-stream`
  antes de tentar ReadAll, emitir minimal event com nota `event_type =
  proxy_stream_unmeasured` no log (não no usage; é diagnóstico).
- **Log estruturado de falhas de parse**: quando o body é JSON mas falta
  `usage`, log `warn event_type=proxy_usage_extraction_failed` com
  `slug` e `model` — operador pode adicionar provider ao catálogo se
  precisar de cost.

## Implementation sketch

### Mudanças em `internal/proxy/handler.go`

```go
// Handler ganha duas deps novas (V1):
//
//   - usage.Emitter — escrita assíncrona em usage_events
//   - models map[string]config.ModelConfig — lookup de cost por public_name
//
// A interface fica:
func Handler(
    svc *proxyservice.Service,
    resolver proxyservice.CredentialResolver,
    transport http.RoundTripper,
    usageEmitter usage.Emitter,
    models map[string]config.ModelConfig,
    logger *slog.Logger,
) http.Handler { ... }
```

### Helper `extractModelFromBody`

```go
// extractModelFromBody parses the canonical OpenAI request body and returns
// the "model" field. Returns "" when body is not JSON or doesn't have a
// "model" key. Tolerates extra fields and unknown structure.
func extractModelFromBody(body []byte) string { ... }
```

### Helper `extractUsageFromResponse`

```go
type extractedUsage struct {
    InputTokens  int
    OutputTokens int
    TotalTokens  int
    ParsedModel  string // some Azure responses include the resolved deployment
}

// extractUsageFromResponse parses an Azure OpenAI / OpenAI response body
// and returns the usage block. Returns empty struct when usage is absent
// or body isn't valid JSON. Errors are logged but not fatal — the caller
// emits a minimal event instead.
func extractUsageFromResponse(body []byte) (extractedUsage, bool) { ... }
```

### `ModifyResponse` callback

```go
ModifyResponse: func(resp *http.Response) error {
    defer fireEnd()

    latencyMs := int(time.Since(start).Milliseconds())
    event := usage.UsageEvent{
        RequestID:       requestIDFrom(r.Context()),
        ApplicationName: app.Name,
        Tier:            string(app.Tier),
        Model:           requestModel, // may be ""
        Provider:        providerFromKind(res.Endpoint.ProviderKind),
        LatencyMs:       latencyMs,
        StatusCode:      resp.StatusCode,
        CreatedAt:       time.Now().UTC(),
    }

    if resp.StatusCode >= 200 && resp.StatusCode < 300 &&
       isIASchemaProvider(res.Endpoint.ProviderKind) &&
       !isStreamResponse(resp) {
        body, err := readCappedBody(resp)
        if err == nil {
            if u, ok := extractUsageFromResponse(body); ok {
                event.InputTokens = u.InputTokens
                event.OutputTokens = u.OutputTokens
                event.TotalTokens = u.TotalTokens
                if mc, ok := models[requestModel]; ok {
                    event.EstimatedCostBRL = costBRL(mc, u.InputTokens, u.OutputTokens)
                }
            }
        }
    }

    usageEmitter.Emit(event)
    return nil
},
```

### Wiring em `cmd/gateway/main.go`

```go
// Build the model lookup map from existing config:
modelByName := make(map[string]config.ModelConfig, len(cfg.Models))
for _, m := range cfg.Models {
    modelByName[m.PublicName] = m
}
proxyHandler := proxy.Handler(
    proxySvc, credResolver, proxyTransport,
    usageWriter, modelByName,
    logger,
)
```

`usageWriter` já existe no main — é o mesmo `*usage.Writer` injetado no
handler legacy.

## Open questions

1. **`Provider` no UsageEvent quando provider_kind = `azure_openai`** — usar
   `"azure"` (consistente com chat legacy) ou `"azure_openai"` (mais
   explícito)? **Decisão V1**: `"azure"` pra alinhamento com legacy;
   future refactor pode unificar a nomenclatura.
2. **Streaming SSE follow-up** quando virar prioridade — ADR-0025
   provavelmente, com interceptor de chunks. Há complexidade extra com
   Azure stream (`stream_options.include_usage`).
3. **Adapters Anthropic/Gemini** — ADR dedicado quando o caso virar
   relevante na prática. Schema dos dois é razoavelmente diferente do
   OpenAI (Anthropic: `input_tokens`/`output_tokens` em vez de
   `prompt_tokens`/`completion_tokens`; Gemini: `usageMetadata`).
4. **Buckets de latency breakdown do proxy** (`LatAuthMs`, etc.). Hoje
   ficam todos NULL. Vale repensar quais buckets fazem sentido pro
   proxy: `auth_resolve`, `target_select`, `upstream`, `encode`? Follow-up.

## References

- ADR-0005 — Usage/audit assíncrono via channel (writer)
- ADR-0010 — Generic HTTP proxy engine
- ADR-0016 — Provider catalog
- ADR-0017 — Path translation per provider_kind
- ADR-0021 — Latency breakdown observável (modelo de buckets que pode ser
  estendido ao proxy em follow-up)
- `internal/api/handlers/chat.go:401` — emit de UsageEvent no handler legacy
- `internal/usage/event.go` — struct UsageEvent
- `internal/config/config.go` — ModelConfig
- Azure OpenAI Chat Completions response schema:
  https://learn.microsoft.com/en-us/azure/ai-services/openai/reference#chat-completions
- OpenAI Chat Completions usage object:
  https://platform.openai.com/docs/api-reference/chat/object#chat/object-usage
