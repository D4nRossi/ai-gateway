# SPEC.md — AI Gateway Specification

> **Status**: Phase 1 (Demo) baseline. This document is the authoritative specification for the AI Gateway project.
> Any divergence from this document **requires an ADR** (see CLAUDE.md, section 7).
> Any ambiguity must be raised with the human before implementation.
>
> **Outstanding divergences not yet folded into this SPEC** (consult the listed ADRs for current state):
> - ADR-0009 — DB-backed admin plane (`applications`, `api_keys` live in DB, not YAML)
> - ADR-0010, ADR-0013, ADR-0016, ADR-0017 — generic proxy plane `/v1/proxy/{slug}/*`
> - ADR-0011 — admin auth via opaque session tokens (bcrypt)
> - ADR-0014 — embedded React/Vite admin SPA at `/ui`
> - ADR-0018 — Azure Key Vault as secret provider (`${kv:NAME}`)
> - ADR-0019 — Azure AI Language PII (Tier 2 fail-open, Tier 3 fail-closed)
> - ADR-0021 — Latency breakdown observable (5 buckets, header `X-Gateway-Latency-Breakdown`)
> - **ADR-0022 — PostgreSQL → SQL Server migration, schema `gogateway`** (drives all references to "Postgres" in this doc — read those as "SQL Server" until the SPEC is fully re-synced)

> **Language note**: technical spec in English (interface contracts, code, schema). Reasoning narrative may be Portuguese.

---

## 1. Project context

### 1.1 Mission
Build a corporate AI Gateway in Go that mediates traffic between internal applications and Azure OpenAI (Azure AI Foundry). The gateway enforces per-application policy, tiered security, rate limits, budgets, PII/PCI masking, optional Prompt Shield, and emits structured usage and audit events.

### 1.2 Non-goals (Phase 1)
- No frontend / admin portal.
- No multi-provider beyond Azure OpenAI (Mock provider exists for tests).
- No tool calling, RAG, function calling pipeline.
- No semantic cache.
- No financial billing.
- No multi-region HA.
- No Kubernetes deployment (Docker Compose only).
- No mTLS / SSO (corporate auth deferred).

### 1.3 Personas
- **Consumer app**: an internal service (e.g. TpCore, TpVoiceAI) holding a bearer token, calling the gateway with OpenAI-compatible payloads.
- **Operator / architect**: maintains config, monitors logs and DB.
- **Security / compliance**: queries audit events table.

---

## 2. High-level architecture

### 2.1 Data plane (critical path)

```
Consumer App
    │
    │  POST /v1/chat/completions
    │  Authorization: Bearer gwk_*
    │  Content-Type: application/json
    ▼
┌────────────────────────────────────────┐
│ AI Gateway (Go)                        │
│                                        │
│  1. Request ID                         │
│  2. Logging                            │
│  3. Auth (Bearer → AppPolicy)          │
│  4. Rate limit (per-app, in-memory)    │
│  5. Model allowlist                    │
│  6. Tier pipeline:                     │
│     • Tier 1: local masking (light)    │
│     • Tier 2: + injection heuristics   │
│     • Tier 3: + Azure Content Safety   │
│  7. Budget pre-check (SQL Server)      │   ← ADR-0022
│  8. Provider call (Azure or Mock)      │
│  9. Streaming or non-streaming         │
│ 10. Post-validation (Tier 3 only)      │
│ 11. Emit usage event (async channel)   │
│ 12. Emit audit event (async channel)   │
│ 13. Return OpenAI-compatible response  │
└────────────────────────────────────────┘
    │                          ▲
    │                          │
    ▼                          │
Azure OpenAI (Foundry)         │
                               │
              ┌────────────────┴─────────┐
              │ SQL Server (ADR-0022)    │
              │ schema: gogateway        │
              │  • usage_events          │
              │  • audit_events          │
              │  • budget_counters       │
              │  • applications, api_keys│
              │  • proxy_endpoints, …    │
              └──────────────────────────┘
```

### 2.2 Control plane (Phase 1)
- **YAML config (`configs/gateway.yaml`)**: applications, keys (hashed), models, tier definitions, timeouts.
- **Environment variables**: secrets (Azure keys, DB password).
- Admin API is **not** implemented in Phase 1.

---

## 3. Directory structure (authoritative)

