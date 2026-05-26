# Roadmap

Estado real do projeto e frentes em andamento. Não há datas — entregas são feitas conforme prioridade do owner.

---

## 1. Estado atual (concluído)

### Phase 1 — Demo executável (concluída)

Núcleo do gateway de IA. Tudo abaixo está em produção do branch `main`/`v2`.

| Componente | Notas |
|---|---|
| Bootstrap (`cmd/gateway/main.go`) | Composição de dependências, graceful shutdown (SIGINT/SIGTERM) |
| Config (`configs/gateway.yaml`) | Expansão de `${VAR}` no boot, validação fail-fast |
| Auth Bearer + SHA-256 | Constant-time compare; prefix O(1) lookup; **ASCII-only enforcement** (Onda 1) |
| Rate limit token-bucket por app | `golang.org/x/time/rate`; burst = RPM/10 |
| Pipeline por tier (1/2/3) | Masking regex → injeção local → Azure CS → post-validação |
| PII masking regex | CPF (mod-11), CNPJ (mod-11), cartão (Luhn), e-mail, telefone BR, CEP |
| Injeção local | 14 padrões, case-insensitive, word boundary |
| Azure Content Safety (Tier 3) | Prompt Shield + Text Analyze; fail-closed |
| Post-validação (Tier 3) | Scanner local na saída do modelo |
| Provider Azure OpenAI | Non-stream + SSE streaming |
| Provider Mock | Resposta determinística para dev/testes |
| Budget pre-check + counter (async) | Fail-open com timeout; UPSERT via canal |
| Usage / Audit events (async) | Worker em background com buffer de canal |
| PostgreSQL (pgx + pool) | pgxpool, ping no boot |
| Migrations (golang-migrate) | 5 migrations, `up`/`down` simétricas |
| Streaming SSE | `WriteTimeout=0` (ADR-0008), Flusher, `stream_cancelled` |
| Timeout 504 vs 502 | `errors.Is(context.DeadlineExceeded)` → 504 |
| Dockerfile + docker-compose | Multi-stage, non-root, healthcheck |
| Suite de testes | ~120 casos, benchmarks, race-detector limpo |

### v2 — Admin plane + Proxy plane (concluída)

Adições da branch `v2` ao demo original. Mantém compatibilidade total com Phase 1.

| Componente | Notas |
|---|---|
| Admin auth (bcrypt + sessões opacas) | ADR-0011 |
| Tabelas `applications` + `api_keys` no DB | Substitui (não remove) o YAML de apps |
| CLI `cmd/admin-create` | Provisiona o primeiro admin |
| CRUD de aplicações pela UI | Criação, rotação de chave, soft-delete |
| Proxy plane `/v1/proxy/{slug}/*` | Engine genérico baseado em `httputil.ReverseProxy` (ADR-0010) |
| Endpoints + targets cadastráveis | DB-backed, credenciais criptografadas (AES-256-GCM, ADR-0012) |
| Load balancer (round-robin / least-conn / ip-hash) | ADR-0013 |
| Provider catalog | 10 providers + `custom` (ADR-0016, Lote A.6 do console) |
| Console React+Vite embedado no binário | `//go:embed` (ADR-0014) |
| Playground UI | Disparo ad-hoc sem curl/Postman |
| Página de Observability | Tabs Uso / Auditoria / Budget |
| Quality of life (search, refresh, Cmd+K, breadcrumbs) | Lote A do console |

### Onda 1 — Hardening de tokens ASCII (concluída)

Diagnóstico em `git log` do commit referente. Resumo: `deriveKeyPrefix` aceitava Unicode (`unicode.IsLetter/IsDigit`), o que gerava prefixes com bytes multibyte que conflitam com a transliteração latin-1 que clientes HTTP fazem em headers (RFC 7230). O resultado era `SQLSTATE 22021` no Postgres surfacando como 500 confuso.

- `internal/auth/hash.go` — `ExtractPrefix` rejeita qualquer byte fora de 0x21–0x7E
- `internal/app/adminservice/service.go` — `deriveKeyPrefix` restrito a `[a-z0-9]`
- `internal/proxy/auth.go` — early-return 401 antes do hit no DB
- 14 casos de teste novos (UTF-8, latin-1, espaços, tabs, byte ≥ 0x80)

---

## 2. Ondas em execução

Ordem fechada em conversa anterior. Cada onda é um PR independente com critério de done explícito.

