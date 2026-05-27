# Como o AI Gateway funciona

Este documento descreve a arquitetura interna, o fluxo de uma requisição e o papel de cada pacote Go.

> **Leitura relacionada:** [Suite de testes](testing.md) · [Roadmap Phase 2](roadmap.md) · [Desenvolvimento local](local-development.md)

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
│   ├── pool.go           ← `*sql.DB` + microsoft/go-mssqldb, ping de validação no boot (ADR-0022)
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
chan (buffer 10.000) ─→ worker goroutine ─→ INSERT/MERGE em gogateway.* no SQL Server
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

## Fluxo de dados no SQL Server

Schema dedicado `gogateway` (ADR-0022) isola as tabelas do gateway das outras
aplicações que vivem no mesmo banco `AzureAI_Gateway_hom`.

```
gogateway.usage_events    ← uma linha por request concluído
gogateway.audit_events    ← uma ou mais linhas por request (cada decisão de política)
gogateway.budget_counters ← uma linha por (app, YYYYMM); MERGE acumulativo
gogateway.applications    ← config de cada consumidor (DB-backed via Admin API)
gogateway.api_keys        ← key_prefix + key_hash; partial UNIQUE em key_prefix WHERE rotated_at IS NULL
gogateway.proxy_endpoints ← endpoints HTTP do proxy plane
gogateway.proxy_targets   ← targets de cada endpoint (auth_config_enc cifrado AES-256-GCM)
gogateway.application_endpoint_grants ← ACL aplicação↔endpoint
gogateway.admin_users     ← admins do console (bcrypt)
gogateway.admin_sessions  ← sessões opacas com filtered index em token_hash WHERE revoked_at IS NULL
```

As migrations rodam automaticamente no boot (`db.RunMigrations`) e são
idempotentes (`golang-migrate` rastreia versão aplicada na tabela
`dbo.schema_migrations` — fica fora do schema gogateway por design,
preservando a separação entre dados do gateway e metadados de deployment).

Sintaxe T-SQL relevante usada nos repos:

- Param binding nomeado posicional: `@p1, @p2, ...`
- `INSERT ... OUTPUT INSERTED.id` substitui o `RETURNING` do PG
- `IF NOT EXISTS (SELECT 1 ...) INSERT ...` substitui `ON CONFLICT DO NOTHING`
- `MERGE WITH (HOLDLOCK)` substitui `INSERT ... ON CONFLICT DO UPDATE` (budget upsert)
- `SYSUTCDATETIME()` substitui `NOW()`
- `errors.Is(err, sql.ErrNoRows)` substitui `pgx.ErrNoRows`

---

## Latência observável por bucket (ADR-0021)

Toda chamada bem-sucedida a `/v1/chat/completions` retorna o header
`X-Gateway-Latency-Breakdown` com a decomposição da latência total em 5
buckets, e persiste os mesmos valores em colunas dedicadas de
`usage_events`:

```
X-Gateway-Latency-Breakdown: auth=2;mask=180;guardrails=0;provider=2400;encode=3
```

| Bucket | O que mede |
|---|---|
| `auth` | Decode do body, validação de modelo permitido, leitura da policy do contexto |
| `mask` | Regex local + Azure AI Language PII (cloud, Tier 2/3) |
| `guardrails` | Local injection scan + Azure Prompt Shield + Content Safety |
| `provider` | Marshal + HTTP roundtrip + unmarshal da chamada Azure OpenAI |
| `encode` | JSON marshal da response + Write (não-stream); merged em provider no stream |

"Other" = `latency_ms - sum(buckets)`. Costuma ficar em 1-5ms (middleware
chi + audit emit assíncrono). Se virar >50ms, é sinal pra investigar.

### Querying