```
ai-gateway/
├── cmd/
│   └── gateway/
│       └── main.go                # composition root, bootstrap, server start
├── configs/
│   └── gateway.yaml               # structural config (apps, models, tiers)
├── docs/
│   ├── adrs/                      # ADR-0001..N (see CLAUDE.md §7)
│   └── (optional supplementary docs)
├── internal/
│   ├── api/
│   │   ├── router.go              # chi router assembly
│   │   ├── handlers/
│   │   │   ├── chat.go            # POST /v1/chat/completions
│   │   │   ├── models.go          # GET /v1/models
│   │   │   └── health.go          # GET /healthz, /readyz
│   │   └── middleware/
│   │       ├── requestid.go
│   │       ├── logging.go
│   │       ├── auth.go
│   │       ├── ratelimit.go
│   │       └── recover.go
│   ├── audit/
│   │   ├── event.go               # AuditEvent struct + EventType constants
│   │   └── writer.go              # async DB writer
│   ├── auth/
│   │   ├── policy.go              # AppPolicy + PolicyStore
│   │   └── hash.go                # token prefix extraction + constant-time compare
│   ├── budget/
│   │   ├── precheck.go            # query budget_counters (sync, fast)
│   │   └── counter.go             # async upsert worker
│   ├── config/
│   │   ├── config.go              # struct + YAML load + env expand + Validate()
│   │   └── doc.go
│   ├── db/
│   │   ├── pool.go                # *sql.DB + microsoft/go-mssqldb setup (ADR-0022)
│   │   └── migrate.go             # runs golang-migrate on boot
│   ├── observability/
│   │   └── logger.go              # slog factory + context keys
│   ├── providers/
│   │   ├── provider.go            # Provider interface + types
│   │   ├── azureopenai/
│   │   │   ├── client.go          # Azure HTTP client (non-stream + stream)
│   │   │   └── sse.go             # SSE chunk parser
│   │   └── mock/
│   │       └── mock.go            # MockProvider for dev
│   ├── ratelimit/
│   │   └── limiter.go             # per-app token bucket
│   ├── security/
│   │   ├── masking/
│   │   │   ├── masker.go          # orchestrator
│   │   │   ├── detectors.go       # CPF, CNPJ, card+Luhn, email, phone, CEP
│   │   │   └── luhn.go            # Luhn algorithm
│   │   ├── promptshield/
│   │   │   ├── client.go          # Azure Content Safety client
│   │   │   ├── local.go           # local heuristics (Tier 2)
│   │   │   └── doc.go
│   │   └── postvalidation/
│   │       └── validator.go       # Tier 3 output check
│   ├── tiers/
│   │   └── engine.go              # Pipeline struct + PipelineFor(tier)
│   └── usage/
│       ├── event.go               # UsageEvent struct
│       └── writer.go              # async DB writer
├── migrations/
│   ├── 001_init.up.sql
│   └── 001_init.down.sql
├── .env.example
├── .gitignore
├── docker-compose.yml
├── Dockerfile
├── go.mod
├── go.sum
├── CLAUDE.md
├── SPEC.md
└── README.md
```

**Notes**:
- All paths inside `internal/` are mandatory for Phase 1. Adding a new directory under `internal/` requires an ADR.
- `cmd/gateway/main.go` is the **only** allowed entrypoint.

---

## 4. Configuration schema (`configs/gateway.yaml`)

```yaml
server:
  port: 8080
  read_timeout_seconds: 10
  read_header_timeout_seconds: 5
  write_timeout_seconds: 0          # 0 disables write deadline (required for SSE streaming)
  idle_timeout_seconds: 60
  max_header_bytes: 1048576

azure_openai:
  endpoint: ${AZURE_OPENAI_ENDPOINT}    # e.g. https://r.openai.azure.com
  api_key: ${AZURE_OPENAI_API_KEY}
  api_version: "2024-10-21"
  request_timeout_seconds: 60

azure_content_safety:                   # OPTIONAL; if absent, Tier 3 falls back to local heuristics
  endpoint: ${AZURE_CS_ENDPOINT}
  api_key: ${AZURE_CS_API_KEY}
  api_version: "2024-09-01"
  prompt_shield_timeout_ms: 1500
  content_safety_timeout_ms: 1500

database:
  # ADR-0022 — structured SQL Server config (replaces single URL string).
  driver: sqlserver
  host: ${DB_HOST}                          # e.g. BRSPVPDEV003
  port: 1433
  database: ${DB_NAME}                      # e.g. AzureAI_Gateway_hom
  user: ${DB_USER}                          # e.g. usr_sist_AzureAI_Gateway_hom
  password: ${kv:AzureAIGateway-DB-Password-hom}   # ADR-0018 — KV only
  schema: gogateway                         # dedicated schema, qualified in all queries
  encrypt: true
  trust_server_certificate: false           # true only for self-signed homologation certs
  max_conns: 20
  min_conns: 2

logging:
  level: info                          # debug | info | warn | error
  format: json                         # json | text
  raw_prompt_logging: false            # MUST be false in any non-dev environment

models:
  - public_name: gpt-4.1-mini
    deployment: gpt-4.1-mini-deploy
    provider: azure
    cost_input_per_1k_brl: 0.00
    cost_output_per_1k_brl: 0.00
  - public_name: gpt-4.1-nano
    deployment: gpt-4.1-nano-deploy
    provider: azure
    cost_input_per_1k_brl: 0.00
    cost_output_per_1k_brl: 0.00

applications:
  - name: AppLeve
    key_prefix: gwk_leve
    key_hash: "<sha256 hex>"
    tier: tier_1
    allowed_models: [gpt-4.1-nano]
    streaming_allowed: true
    max_rpm: 600
    max_tpm: 500000
    monthly_budget_brl: 100.00

  - name: AppMedio
    key_prefix: gwk_med
    key_hash: "<sha256 hex>"
    tier: tier_2
    allowed_models: [gpt-4.1-mini, gpt-4.1-nano]
    streaming_allowed: true
    max_rpm: 300
    max_tpm: 250000
    monthly_budget_brl: 500.00

  - name: AppSensivel
    key_prefix: gwk_sens
    key_hash: "<sha256 hex>"
    tier: tier_3
    allowed_models: [gpt-4.1-mini]
    streaming_allowed: false
    max_rpm: 60
    max_tpm: 50000
    monthly_budget_brl: 1000.00
```

