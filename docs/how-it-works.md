# Como o AI Gateway funciona

Este documento descreve a arquitetura interna, o fluxo de uma requisição e o papel de cada pacote Go.

---

## Mapa de pacotes

```
cmd/gateway/
└── main.go               ← composição de dependências + bootstrap + HTTP server

internal/
├── config/               ← carrega e valida configs/gateway.yaml
├── observability/        ← factory do slog.Logger + helpers de contexto
│
├── auth/                 ← autenticação Bearer + políticas por aplicação
│   ├── policy.go         ← AppPolicy, PolicyStore (interface), NewPolicyStore
│   └── hash.go           ← ExtractPrefix (prefixo do token para lookup O(1))
│
├── providers/            ← abstração de LLM
│   ├── provider.go       ← interface Provider + tipos OpenAI-compatíveis
│   ├── azureopenai/      ← cliente HTTP Azure OpenAI (non-stream + SSE)
│   └── mock/             ← provider determinístico para dev/testes
│
├── security/
│   ├── masking/          ← detecção e redação de PII/PCI
│   │   ├── luhn.go       ← algoritmo Luhn (validação de cartão)
│   │   ├── detectors.go  ← CPF, CNPJ, cartão+Luhn, e-mail, telefone, CEP
│   │   └── masker.go     ← orquestrador com resolução de sobreposições
│   ├── promptshield/     ← detecção de injeção de prompt
│   │   ├── client.go     ← Azure Content Safety (Prompt Shield + Text Analyze)
│   │   └── local.go      ← heurística de keywords (fallback sem Azure CS)
│   └── postvalidation/   ← verificação de saída Tier 3
│
├── tiers/
│   └── engine.go         ← Pipeline struct + PipelineFor(tier) → quais guardrails ativar
│
├── ratelimit/
│   └── limiter.go        ← token bucket por app (golang.org/x/time/rate)
│
├── budget/
│   ├── precheck.go       ← query síncrona em budget_counters (500 ms timeout, fail-open)
│   └── counter.go        ← UPSERT assíncrono via canal
│
├── usage/                ← UsageEvent + writer assíncrono → usage_events
├── audit/                ← AuditEvent + writer assíncrono → audit_events
│
├── db/
│   ├── pool.go           ← pgxpool com ping de validação no boot
│   └── migrate.go        ← golang-migrate (up idempotente no boot)
│
└── api/
    ├── router.go         ← monta chi.Mux com cadeia de middleware
    ├── middleware/
    │   ├── requestid.go  ← gera UUID, injeta em ctx e header X-Request-Id
    │   ├── logging.go    ← log de request_started / request_completed + responseRecorder
    │   ├── auth.go       ← valida Bearer, injeta AppPolicy em ctx
    │   ├── ratelimit.go  ← consulta limiter, retorna 429 se negado
    │   └── recover.go    ← captura panic, loga stack trace, retorna 500
    └── handlers/
        ├── health.go     ← GET /healthz e GET /readyz
        ├── models.go     ← GET /v1/models
        └── chat.go       ← POST /v1/chat/completions (fluxo completo)
```

---

## Cadeia de middleware (do mais externo para o mais interno)

```
Request
  │
  ▼ Recover        ← captura panic em qualquer handler downstream
  ▼ RequestID      ← injeta UUID no ctx e no header X-Request-Id
  ▼ Logging        ← loga request_started; responseRecorder captura status_code
  ▼ Auth           ← valida Bearer token; injeta AppPolicy no ctx
  ▼ RateLimit      ← verifica token bucket por aplicação
  ▼ Handler        ← lógica de negócio
```

A cadeia é montada em `internal/api/router.go`. `/healthz` e `/readyz` ficam **fora** do grupo autenticado (não passam por Auth/RateLimit).

---

## Fluxo de uma requisição POST /v1/chat/completions

### Etapas (non-streaming)