### Onda 2 — Path translation por `provider_kind` (próxima)

**Pedido:** "a aplicação só chama seu respectivo endpoint e o gateway resolve tudo". Hoje o cliente precisa colar `/openai/deployments/gpt-4.1/chat/completions?api-version=2025-01-01-preview`; depois disso vai chamar `POST /v1/proxy/{slug}/chat/completions` com body OpenAI-style.

**Escopo:**
- Migration nova: `006_endpoint_provider_config.up.sql` adiciona `provider_config JSONB DEFAULT '{}'` em `proxy_endpoints`. Pra Azure: `{api_version, model_to_deployment: {...}}`
- Novo `internal/proxy/translator/` com interface `PathTranslator` + impl `AzureOpenAITranslator`
- `internal/proxy/director.go` ganha hook que invoca translator quando `provider_kind != "custom"`
- UI: campos `api_version` + mapping no form de endpoint Azure
- Playground reformulado: campo "model" + body OpenAI-style automático em endpoints Azure
- ADR-0017 (path translation no proxy plane)

**Critério de done:** request `POST /v1/proxy/{slug}/chat/completions` body `{"model":"gpt-4.1","messages":[...]}` chega no Azure pelo deployment correto. Endpoint `custom` continua passthrough idêntico.

### Onda 3 — Azure Key Vault

**Pedido:** mover segredos do `.env` pro `https://danieldev.vault.azure.net/`, usando `DefaultAzureCredential` (decidido em conversa).

**Escopo:**
- Dependências: `azidentity` + `azsecrets` do Azure SDK for Go (precisa ADR-0018)
- `internal/infra/keyvault/` com client + cache LRU + TTL configurável (default 5min)
- Loader de config aceita `${kv:NOME-DO-SECRET}` além de `${VAR}`
- `KEYVAULT_URI` opcional no `.env`; se ausente, `${kv:…}` falha no boot com mensagem clara
- Nova doc `docs/keyvault-setup.md`: como cadastrar credenciais (CLI + RBAC role do user), como `az login` em dev, como Managed Identity em prod
- README + `.env.example` atualizados

**Critério de done:** substituir `AZURE_OPENAI_API_KEY` no `.env` por `${kv:AZURE-OPENAI-API-KEY}` no YAML, remover do `.env`, gateway sobe normal.

### Onda 4 — Azure Language PII

**Pedido:** detecção de PII complementar (cobre o que regex não pega), endpoint `https://tp-language-pii.cognitiveservices.azure.com/`. Tier 2 fail-open, Tier 3 fail-closed (decidido em conversa).

**Escopo:**
- `internal/security/azlanguage/` — cliente `/language/:analyze-text?api-version=2024-11-01` com `kind=PiiEntityRecognition`
- Nova seção `azure_language` no YAML, com chave puxada do KV (depende da Onda 3)
- Pipeline em `chat.go`: roda em paralelo (goroutine) com masking local; substitui no body antes do call provider
- Novo evento audit `pii_detected_remote` (separado de `pii_masked` regex)
- ADR-0019 com análise de latência (timeout default 1500ms)

**Critério de done:** request Tier 2 com PII que regex não pega aparece mascarada no body enviado. Tier 3 bloqueia (503) quando Language está fora.

### Onda 5 — UI

**Pedido:** modal de Token Gerado ilegível no dark mode + modal lenta abrindo + playground reformulado pra refletir Onda 2.

**Escopo:**
- Investigar `web/src/pages/Applications.tsx` / `ApplicationDetail.tsx` — buscar classes sem variante dark
- Profiler React DevTools no modal → identificar re-renders desnecessários
- Playground: novos campos quando endpoint é Azure (depende da Onda 2)
- Atualização do `console-roadmap.md`

**Critério de done:** modal legível em ambos os temas (contraste WCAG AA mínimo), abre em <50ms medido. Playground com Azure não exige path manual.

---

## 3. Frentes descobertas durante a Onda 1

Pequenas/médias, não bloqueantes, surgiram do debugging atual. Vão entrar numa onda futura ou em PRs pequenos avulsos.

### Admin audit

**Causa:** ações administrativas (criar app, criar endpoint, conceder grant, rotacionar chave, login admin) **não emitem eventos**. A página Observability fica vazia até alguém disparar request de chat. Confirmado via `grep` em `internal/api/admin/` e `internal/app/adminservice/`.