### 4.1 Required validations on boot (`Config.Validate()`)
- `server.port` between 1 and 65535
- `azure_openai.endpoint` and `azure_openai.api_key` non-empty after `${...}` expansion
- `database.url` non-empty after expansion
- At least 1 entry in `models` and 1 in `applications`
- Each `applications[].tier` ∈ {`tier_1`, `tier_2`, `tier_3`}
- Each `applications[].allowed_models[]` references a `models[].public_name`
- Each `applications[].key_hash` is 64 hex chars (SHA-256 digest length)
- If `azure_content_safety` section present, all sub-fields populated

Fail fast: any validation error means `os.Exit(1)` with an `slog.Error` log of the validation failures.

---

## 5. Domain types

### 5.1 OpenAI-compatible contracts (in `internal/providers/provider.go`)

```go
type ChatMessage struct {
    Role    string `json:"role"`              // "system" | "user" | "assistant"
    Content string `json:"content"`
}

type ChatCompletionRequest struct {
    Model       string        `json:"model"`
    Messages    []ChatMessage `json:"messages"`
    Temperature *float64      `json:"temperature,omitempty"`
    MaxTokens   *int          `json:"max_tokens,omitempty"`
    TopP        *float64      `json:"top_p,omitempty"`
    Stream      bool          `json:"stream,omitempty"`
    StreamOptions *StreamOptions `json:"stream_options,omitempty"`
}

type StreamOptions struct {
    IncludeUsage bool `json:"include_usage,omitempty"`
}

type ChatCompletionResponse struct {
    ID      string                 `json:"id"`
    Object  string                 `json:"object"`            // "chat.completion"
    Created int64                  `json:"created"`
    Model   string                 `json:"model"`
    Choices []ChatCompletionChoice `json:"choices"`
    Usage   *Usage                 `json:"usage,omitempty"`
}

type ChatCompletionChoice struct {
    Index        int         `json:"index"`
    Message      ChatMessage `json:"message"`
    FinishReason string      `json:"finish_reason"`           // "stop" | "length" | "content_filter" | "tool_calls"
}

type Usage struct {
    PromptTokens     int `json:"prompt_tokens"`
    CompletionTokens int `json:"completion_tokens"`
    TotalTokens      int `json:"total_tokens"`
}

type StreamChunk struct {
    Data []byte    // raw SSE data line (without "data: " prefix)
    Done bool      // true on "[DONE]" sentinel
    Err  error     // set if upstream errored mid-stream
}

type Provider interface {
    ChatCompletions(ctx context.Context, req ChatCompletionRequest, deployment string) (*ChatCompletionResponse, error)
    StreamChatCompletions(ctx context.Context, req ChatCompletionRequest, deployment string) (<-chan StreamChunk, error)
}
```

### 5.2 Auth (`internal/auth/policy.go`)

```go
type AppPolicy struct {
    Name             string
    KeyPrefix        string
    KeyHash          string      // hex of SHA-256
    Tier             string      // tier_1 | tier_2 | tier_3
    AllowedModels    []string
    StreamingAllowed bool
    MaxRPM           int
    MaxTPM           int
    MonthlyBudgetBRL float64
}

type PolicyStore interface {
    Lookup(prefix string) (AppPolicy, bool)
}
```

### 5.3 Tier pipeline (`internal/tiers/engine.go`)

```go
type Pipeline struct {
    RunLocalMasking   bool
    RunLocalInjection bool
    RunPromptShield   bool
    RunContentSafety  bool
    RunPostValidation bool
    FailMode          string      // "open" | "closed"
}

func PipelineFor(tier string) Pipeline
```

| Tier | Local Masking | Local Injection | Prompt Shield | Content Safety | Post-Validation | Fail Mode |
|---|---|---|---|---|---|---|
| tier_1 | ✓ (CPF, card+Luhn only) | ✗ | ✗ | ✗ | ✗ | open |
| tier_2 | ✓ (all detectors) | ✓ | ✗ | ✗ | ✗ | open |
| tier_3 | ✓ (all detectors) | ✓ | ✓ | ✓ | ✓ | closed |

### 5.4 Events

```go
// internal/usage/event.go
type UsageEvent struct {
    RequestID        string
    ApplicationName  string
    Tier             string
    Model            string
    Provider         string         // "azure" | "mock"
    InputTokens      int
    OutputTokens     int
    TotalTokens      int
    LatencyMs        int
    StatusCode       int
    EstimatedCostBRL float64
    CreatedAt        time.Time
}

// internal/audit/event.go
type AuditEvent struct {
    RequestID       string
    ApplicationName string
    EventType       string         // see constants below
    Severity        string         // info | warn | error
    Metadata        map[string]any // freeform; serialized as JSONB
    CreatedAt       time.Time
}

// Constants:
const (
    EventAuthFailed             = "auth_failed"
    EventModelBlocked           = "model_blocked"
    EventPIIMasked              = "pii_masked"
    EventInjectionDetected      = "injection_detected"     // local heuristic
    EventPromptShieldBlock      = "prompt_shield_block"    // Azure CS
    EventContentSafetyBlock     = "content_safety_block"   // Azure CS
    EventRateLimited            = "rate_limited"
    EventBudgetExceeded         = "budget_exceeded"
    EventProviderError          = "provider_error"
    EventStreamCancelled        = "stream_cancelled"
)
```