```sql
-- p50/p95/p99 de cada bucket nas últimas 24h por aplicação
SELECT
  application_name,
  percentile_cont(0.50) WITHIN GROUP (ORDER BY lat_provider_ms) AS p50_provider,
  percentile_cont(0.95) WITHIN GROUP (ORDER BY lat_provider_ms) AS p95_provider,
  percentile_cont(0.50) WITHIN GROUP (ORDER BY lat_mask_ms)     AS p50_mask,
  percentile_cont(0.95) WITHIN GROUP (ORDER BY lat_mask_ms)     AS p95_mask
FROM usage_events
WHERE created_at >= NOW() - INTERVAL '24 hours'
  AND lat_provider_ms IS NOT NULL
GROUP BY application_name;

-- onde está o gargalo da minha latência (top 100 requests mais lentos)
SELECT request_id, application_name, latency_ms,
       lat_auth_ms, lat_mask_ms, lat_guardrails_ms, lat_provider_ms, lat_encode_ms,
       latency_ms - COALESCE(lat_auth_ms,0) - COALESCE(lat_mask_ms,0)
                  - COALESCE(lat_guardrails_ms,0) - COALESCE(lat_provider_ms,0)
                  - COALESCE(lat_encode_ms,0) AS other_ms
FROM usage_events
ORDER BY latency_ms DESC
LIMIT 100;
```

### Limitação no streaming

Em SSE, o header `X-Gateway-Latency-Breakdown` é emitido **antes** do
primeiro chunk e por isso traz `provider=0` e `encode=0`. Os valores
finais ainda são persistidos corretamente em `usage_events` ao final do
stream. Pra inspecionar latência de streaming, use as queries SQL acima.

---

## Detecção de PII em duas camadas (ADR-0019)

Tier 2 e Tier 3 rodam dois detectores **em sequência**, no body já mascarado:

```
prompt original
   │
   ▼
RunLocalMasking (regex)               ← sub-ms, sempre
   │  CPF/CNPJ (mod-11), cartão (Luhn),
   │  e-mail, telefone BR, CEP
   ▼
body parcialmente mascarado
   │
   ▼
RunRemotePII (Azure AI Language)      ← ~150-250ms p50, só Tier 2+
   │  Person, Address, DateTime, Email,
   │  PhoneNumber, IPAddress, BRCPFNumber,
   │  BRLegalEntityNumber, CreditCard,
   │  +20 outras categorias
   ▼
body totalmente mascarado → provider
```

### Por que sequencial e não paralelo

Se rodassem em paralelo, o Language veria o texto **original** — incluindo
CPF/cartão que o regex já ia mascarar. A cobrança e a latência ficam iguais,
mas o resultado do Language traz duplicação que precisa ser merged. Rodando
sequencial, o Language só processa o que regex perdeu: nomes próprios,
endereços completos, datas em texto livre. Menos ruído, mais sinal.

### Fail-open vs fail-closed

| Tier | RunRemotePII | Comportamento em erro do Language |
|---|---|---|
| Tier 1 | não | (não chama) |
| Tier 2 | sim | fail-open: emite `pii_remote_unavailable` warn no audit, segue request |
| Tier 3 | sim | fail-closed: emite `pii_remote_unavailable` error no audit, 503 ao cliente |

Quando `azure_language` está ausente do YAML, o `LanguageClient` é nil e a
etapa é skipped silenciosamente — mesmo pra Tier 2/3. Útil pra dev local
sem chave Azure ou pra ambientes que não querem custo cloud.

### Placeholder format

Em vez do `redactedText` que o Azure devolve (asteriscos), o cliente
reconstrói o texto a partir do array `entities` substituindo cada span por
`[CATEGORY_REDACTED]`. Mantém consistência com o regex local (que usa
`[BR_CPF_REDACTED]`, `[PCI_CARD_REDACTED]`).

Exemplo:
```
"Meu cliente João Silva mora em Belo Horizonte"
       ↓ Language detecta Person + Address
"Meu cliente [PERSON_REDACTED] mora em [ADDRESS_REDACTED]"
```