**Proposta de escopo:**
- Decisão de schema (precisa input): estender `audit_events` com colunas opcionais `admin_username`, `target_type`, `target_id` **ou** criar tabela separada `admin_audit_events`. A segunda é mais limpa, a primeira economiza join na UI
- Eventos novos: `application_created`, `application_updated`, `application_deleted`, `endpoint_created`, `endpoint_updated`, `endpoint_deleted`, `grant_granted`, `grant_revoked`, `key_rotated`, `admin_login`, `admin_logout`, `admin_login_failed`
- Aba nova "Admin" na página Observability ou merge na aba Auditoria com filtro
- ADR a discutir

### Validação sistemática de inputs

**Diagnóstico não feito ainda** — surgiu do pedido "valide inputs, consultas, etc etc". Vou auditar todos os handlers admin (`internal/api/admin/handlers/`) e produzir um relatório:
- Comprimento máximo de cada campo
- Sanitização de slug (já existe pra endpoint, conferir para app name)
- Validação de URL em targets
- Validação de hex hash, prefixes
- Defesa contra SQL injection (pgx parameter mode já protege, mas verificar 100%)

### Latency breakdown observability

Hoje `usage_events.latency_ms` é só o total. Quando o user reclama de 2.6s, não dá pra saber se foi auth, masking, provider ou serialização. Proposta:
- Header `X-Gateway-Latency-Breakdown` no response (`auth=2ms;mask=3ms;provider=2580ms;…`)
- Colunas novas em `usage_events` (`latency_auth_ms`, `latency_pipeline_ms`, `latency_provider_ms`) — opcional, baixa cardinalidade
- Não medir o que não importa: nada de timer per-middleware (overhead > benefício)

### UX dos filtros de Observability

`web/src/pages/Observability.tsx:89-90` — `from` e `to` são fixados no `useState` inicial e nunca recalculam. Se você abre a página e espera, o filtro "Até" fica congelado. **Correção trivial:** ao clicar Aplicar, recalcular `to = new Date().toISOString()`. Não é bug de timezone — confirmado que migrations usam `TIMESTAMPTZ` corretamente e o front usa `toLocaleString`.

### Fixture quebrada de `TestValidate_ValidConfig`

`internal/config/config_test.go:42` — fail pré-existente (anterior à Onda 1), confirmado via `git stash`. Falta `encryption_key_hex` válido no fixture pós-ADR-0012. Fix trivial: adicionar `EncryptionKeyHex: "0000…"` (64 hex chars) no test data.

### Cleanup de apps órfãs (acompanhamento Onda 1)

Apps cadastradas antes da Onda 1 com chars Unicode no prefix ficam órfãs. Hoje o cleanup é manual via `psql`. Considerar: comando CLI `cmd/admin-tools cleanup-nonascii-prefixes` que faz a query e pede confirmação. Baixa prioridade — provavelmente uma vez na vida.

### Logs do slog assíncronos

Hoje `audit_events`, `usage_events` e `budget_counters` são assíncronos via canal (ADR-0005). Mas o `slog.Logger` direto (start/end/error logs) é síncrono — cada `logger.Info(...)` faz write no stdout dentro da goroutine do handler. Em prod com stdout indo pra arquivo + fsync, vira gargalo acima de ~1k req/s. Solução: handler com buffer + flush em background, ou integração com Loki/Azure Monitor (que já fica na Phase 3, "Observabilidade externa"). Para a demo atual (≤100 req/s) é invisível.

### Compression de payload

- Outbound gateway → cliente: hoje não comprime. Response de chat com histórico pode chegar a 50-200KB; gzip cortaria pra ~10-30KB. Ganho real em conexão mobile/edge.
- Outbound gateway → Azure: `DisableCompression: false` no transport, mas o Go por padrão só negocia gzip em GET; POST não envia `Accept-Encoding` automaticamente. Azure provavelmente sempre retorna identity.
- Inbound cliente → gateway: chi não descompacta gzip request body. Se cliente comprimir, gateway quebra silenciosamente.

Frente nova de 1-2h de trabalho. Não bloqueia nada urgente — entra na Phase 3.

### Cache de policy/endpoint lookup

Hoje cada request `/v1/proxy/*` faz `GetBySlug` + `HasGrant` no DB (~2-5ms). Cache LRU+TTL cortaria pra <0.1ms — micro-otimização. Vale quando aparecer pressão real. Phase 3.