---

## 6. HTTP API specification

### 6.1 Endpoints

```
GET  /healthz                    → 200 OK (no auth)
GET  /readyz                     → 200 if DB + Azure reachable, 503 otherwise (no auth)
GET  /v1/models                  → list models accessible to caller's app (auth required)
POST /v1/chat/completions        → main chat endpoint (auth required, both stream and non-stream)
```

### 6.2 `POST /v1/chat/completions` — non-streaming

**Request**:
```http
POST /v1/chat/completions HTTP/1.1
Host: localhost:8080
Authorization: Bearer gwk_med_realkey_67890
Content-Type: application/json

{
  "model": "gpt-4.1-mini",
  "messages": [
    {"role": "system", "content": "You are a helpful assistant."},
    {"role": "user", "content": "What is an AI gateway?"}
  ],
  "temperature": 0.2,
  "max_tokens": 500
}
```

**Successful response (200)**:
```http
HTTP/1.1 200 OK
Content-Type: application/json
X-Request-Id: 01HXYZ...

{
  "id": "chatcmpl-...",
  "object": "chat.completion",
  "created": 1747627200,
  "model": "gpt-4.1-mini",
  "choices": [{
    "index": 0,
    "message": {"role": "assistant", "content": "..."},
    "finish_reason": "stop"
  }],
  "usage": {"prompt_tokens": 22, "completion_tokens": 117, "total_tokens": 139}
}
```

### 6.3 `POST /v1/chat/completions` — streaming (SSE)

**Request**: `"stream": true` and optionally `"stream_options": {"include_usage": true}`.

**Response headers**:
```http
HTTP/1.1 200 OK
Content-Type: text/event-stream
Cache-Control: no-cache
Connection: keep-alive
X-Accel-Buffering: no
X-Request-Id: 01HXYZ...
```

**Response body**: stream of SSE events.
```
data: {"id":"...","object":"chat.completion.chunk","created":...,"model":"gpt-4.1-mini","choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]}

data: {"id":"...","choices":[{"index":0,"delta":{"content":"An "},"finish_reason":null}]}

data: {"id":"...","choices":[{"index":0,"delta":{"content":"AI "},"finish_reason":null}]}

...

data: {"id":"...","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":22,"completion_tokens":117,"total_tokens":139}}

data: [DONE]
```

**Cancellation**: if the consumer closes the connection, the gateway must cancel the upstream Azure call (via shared `context.Context`) and emit an audit event `event_type=stream_cancelled`.

### 6.4 Error responses

| Condition | HTTP code | Body |
|---|---|---|
| Missing/malformed bearer | 401 | `{"error":{"message":"unauthorized","type":"auth_error"}}` |
| Wrong/expired key | 401 | same |
| Model not in app's allowlist | 403 | `{"error":{"message":"model_not_allowed","type":"policy_error"}}` |
| Streaming not allowed for this app | 403 | `{"error":{"message":"streaming_not_allowed","type":"policy_error"}}` |
| Rate limited | 429 | `{"error":{"message":"rate_limited","type":"rate_limit_error"}}` + `Retry-After` header |
| Monthly budget exceeded | 429 | `{"error":{"message":"budget_exceeded","type":"budget_error"}}` |
| Prompt Shield block (Tier 3) | 403 | `{"error":{"message":"blocked_by_security","type":"security_error"}}` |
| Content Safety block (Tier 3) | 403 | same |
| Local injection heuristic (Tier 2+) | 403 | same |
| Body > max | 413 | `{"error":{"message":"payload_too_large"}}` |
| Invalid JSON | 400 | `{"error":{"message":"invalid_json"}}` |
| Upstream Azure error | 502 | `{"error":{"message":"upstream_error","type":"provider_error"}}` |
| Upstream timeout | 504 | `{"error":{"message":"upstream_timeout"}}` |
| Internal panic | 500 | `{"error":{"message":"internal_error"}}` |

Every error response triggers an audit event with the corresponding `event_type`.

### 6.5 `GET /v1/models`

Returns the models the caller's application is authorized to use.

```json
{
  "object": "list",
  "data": [
    {"id": "gpt-4.1-mini", "object": "model", "owned_by": "azure"},
    {"id": "gpt-4.1-nano", "object": "model", "owned_by": "azure"}
  ]
}
```

---

## 7. Azure OpenAI mapping

### 7.1 URL construction

Azure OpenAI endpoint URL pattern:
```
{endpoint}/openai/deployments/{deployment}/chat/completions?api-version={api_version}
```

Where:
- `endpoint` from `azure_openai.endpoint` (no trailing slash; gateway should strip if present)
- `deployment` from `models[].deployment` matching the requested `models[].public_name`
- `api_version` from `azure_openai.api_version`