```
1.  RequestID middleware   → UUID gerado, ctx anotado
2.  Logging middleware     → request_started logado
3.  Auth middleware        →
      a. Extrai Bearer token do header Authorization
      b. ExtractPrefix("gwk_med_abc123") → "gwk_med"
      c. PolicyStore.Lookup("gwk_med") → AppPolicy
      d. SHA-256(token) vs KeyHash via subtle.ConstantTimeCompare
      e. Injeta AppPolicy no ctx; falha → 401 + audit auth_failed
4.  RateLimit middleware   → limiter.Allow(app.Name); falha → 429 + audit rate_limited
5.  Handler Chat:
      a. MaxBytesReader(1 MiB) + json.Decode → ChatCompletionRequest
      b. Validação: model ∈ AllowedModels?  → 403 se não
      c. req.Stream && !StreamingAllowed?   → 403 se violado
6.  Pipeline (PipelineFor(policy.Tier)):
      Tier 1: masking CPF+cartão
      Tier 2: masking completo + detecção local de injeção
      Tier 3: masking + injeção + Azure Prompt Shield + Azure Content Safety
      → cada bloqueio emite audit event e retorna 403
7.  Budget precheck        → SELECT estimated_cost_brl WHERE app+período
                            → 429 se >= monthly_budget_brl
8.  Provider.ChatCompletions(ctx, req, deployment)
      → azureopenai.Client faz POST para Azure com header api-key
      → falha → 502 + audit provider_error
9.  Post-validation        → Tier 3: verifica saída com heurística local
10. Emit UsageEvent        → canal assíncrono → INSERT usage_events
11. Emit BudgetUpdate      → canal assíncrono → UPSERT budget_counters
12. json.Encode(resp)      → 200 OK
13. Logging middleware     → request_completed com latency_ms + status_code
```

### Diferença no streaming (SSE)

A partir da etapa 8, o handler:
1. Verifica que `w` implementa `http.Flusher` (necessário para SSE)
2. Seta headers: `Content-Type: text/event-stream`, `Cache-Control: no-cache`, `X-Accel-Buffering: no`
3. Chama `provider.StreamChatCompletions` → recebe `<-chan StreamChunk`
4. Para cada chunk: escreve `data: <json>\n\n` + `flusher.Flush()`
5. Em `ctx.Done()` (cliente desconectou): emite `stream_cancelled` + retorna
6. Após `[DONE]`: coleta usage do último chunk (se `include_usage=true`)

---

## Tiers de segurança

| Tier | Masking | Injeção local | Prompt Shield (Azure CS) | Content Safety (Azure CS) | Post-validação | Fail-mode |
|---|---|---|---|---|---|---|
| tier_1 | CPF + cartão | ✗ | ✗ | ✗ | ✗ | open |
| tier_2 | Completo | ✓ | ✗ | ✗ | ✗ | open |
| tier_3 | Completo | ✓ | ✓ | ✓ | ✓ | closed |

**Fail-open**: Azure CS indisponível → warn log + request continua.
**Fail-closed**: Azure CS indisponível → 503 + audit + request bloqueada.

---

## Persistência assíncrona

Três workers rodam como goroutines em background a partir do boot:

```
Handler
  │ Emit() ← non-blocking (select/default)
  ▼
chan (buffer 10.000) ─→ worker goroutine ─→ INSERT/UPSERT no PostgreSQL
```

Se o canal encher (> 10.000 eventos em voo), o evento é **descartado** com `warn` log (`event_type=usage_dropped`). Isso protege a latência do request mas pode resultar em lacunas sob carga extrema.

O shutdown graceful cancela o contexto dos workers, que então **drenam o canal** antes de sair (loop `select` com `default`).

---

## Autenticação: como o hash funciona

```
Token original: "gwk_leve_meutokendeteste123"
        │
        ├─ ExtractPrefix → "gwk_leve"  (lookup no PolicyStore)
        │
        └─ sha256.Sum256(token) → [32]byte
                │
                └─ subtle.ConstantTimeCompare(sum[:], hex.Decode(policy.KeyHash))
                   → 1 = ok, 0 = 401
```

O token nunca é logado. Apenas o `key_prefix` pode aparecer em logs.

---

## Fluxo de dados no PostgreSQL

```
usage_events    ← uma linha por request concluído
audit_events    ← uma ou mais linhas por request (cada decisão de política)
budget_counters ← uma linha por (app, YYYYMM); UPSERT acumulativo
```

As migrations rodam automaticamente no boot (`db.RunMigrations`) e são idempotentes (golang-migrate rastreia versão aplicada).