---

## 4. Phase 3 — Escalabilidade e produção

Itens sem ordem fixa. Cada um é candidato a ADR próprio.

### Multi-instância

- **Redis rate limiter** — substitui `golang.org/x/time/rate` in-memory. A interface `ratelimit.Limiter` já existe; basta nova impl
- **Redis budget cache** — pre-check de budget hoje faz SQL por request; cache com TTL 60s elimina pressão no DB
- **DB-backed sessões admin** já está em produção (`admin_sessions`) — multi-instance OK pra admin desde já

### Observabilidade externa

- **Métricas Prometheus** — `/metrics` com counters/histograms padrão (requests_total, request_duration_seconds, tokens_total, budget_brl_spent, pii_masked_total)
- **Tracing OpenTelemetry** — `trace_id`/`span_id` nos logs, propagação via `traceparent`, exporter OTLP
- **Logs estruturados centralizados** — já é JSON com `slog`; falta integrar com algum sink (Loki, Azure Monitor)

### Resiliência

- **Circuit breaker** por target — abre após N falhas seguidas, half-open com backoff
- **Retries com jitter** — para erros transientes do provider (5xx, timeout); idempotência só em GET/PUT
- **Drain mode** — flag operacional para drenar conexões antes de shutdown (Lote F do console)
- **Health check ativo de targets** — hoje o LB confia que targets estão vivos; ping periódico marca como degraded

### Funcionalidades novas

- **`POST /v1/embeddings`** (e `/v1/proxy/.../embeddings`) — mesmo pipeline de masking
- **Realtime / Voice Live** — WebSocket, fora do modelo HTTP atual; ADR à parte
- **Streaming Tier 3** — hoje bloqueado porque Azure CS não tem stream nativo; opção: buffer + check + flush

### Governança / LGPD

- **Particionamento mensal** de `usage_events` e `audit_events` + job de retenção
- **Auditoria imutável** — replicar audit_events para Azure Blob (append-only) ou Event Hub
- **Relatório de consumo** `/admin/v1/usage/report?period=YYYYMM`
- **Anonimização** opcional de logs após N dias (mascarar `application_name` em audit antigos)

### Segurança operacional

- **mTLS** entre gateway e backends sensíveis (opcional por endpoint)
- **IP allowlist** por aplicação (Lote F do console)
- **2FA opcional** pra admins (Lote D do console)
- **Webhook + alertas** em thresholds de budget e error rate (Lote E do console)

---

## 5. Visão de longo prazo — gateway genérico não-IA

O proxy plane (`/v1/proxy/{slug}/*` com `provider_kind=custom`) já permite cadastrar **qualquer endpoint HTTP**. Hoje é passthrough bruto. Pra virar API gateway corporativo de verdade, faltam capabilities que são **complementares**, não substitutas, do que existe:

- **Template engine de path** — `{slug}/users/{id}` no cadastro, cliente chama `/users/42` e gateway traduz pra `https://upstream/api/v2/users/42`
- **Transformações de request/response declarativas** — adicionar headers, renomear campos JSON, mapear status codes, sem código
- **Rate limit / quota por endpoint × app × verbo** — hoje é por app só
- **Cache de response** com TTL e invalidation por header/path
- **Mock mode por endpoint** — útil pra contratos antes do backend existir
- **Versionamento de endpoint** — `/v1/proxy/{slug}` vs `/v2/proxy/{slug}` com config diferente

Nenhum desses está em planejamento ainda — anotados aqui só pra deixar o vetor de evolução explícito.

---

## 6. Backlog técnico (não funcional)

- [ ] `go test -coverprofile` e meta de cobertura > 80% nos pacotes com lógica de negócio
- [ ] CI/CD (GitHub Actions): lint + test + build + push pra ACR em cada merge
- [ ] Testes de integração contra Postgres real (`testcontainers-go` ou banco de CI)
- [ ] Testes de contrato SSE (validar chunks contra schema OpenAI)
- [ ] Load test (`k6` ou `vegeta`) com Azure real
- [ ] Lint customizado bloqueando `unicode.IsLetter`/`IsDigit` em contextos onde a saída vai pra Postgres `text` (regressão da Onda 1)
- [ ] CHANGELOG.md auto-gerado a partir do git log com conventional commits