### 7.2 Headers
- `api-key: {AZURE_OPENAI_API_KEY}` (Azure-specific header, **not** `Authorization: Bearer`)
- `Content-Type: application/json`

### 7.3 Request body
Forward the consumer's body as-is, **with one modification**: the `model` field is **kept** (Azure ignores it when deployment is in URL, but tools may inspect it). Other fields (`messages`, `temperature`, `max_tokens`, `stream`, `stream_options`) pass through unchanged.

### 7.4 Response
Azure returns OpenAI-compatible JSON. The gateway **passes it back to the consumer unchanged**, except:
- May add `X-Request-Id` header.
- Streaming: relay chunks as-is; do not re-serialize.

### 7.5 Notes
- The Azure resource URL might also appear as `https://{name}.services.ai.azure.com/openai/v1/...` for some Foundry deployments — confirm with the human at runtime if URL behaves unexpectedly.

---

## 8. Database schema (initial migration — SQL Server, ADR-0022)

> **Note**: Section content below is the **historical PostgreSQL schema** preserved
> as documentation reference. The current authoritative schema is in T-SQL under
> `migrations/00*.up.sql` (qualified with `gogateway.*`). The legacy PG migrations
> are preserved under `migrations/postgres-legacy/`. Tipo mappings PG → T-SQL
> are documented in `CLAUDE.md` §9.4.

### 8.1 `migrations/001_init.up.sql`

```sql
CREATE TABLE usage_events (
    id BIGSERIAL PRIMARY KEY,
    request_id TEXT NOT NULL,
    application_name TEXT NOT NULL,
    tier TEXT NOT NULL,
    model TEXT NOT NULL,
    provider TEXT NOT NULL,
    input_tokens INTEGER,
    output_tokens INTEGER,
    total_tokens INTEGER,
    latency_ms INTEGER NOT NULL,
    status_code INTEGER NOT NULL,
    estimated_cost_brl NUMERIC(12, 6),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_usage_app_created ON usage_events(application_name, created_at DESC);
CREATE INDEX idx_usage_request ON usage_events(request_id);

CREATE TABLE audit_events (
    id BIGSERIAL PRIMARY KEY,
    request_id TEXT NOT NULL,
    application_name TEXT NOT NULL,
    event_type TEXT NOT NULL,
    severity TEXT NOT NULL,
    metadata JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_audit_app_created ON audit_events(application_name, created_at DESC);
CREATE INDEX idx_audit_event_type ON audit_events(event_type);
CREATE INDEX idx_audit_request ON audit_events(request_id);

CREATE TABLE budget_counters (
    id BIGSERIAL PRIMARY KEY,
    application_name TEXT NOT NULL,
    period_yyyymm TEXT NOT NULL,
    total_requests BIGINT NOT NULL DEFAULT 0,
    total_tokens BIGINT NOT NULL DEFAULT 0,
    estimated_cost_brl NUMERIC(14, 6) NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (application_name, period_yyyymm)
);
CREATE INDEX idx_budget_app ON budget_counters(application_name);
```

### 8.2 `migrations/001_init.down.sql`

```sql
DROP INDEX IF EXISTS idx_budget_app;
DROP TABLE IF EXISTS budget_counters;

DROP INDEX IF EXISTS idx_audit_request;
DROP INDEX IF EXISTS idx_audit_event_type;
DROP INDEX IF EXISTS idx_audit_app_created;
DROP TABLE IF EXISTS audit_events;

DROP INDEX IF EXISTS idx_usage_request;
DROP INDEX IF EXISTS idx_usage_app_created;
DROP TABLE IF EXISTS usage_events;
```

---

## 9. Request flow (numbered)

### 9.1 Non-streaming

```
1.  Server receives POST /v1/chat/completions.
2.  Middleware `requestid` generates UUID → injects into ctx and X-Request-Id header.
3.  Middleware `logging` starts request span.
4.  Middleware `auth`:
    a. Extract Bearer.
    b. Lookup AppPolicy by key_prefix.
    c. SHA-256(token) vs. policy.KeyHash via subtle.ConstantTimeCompare.
    d. On match: inject AppPolicy into ctx; else 401 + audit event.
5.  Middleware `ratelimit`: token bucket check by app name; on deny: 429 + audit + Retry-After.
6.  Handler `chat`:
    a. Read body (limit MaxBodyBytes; e.g. 1 MiB).
    b. Unmarshal into ChatCompletionRequest.
    c. Validate: model ∈ policy.AllowedModels; else 403 + audit.
    d. If req.Stream && !policy.StreamingAllowed → 403 + audit.
7.  Tier pipeline:
    a. `pipe := PipelineFor(policy.Tier)`
    b. If `pipe.RunLocalMasking`: run masker on each message.Content; on detections, replace text and emit audit `pii_masked` with category counts.
    c. If `pipe.RunLocalInjection`: scan each message; on detection → 403 + audit `injection_detected` (only if Tier 2/3).
    d. If `pipe.RunPromptShield`: call Azure CS shieldPrompt with timeout from config; on attack → 403 + audit `prompt_shield_block`; on error: fail_mode (open/closed).
    e. If `pipe.RunContentSafety`: call Azure CS analyze with timeout; on severity ≥ 4 → 403 + audit `content_safety_block`.
8.  Budget pre-check (sync, fast):
    a. Query budget_counters for (app, period_yyyymm).
    b. If estimated_cost_brl >= policy.MonthlyBudgetBRL → 429 + audit `budget_exceeded`.
9.  Provider call:
    a. Resolve deployment from models config.
    b. ctx = WithTimeout(parent, azure.request_timeout_seconds).
    c. provider.ChatCompletions(ctx, req, deployment).
    d. On error: 502 + audit `provider_error`.
10. Post-validation (Tier 3 only): run promptshield/postvalidation on response.choices[].message.content; on block → 403 + audit.
11. Compute cost estimate: input_tokens * model.cost_input + output_tokens * model.cost_output.
12. Publish UsageEvent (non-blocking on full channel; warn if dropped).
13. Publish BudgetUpdateEvent (separate channel or unified).
14. Return JSON response.
15. Middleware `logging` finalizes: log info with latency_ms, status_code, tokens.
```