Offsets do Azure são pedidos como `UnicodeCodePoint` (== rune Go) pra
acertar palavras com `ã/ç/é` sem precisar de conversão UTF-16/UTF-8.

---

## Como a aplicação cliente chama o gateway

Existem dois planos paralelos, mantidos por compatibilidade:

### Plano Phase 1 — `/v1/chat/completions` (Azure OpenAI hard-coded)

Modelado em SPEC §6/§7. Para configs carregadas via YAML (`azure_openai` global).
Cliente envia body OpenAI-style; gateway monta o path Azure usando
`models[].deployment` do YAML.

```
POST /v1/chat/completions
Authorization: Bearer gwk_appbasico_...
Content-Type: application/json

{"model":"gpt-4.1-nano","messages":[...]}
```

### Plano v2 — `/v1/proxy/{slug}/*` (proxy genérico + path translation)

Modelado em ADR-0010 (motor genérico) + ADR-0016 (provider catalog) + ADR-0017
(path translation). Para endpoints cadastrados no DB pela UI admin, podendo
apontar para qualquer provider HTTP.

**Endpoints `custom`**: passthrough total. O path enviado pelo cliente é
encaminhado verbatim para o target.

**Endpoints com `provider_kind` que tem translator** (hoje só `azure_openai`):
o cliente fala OpenAI-style canônico (`/chat/completions`) e o gateway
traduz para o path nativo do upstream.

```
POST /v1/proxy/azure-foundry/chat/completions
Authorization: Bearer gwk_minhaapp_...
Content-Type: application/json

{"model":"gpt-4.1","messages":[...]}
```

Vira upstream:
```
POST https://danie-mc4ryviy-westeurope.cognitiveservices.azure.com/openai/deployments/gpt-4.1/chat/completions?api-version=2025-01-01-preview
api-key: <decrypted target auth>
```

A tradução acontece em `internal/proxy/translator/`:

1. `handler.go` lê o body se método tem body (POST/PUT/PATCH), capando em 1 MiB
2. Restaura o body como `bytes.NewReader` no Request (para o ReverseProxy
   poder re-streamar pro upstream)
3. Invoca `translator.For(endpoint.ProviderKind)` — se houver translator,
   chama `Translate(Input)` passando path, query, método, body e
   `provider_config`
4. `Output{Path, RawQuery}` é repassado para o `Rewrite` do ReverseProxy
5. Sem translator (kind=custom ou outros não implementados): passthrough

### Códigos de erro do translator

| Sentinel | HTTP | Quando acontece |
|---|---|---|
| `ErrUnknownModel` | 400 `unknown_model` | Body tem `model` que não está em `model_to_deployment`. Mensagem lista os modelos disponíveis |
| `ErrUnsupportedOperation` | 400 `bad_request` | Path canônico que o translator não conhece (ex.: `/embeddings` num translator que só faz chat) |
| `ErrEndpointMisconfigured` | 500 `endpoint_misconfigured` | Endpoint salvo sem campos obrigatórios. Admin precisa editar |

### Backward compatibility

Clientes que enviavam o path Azure raw (`/openai/deployments/.../chat/completions`)
continuam funcionando — o translator Azure detecta o prefixo `legacy` e faz
passthrough. Quando todos migrarem pro path canônico, esse fallback pode
ser removido em ADR futuro.

### Adicionar um translator novo

1. Implementar `translator.PathTranslator` no pacote (~30 LOC para qualquer
   API OpenAI-compat: Groq, vLLM, Together, OpenAI nativo)
2. Registrar no `translator.For` (1 case do switch)
3. Documentar a forma esperada de `provider_config` no doc comment do struct
4. Adicionar a validação em `adminservice.validateProviderConfig` se houver
   campos obrigatórios
5. Tests table-driven cobrindo: happy path, model ausente, mapping vazio,
   path desconhecido, body sem model

Detalhe completo do contrato + alternativas consideradas em
[ADR-0017](adrs/0017-path-translation-proxy-plane.md).
