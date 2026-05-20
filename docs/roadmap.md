# Roadmap

Estado atual do projeto e próximos passos planejados.

---

## Phase 1 — Demo executável (✅ concluída em 2026-05-20)

### O que está pronto

| Componente | Status | Notas |
|---|---|---|
| Bootstrap (`cmd/gateway/main.go`) | ✅ | Composição de dependências, graceful shutdown |
| Config (`configs/gateway.yaml`) | ✅ | Expansão de `${VAR}`, validação no boot |
| Auth (Bearer token + SHA-256) | ✅ | Constant-time compare; prefix O(1) lookup |
| Rate limit (token bucket por app) | ✅ | `golang.org/x/time/rate`; burst = RPM/10 |
| Tier pipeline (1/2/3) | ✅ | Masking + injeção + Azure CS + post-val |
| PII masking | ✅ | CPF (mod-11), CNPJ (mod-11), cartão (Luhn), e-mail, telefone BR, CEP |
| Injeção local (keywords) | ✅ | 14 padrões, case-insensitive, word boundary |
| Azure Content Safety (Tier 3) | ✅ | Prompt Shield + Text Analyze; fail-closed |
| Post-validação (Tier 3) | ✅ | Scanner local na saída do modelo |
| Provider Azure OpenAI | ✅ | Non-stream + SSE streaming |
| Provider Mock | ✅ | Resposta determinística para dev/testes |
| Budget pre-check | ✅ | Fail-open com timeout 500 ms |
| Budget counter (async) | ✅ | UPSERT via canal, worker em background |
| Usage events (async) | ✅ | INSERT via canal, worker em background |
| Audit events (async) | ✅ | INSERT via canal, worker em background |
| PostgreSQL (pgx + pool) | ✅ | pgxpool, ping no boot |
| Migrations (golang-migrate) | ✅ | 3 tabelas: usage_events, audit_events, budget_counters |
| Middleware chain | ✅ | Recover → RequestID → Logging → Auth → RateLimit |
| UUID v7 (request_id) | ✅ | Time-ordered, fallback para v4 em caso de erro de entropia |
| Endpoints | ✅ | /healthz, /readyz (DB + Azure HEAD), /v1/models, /v1/chat/completions |
| Streaming SSE | ✅ | WriteTimeout=0 (ADR-0008), Flusher, stream_cancelled |
| Timeout 504 vs 502 | ✅ | `errors.Is(context.DeadlineExceeded)` → 504; outros → 502 |
| Dockerfile + docker-compose | ✅ | Multi-stage (golang:1.24-alpine + alpine:3.21), non-root |
| Suite de testes | ✅ | ~120 casos, benchmarks, race-detector limpo |
| Documentação | ✅ | README, how-it-works, local-dev, production-deploy, maintenance, testing, 8 ADRs |

### Aplicações de homologação configuradas

Endpoint Azure: `https://danie-mc4ryviy-westeurope.cognitiveservices.azure.com`

| App | Tier | Token (homologação) | Modelos |
|---|---|---|---|
| AppBasico | tier_1 | `gwk_basic_k9mxqr7tz2wn3vfp` | gpt-4.1-nano |
| AppPro | tier_2 | `gwk_pro_n4vwlp8fy6hkjcqm` | gpt-4.1-mini, gpt-4.1-nano |
| AppVault | tier_3 | `gwk_vault_j3hsbn2cq1xdtzer` | gpt-4.1-mini |

---

## O que NÃO está no escopo da Phase 1

Estes itens foram explicitamente deixados de fora conforme SPEC.md §18:

- **Admin API** — não há endpoint para criar/alterar aplicações em tempo real (exige restart para mudar YAML)
- **Multi-instância / Redis** — rate limit é in-memory; múltiplas instâncias não compartilham estado (ADR-0006)
- **gpt-realtime (Voice Live)** — WebSocket/Realtime API não suportado pelo gateway HTTP
- **Embeddings** (`/embeddings`) — endpoint não implementado
- **TLS no gateway** — TLS termina no load balancer/edge
- **Dashboard / UI** — monitoramento via logs estruturados e queries SQL diretas
- **Autenticação multi-método** — apenas Bearer token; mTLS é Phase 2+

---

## Phase 2 — Produção (próximos passos)

Ordenados por prioridade sugerida.

### P1 — Pré-requisito para escalar