### 9.2 Streaming
Steps 1–8 identical. Then:
```
9.  Set SSE headers (Content-Type, Cache-Control, Connection, X-Accel-Buffering).
10. flusher, ok := w.(http.Flusher); if !ok → 500 (server config error).
11. ch, err := provider.StreamChatCompletions(ctx, req, deployment).
12. For chunk := range ch:
    a. If chunk.Err != nil → write SSE error event, break.
    b. If chunk.Done → write "data: [DONE]\n\n", break.
    c. Write "data: %s\n\n" with chunk.Data; flusher.Flush().
    d. If ctx.Done() (client disconnect) → break; cancel upstream; audit `stream_cancelled`.
13. Parse final chunk for usage (if include_usage was set); publish UsageEvent + BudgetUpdate.
14. Return.
```

---

## 10. Masking specification

### 10.1 Detectors and tags

| Category | Detection | Replacement |
|---|---|---|
| `BR_CPF` | regex + 11-digit mod-11 checksum | `[BR_CPF_REDACTED]` |
| `BR_CNPJ` | regex + 14-digit mod-11 checksum | `[BR_CNPJ_REDACTED]` |
| `PCI_CARD` | 13–19 digit regex + Luhn check | `[PCI_CARD_REDACTED]` |
| `EMAIL` | pragmatic regex (not RFC) | `[EMAIL_REDACTED]` |
| `PHONE_BR` | regex for BR formats | `[PHONE_REDACTED]` |
| `CEP_BR` | `\d{5}-?\d{3}` | `[CEP_REDACTED]` |

### 10.2 Tier 1 vs Tier 2+
- **Tier 1**: only `BR_CPF` and `PCI_CARD` (highest-risk PII/PCI).
- **Tier 2+**: all detectors above.

### 10.3 Overlap resolution
If two detectors match overlapping regions, the **longer** match wins. If equal length, prefer order: CARD > CNPJ > CPF > PHONE > CEP > EMAIL.

### 10.4 Audit metadata
Emit `event_type=pii_masked`, `severity=info`, with metadata:
```json
{
  "categories": {"BR_CPF": 2, "PCI_CARD": 1},
  "total_replacements": 3
}
```
**Do not include the original or masked text** in audit.

### 10.5 Luhn algorithm
Standard mod-10 checksum. Digit sequence valid if `sum(doubled_alternating_digits + original_other_digits) % 10 == 0`. Doubled digits > 9 have their digits summed (or equivalently: subtract 9).

### 10.6 CPF / CNPJ checksum
Standard Brazilian mod-11 algorithm. Implementation must reject obviously invalid inputs (all-same digit: `11111111111`, `00000000000`).

---

## 11. Prompt Shield specification

### 11.1 Azure Content Safety — Prompt Shield

Endpoint:
```
POST {endpoint}/contentsafety/text:shieldPrompt?api-version={api_version}
```

Headers:
- `Ocp-Apim-Subscription-Key: {AZURE_CS_API_KEY}`
- `Content-Type: application/json`

Request body:
```json
{
  "userPrompt": "<concatenated user/system messages>",
  "documents": []
}
```

Response (relevant field):
```json
{
  "userPromptAnalysis": {"attackDetected": true|false},
  "documentsAnalysis": []
}
```

If `userPromptAnalysis.attackDetected == true` → block with 403 and audit.

**Source of truth for request/response shape**: confirm at runtime via `WebFetch` of `https://learn.microsoft.com/en-us/azure/ai-services/content-safety/quickstart-prompt-shield` before implementing.

### 11.2 Azure Content Safety — Text Analyze

Endpoint:
```
POST {endpoint}/contentsafety/text:analyze?api-version={api_version}
```

Request body:
```json
{
  "text": "<concatenated messages>",
  "categories": ["Hate", "SelfHarm", "Sexual", "Violence"]
}
```

Response includes `categoriesAnalysis[].severity`. Severity scale: 0, 2, 4, 6. Block if any severity ≥ 4.

**Confirm shape via `WebFetch` of official quickstart before implementation.**

### 11.3 Local heuristics fallback

When `azure_content_safety` is not configured **or** when running Tier 2 (which never calls Azure), apply local heuristics:

- Match against keyword list (case-insensitive, with word boundaries):
  - `ignore previous instructions`, `ignore all previous`, `disregard the above`
  - `system prompt`, `your instructions`, `your system message`
  - `pretend to be`, `act as`, `you are now`, `you are DAN`, `developer mode`
  - `<\|im_start\|>`, `<\|im_end\|>`, `<\|system\|>`
- Optional: ratio of imperative verbs to total tokens above threshold.

On match: Tier 2 → 403 + audit `injection_detected`. (Tier 3 should normally have caught it via Azure CS; local is fallback.)

### 11.4 Fail-mode semantics
- **Fail-open** (Tier 1, Tier 2): if Azure CS unreachable/timeout → continue with `warn` log + audit `prompt_shield_block` with `severity=warn` and metadata `{"reason":"fail_open"}`. **Do not block** the request.
- **Fail-closed** (Tier 3): if Azure CS unreachable/timeout → 503 + audit `prompt_shield_block` with `severity=error`. **Block** the request.

---

## 12. Rate limit and budget

### 12.1 Rate limit
- Library: `golang.org/x/time/rate`.
- One `*rate.Limiter` per application, stored in a `sync.Mutex`-guarded map.
- Constructor: `rate.NewLimiter(rate.Every(time.Minute/time.Duration(rpm)), burst)` where `burst = max(1, rpm/10)`.
- Check: `limiter.Allow()`. If false → 429 + audit + `Retry-After: 1` header (best-effort).

### 12.2 Budget pre-check
- Sync query: `SELECT estimated_cost_brl FROM budget_counters WHERE application_name=$1 AND period_yyyymm=$2`.
- Period format: `YYYYMM` UTC (e.g., `202605`).
- If current spend ≥ policy budget → 429 + audit `budget_exceeded`.
- Query timeout: 500ms; on timeout, **fail-open** (warn log) — do not block business on DB hiccup.

### 12.3 Budget update (async)
- Publish event to `chan BudgetUpdateEvent` after each successful (and failed but counted) request.
- Worker UPSERT:
  ```sql
  INSERT INTO budget_counters (application_name, period_yyyymm, total_requests, total_tokens, estimated_cost_brl, updated_at)
  VALUES ($1, $2, 1, $3, $4, NOW())
  ON CONFLICT (application_name, period_yyyymm)
  DO UPDATE SET
      total_requests = budget_counters.total_requests + 1,
      total_tokens = budget_counters.total_tokens + EXCLUDED.total_tokens,
      estimated_cost_brl = budget_counters.estimated_cost_brl + EXCLUDED.estimated_cost_brl,
      updated_at = NOW();
  ```

### 12.4 Cost estimation
- Per model, read `cost_input_per_1k_brl` and `cost_output_per_1k_brl` from config.
- `cost_brl = (input_tokens / 1000) * cost_input_per_1k_brl + (output_tokens / 1000) * cost_output_per_1k_brl`
- Use `float64`. Precision is acceptable for Phase 1.

---

## 13. Observability

### 13.1 Logging
- Library: `log/slog` standard library.
- Format: JSON in production, text in development (controlled by `logging.format` config).
- Default level: `info`.
- Fields required per request lifecycle (when applicable to context):
  - `request_id`
  - `application_name`
  - `tier`
  - `model`
  - `provider`
  - `latency_ms`
  - `status_code`
  - `event_type`
  - `err` (when error)
- **Never log** prompt content, response content, raw bearer token, or env values.

### 13.2 Metrics (Phase 2 — out of scope unless time permits)
If implemented, expose Prometheus exporter at `/metrics`. Metrics: see briefing section 19.

### 13.3 Health endpoints
- `GET /healthz`: liveness — always 200 if process running.
- `GET /readyz`: readiness — checks DB ping (timeout 1s) and Azure endpoint reachability (HEAD with 1s timeout). Returns 503 with body listing failed checks.

---

## 14. Security

### 14.1 Token storage
- Bearer tokens **never stored** in DB or logs.
- Only SHA-256 hex digest in config (`key_hash` in YAML).
- Generation: `echo -n "<token>" | sha256sum`.

### 14.2 Constant-time comparison
- All hash/token comparisons via `subtle.ConstantTimeCompare`. Never `bytes.Equal`, never `==`.

### 14.3 Body limits
- `http.MaxBytesReader(w, r.Body, MaxBodyBytes)`. Default `MaxBodyBytes = 1 << 20` (1 MiB).

### 14.4 Timeouts (HTTP server)
- `ReadTimeout: 10s`
- `ReadHeaderTimeout: 5s`
- `WriteTimeout: 0` (disabled — required for SSE)
- `IdleTimeout: 60s`
- `MaxHeaderBytes: 1 << 20`

Streaming required `WriteTimeout: 0`; this is the known trade-off. See **ADR-0008**.

### 14.5 TLS
- Phase 1: no TLS on the gateway itself. NGINX or service mesh is expected at the edge in production.
- The gateway **does** use TLS when calling Azure (system trust store).

### 14.6 Container security
- Runs as non-root user `app` (uid auto-assigned).
- Multi-stage Docker build; final image based on `alpine:3.21`.
- No shell tools installed in final image beyond Alpine base.

---

## 15. Streaming details

### 15.1 SSE parsing (Azure → gateway)
- Azure responds with `Content-Type: text/event-stream`.
- Events separated by `\n\n`.
- Each event has `data: <json>` lines; the gateway must:
  - Read line by line (`bufio.Scanner` with adjusted buffer for long lines, or `bufio.Reader`).
  - Strip `data: ` prefix.
  - On `[DONE]` sentinel: end stream.
  - On empty `data:` (keepalive): skip.
  - Forward each chunk's JSON payload as-is to consumer's SSE stream (do not re-marshal).

### 15.2 Buffer sizing
- `bufio.NewReaderSize` with `64 KiB` (default may be too small for some chunks; default Scanner limit can panic).

### 15.3 Cancellation
- Handler must use `r.Context()` for all upstream calls.
- Detect client disconnect: when `ctx.Done()` fires, the goroutine reading from Azure cancels its `http.Request`, and the Go HTTP client closes the connection (releases tokens upstream).
- Emit `event_type=stream_cancelled`.

### 15.4 Usage extraction
- If consumer sent `stream_options.include_usage: true`, Azure emits a final chunk with `usage` populated. Extract and publish.
- If not sent (or upstream omits): emit `UsageEvent` with `input_tokens=0, output_tokens=0`, mark with `note=no_usage_in_stream` in audit.

---

## 16. Bootstrap sequence (`cmd/gateway/main.go`)

```
1.  Parse env / flags (config file path).
2.  Load YAML config → ExpandEnv → Unmarshal → Validate(). On error: log + exit 1.
3.  Initialize slog logger (level, format from config).
4.  Initialize SQL Server connection (`db.NewMSSQL(ctx, cfg.Database)`). On error: exit 1.
5.  Run migrations. On error: exit 1.
6.  Build PolicyStore from config.applications.
7.  Build model catalog from config.models.
8.  Initialize MockProvider and AzureOpenAIProvider; choose primary based on env PROVIDER (default: azure).
9.  Initialize Masker (load detectors with regex compilations).
10. Initialize PromptShield client (if azure_content_safety configured).
11. Initialize RateLimiter Manager.
12. Initialize Budget PreChecker + AsyncCounter worker.
13. Initialize Usage writer (channel + worker goroutine).
14. Initialize Audit writer (channel + worker goroutine).
15. Build chi router with middleware chain: recover → requestid → logging → auth → ratelimit → handlers.
16. Build http.Server with timeouts from config.
17. Trap SIGINT / SIGTERM; trigger graceful shutdown: stop accepting, wait writers to drain (with grace period 5s), close pool, close http.Server.
18. ListenAndServe.
```

---

## 17. Acceptance criteria (Phase 1 demo)

The demo is successful when the following 13 criteria are met (mirrors briefing §34):

1. Consumer app calls gateway with Bearer token.
2. Gateway validates the bearer.
3. Gateway resolves application identity.
4. Gateway validates requested model against allowlist.
5. Gateway applies the application's tier pipeline.
6. Gateway masks PII/PCI per tier rules (audit emitted).
7. Gateway forwards to Azure OpenAI.
8. Gateway returns OpenAI-compatible response.
9. Gateway persists `usage_events` row per request.
10. Gateway persists `audit_events` rows for policy decisions.
11. Gateway exposes basic observability (structured JSON logs at minimum; `/metrics` desirable).
12. Gateway supports streaming (SSE) end-to-end.
13. Gateway blocks requests violating policy (model, tier, rate, budget, shield).

---

## 18. Out of scope for Phase 1 (roadmap)

Plan items deliberately deferred (each will get its own ADR when initiated):
- Admin API (CRUD for apps, keys, policies).
- DB-backed apps/keys/policies (replace YAML).
- Redis-backed rate limit (multi-instance).
- OpenTelemetry traces and Prometheus exporter.
- mTLS / SSO for admin plane.
- Multi-provider beyond Azure (Anthropic, local models, LiteLLM bridge).
- Tool calling / function calling support.
- Embeddings, audio, rerank endpoints.
- Semantic cache.
- Multi-region HA.
- Frontend / dashboard.

---

## 19. Reference index

| Topic | Reference |
|---|---|
| Bearer token spec | RFC 6750 |
| SSE spec | https://html.spec.whatwg.org/multipage/server-sent-events.html |
| OpenAI Chat API | https://platform.openai.com/docs/api-reference/chat |
| Azure OpenAI REST | https://learn.microsoft.com/en-us/azure/ai-services/openai/reference |
| Azure Prompt Shield | https://learn.microsoft.com/en-us/azure/ai-services/content-safety/quickstart-prompt-shield |
| Azure Content Safety (text) | https://learn.microsoft.com/en-us/azure/ai-services/content-safety/quickstart-text |
| chi router | https://github.com/go-chi/chi |
| microsoft/go-mssqldb (ADR-0022) | https://github.com/microsoft/go-mssqldb |
| pgx (legacy — Phase 1 reference) | https://github.com/jackc/pgx |
| golang-migrate | https://github.com/golang-migrate/migrate |
| slog | https://pkg.go.dev/log/slog |
| Luhn algorithm | https://en.wikipedia.org/wiki/Luhn_algorithm |
| Brazilian CPF / CNPJ | Receita Federal (algoritmo público) |

---

End of SPEC.md.