#### ADR-0002: DB-backed policies (eliminar restart para onboarding)
Implementar tabela `applications` e `api_keys` no PostgreSQL. A interface `PolicyStore` já existe — basta criar uma implementação que lê do banco em vez do YAML.

```
Impacto: deploy.go, internal/auth/policy.go (nova implementação)
Pré-requisito: Admin API (P2)
```

#### ADR-0006: Redis rate limiter (multi-instância)
Substituir `*rate.Limiter` in-memory por Redis. A interface `ratelimit.Limiter` já existe — basta implementar `RedisLimiter`.

```
Impacto: internal/ratelimit/ (nova implementação)
Pré-requisito: Redis no ambiente de deploy
```

#### Redis budget cache
O pre-check de budget faz uma query SQL por request. Em alta carga, isso cria pressão no banco. Cache Redis com TTL de 1 minuto resolve o problema.

### P2 — Qualidade operacional

#### Admin API (CRUD de aplicações)
Endpoints autenticados para criar, listar, atualizar e revogar aplicações sem restart.

```
GET    /admin/v1/applications
POST   /admin/v1/applications
PATCH  /admin/v1/applications/{name}
DELETE /admin/v1/applications/{name}
```

Requer autenticação separada (ex: token de admin com escopo diferente de `gwk_*`).

#### Métricas Prometheus
Expor `/metrics` com:
- `gateway_requests_total{app, model, tier, status}`
- `gateway_request_duration_seconds{app, tier}`
- `gateway_tokens_total{app, model, type}` (input/output)
- `gateway_budget_brl_spent{app}`
- `gateway_pii_masked_total{app, category}`

#### Tracing distribuído (OpenTelemetry)
Injetar `trace_id` e `span_id` nos logs e propagar via headers `traceparent`. Essencial para correlacionar requests entre gateway e Azure.

### P3 — Funcionalidades de produto

#### Endpoint `/embeddings`
Suporte a `POST /v1/embeddings` com `text-embedding-ada-002`. Requer:
- Novo tipo `EmbeddingRequest/Response` em `internal/providers/`
- Novo handler `Embeddings`
- Masking de PII antes de enviar (mesma lógica do chat)

#### Streaming para Tier 3 (com Content Safety)
Atualmente, Tier 3 não permite streaming porque Azure CS não tem suporte nativo a streams. Opções:
- Buffer completo do stream → CS check → liberação (aumenta latência)
- Azure CS com análise incremental (verificar disponibilidade da API)

#### Rotação automática de chaves
Integração com Azure Key Vault para rotacionar `AZURE_OPENAI_API_KEY` sem restart.

#### Suporte a múltiplos providers
Adicionar `provider: openai` ou `provider: anthropic` usando a interface `providers.Provider` já existente.

### P4 — Conformidade e governança

#### LGPD / retenção automática
Particionamento de `usage_events` e `audit_events` por mês + job de limpeza automática.

#### Relatório de consumo por aplicação
Endpoint `/admin/v1/usage/report?period=2026-05` retornando custo e tokens por aplicação.

#### Auditoria imutável
Opcional: escrever eventos de auditoria em Azure Blob Storage ou Event Hub para retenção imutável além do PostgreSQL.

---

## Como priorizar

```
Você precisa rodar com mais de 1 instância?
  → Sim: Phase 2 P1 (Redis rate limit + DB policies) primeiro
  → Não: continue na Phase 1; adicione Prometheus (P2) para visibilidade

Você precisa onboarding de apps sem restart?
  → Sim: DB-backed policies + Admin API (P1+P2)
  → Não: YAML + restart é aceitável para menos de 20 apps

Você precisa de embeddings?
  → Sim: P3 embeddings
  → Não: gateway atual cobre 100% dos casos de LLM chat
```

---

## Backlog de melhorias técnicas (não funcionais)

- [ ] `go test -coverprofile` e manter cobertura > 80% nos pacotes com lógica de negócio
- [ ] CI/CD (GitHub Actions): lint + test + build + push para ACR em cada merge para `main`
- [ ] Testes de integração contra PostgreSQL real (usando `testcontainers-go` ou banco de CI)
- [ ] Testes de contrato SSE (validar formato dos chunks contra o schema OpenAI)
- [ ] Load test real com `k6` ou `vegeta` para validar throughput em ambiente com Azure
