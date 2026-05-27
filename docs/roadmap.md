# Roadmap

Estado real do projeto e frentes em aberto, organizadas por **eixos estratégicos**. Sem datas — entregas são feitas conforme prioridade do owner.

> **Como ler este documento**
>
> - **§1 Estado atual** — o que está em produção e testável agora.
> - **§2 Em execução** — trabalho ativo no momento da última conversa.
> - **§3 Eixos estratégicos** — todas as outras frentes agrupadas por intenção (auditoria, desempenho, segurança, requisitos, dados, legalidade, escalabilidade). Cada item declara prioridade relativa (P1/P2/P3) e trade-offs conhecidos. Prioridade aqui é uma sugestão de ordem; o owner decide.
> - **§4 Decisões pendentes** — frentes anotadas mas que ainda precisam de definição de escopo antes de virar PR.
> - **§5 Backlog técnico** — itens não funcionais (cobertura, CI, infra de teste).
> - **§6 Histórico de ondas** — entregas anteriores indexadas com seus ADRs.

---

## 1. Estado atual (concluído)

Tudo abaixo está em produção no branch `v2`.

### Phase 1 — Gateway core de IA

| Componente | Notas |
|---|---|
| Bootstrap + graceful shutdown | `cmd/gateway/main.go`, SIGINT/SIGTERM |
| Config YAML + expansão `${VAR}` e `${kv:NAME}` | Fail-fast no boot |
| Auth Bearer + SHA-256 constant-time | ASCII-only prefix (Onda 1) |
| Rate limit token-bucket por app | `golang.org/x/time/rate` in-memory |
| Pipeline por tier (1/2/3) | Masking regex → Language PII → injection → CS → post-val |
| PII regex | CPF mod-11, CNPJ mod-11, cartão Luhn, e-mail, tel BR, CEP |
| Azure AI Language PII | Tier 2 fail-open, Tier 3 fail-closed (ADR-0019) |
| Injeção local (keywords) | 14 padrões, word-boundary |
| Azure Content Safety (opcional, Tier 3) | Prompt Shield + Text Analyze |
| Post-validação (Tier 3) | Scanner local na saída do modelo |
| Provider Azure OpenAI | Non-stream + SSE streaming |
| Provider Mock | Resposta determinística pra dev |
| Budget pre-check + counter (async) | UPSERT via canal |
| Usage / Audit events (async) | Worker em background |
| SQL Server (microsoft/go-mssqldb) + migrations T-SQL idempotentes | 9 migrations aplicadas no boot, schema dedicado `gogateway` (ADR-0022) |
| Streaming SSE | `WriteTimeout=0` (ADR-0008), `stream_cancelled` audit |

### v2 — Admin plane + Proxy plane

| Componente | Notas |
|---|---|
| Admin auth (bcrypt + sessões opacas) | ADR-0011 |
| Tabelas DB-backed `applications` + `api_keys` | UNIQUE parcial pós-bugfix 007 |
| CLI `cmd/admin-create` | Provisiona primeiro admin |
| Proxy plane `/v1/proxy/{slug}/*` | Engine genérico (ADR-0010) |
| Endpoints + targets cadastráveis | Credenciais AES-256-GCM (ADR-0012) |
| Load balancer (RR / weighted / least-conn / random / ip-hash) | ADR-0013 |
| Provider catalog (10 + custom) | ADR-0016 |
| Path translation por `provider_kind` | ADR-0017 (Onda 2) |
| Azure Key Vault como provider de segredos | ADR-0018 (Onda 3) |
| Console React+Vite embedado | ADR-0014 |
| Form de endpoint Azure com `provider_config` | Onda 5A |
| Playground modo canônico Azure + catálogo de exemplos | Onda 5B + 5F |
| Alert/Dialog corrigidos pra dark mode + perf | Onda 5C + 5D |
| Quality of life UI (search, refresh, Cmd+K, breadcrumbs) | Lote A do console |

### Bugfixes capturados em validação ao vivo

| Bug | Fix |
|---|---|
| Token UTF-8 quebrava Postgres SQLSTATE 22021 (legacy) | Onda 1: `ExtractPrefix` + `deriveKeyPrefix` ASCII-only (mantém-se válido em SQL Server pra cobrir NVARCHAR header roundtrip) |
| `${kv:NAME}` consumido por `os.ExpandEnv` antes do resolver KV | Hotfix Onda 3: `expandEnvPreservingKV` |
| `api_keys.application_id UNIQUE` bloqueia rotate | Migration 007: UNIQUE parcial `WHERE rotated_at IS NULL` |
| `TestValidate_ValidConfig` faltava `EncryptionKeyHex` | Fixture atualizada |
| `chatHandler` test helper sem Maskers → panic | Helper populado com 3 tiers |

---

## 2. Em execução

**Onda 6 — Latency Breakdown Observável (ADR-0021)** — código entregue,
testes/validação ao vivo pendentes. Ver `docs/handoff.md` pra passos
exatos de retomada amanhã.

Próximas sugeridas (depois da validação da Onda 6):
- **Onda 4.5** — Target credentials no Key Vault (§3.3, P1 Segurança)
- **Cache de lookup** (§3.1, P1 Desempenho)
- **Streaming Tier 3** (§3.1, P1 Desempenho)

---

## 3. Eixos estratégicos

Cada eixo lista frentes priorizadas e trade-offs. Prioridade é sugestão; ordem real depende do owner.

### 3.1 Desempenho

**Diagnóstico atual**: latência p50 ~1.7-3.3s pra Tier 2/3, dominada pelo Azure OpenAI (1.5-3s). Gateway adiciona ~150-310ms — principalmente o Azure Language PII (~150-300ms cloud call). Ganhos no gateway são em ordem de dezenas de ms.

**Pontos atacáveis (impacto real):**

- **P1 — Cache de policy/endpoint/grant lookup** (~5-10ms por request, baixo risco). Cada request hoje faz 2 DB hits pra resolver auth + grant. LRU+TTL em memória elimina ambos no cache hit. Invalidação: TTL curto (30-60s) ou pub/sub se virar multi-instance.
- **P1 — Streaming permitido em Tier 3**. Hoje bloqueado porque Content Safety não tem stream nativo. Opções: (a) buffer completo do response, CS check, flush — aumenta latência total mas mantém Tier 3 igual; (b) confiar no pré-check de prompt e liberar stream — semântica diferente, exige ADR.
- **P2 — Azure Language PII em paralelo com regex local**. Decisão da Onda 4 foi sequencial pra Language ver só o que regex perdeu. Em paralelo, latência total = max(local, cloud) ≈ Language sozinho → economiza os <1ms do regex. Trade-off: Language vê texto original (pode duplicar mascaramento). Revisão da decisão ADR-0019.
- **P2 — Connection warming pra Azure OpenAI**. Pré-abre conexões HTTP/2 no boot — evita TLS handshake de ~50-100ms no primeiro request por target. Simples (warmup goroutine).
- **✅ Latency breakdown observável (Onda 6, ADR-0021)** — entregue mas pendente de validação ao vivo. Header `X-Gateway-Latency-Breakdown: auth=2;mask=180;guardrails=0;provider=2400;encode=3` + 5 colunas em `usage_events`. Pré-requisito pra defender qualquer afirmação sobre latência com dado real.
- **P2 — Latency trace no log estruturado** (follow-up direto da Onda 6). Hoje o middleware `Logging` (`internal/api/middleware/logging.go`) loga `request_completed` com `latency_ms` total apenas; o trace só aparece no header de response e na tabela `usage_events`. Propagar `*LatencyTrace` via `r.Context()` (`observability.WithTrace`/`TraceFrom`) pra que o middleware enriqueça o log com os 5 buckets. Diff esperado: ~3 LOC no `chat.go` (injetar no contexto antes do `next`), ~5 LOC no `logging.go` (pickup + 5 campos no log). Ganho: validação de runtime e debug ad-hoc só com o stdout do GoLand, sem precisar entrar no Postgres. Não é otimização de latência — é redução de fricção operacional pós-Onda 6.
- **P3 — Semantic cache de respostas** (ver §3.7 Escalabilidade pra detalhes). Hash exato do payload (model + messages + temperature + etc.). Cache hit retorna em <10ms sem custo Azure. Trade-offs: complexidade (Redis), invalidação por mudança de modelo, risco de respostas "velhas". Frente grande.
- **P3 — Compression de payload outbound** (gzip response → cliente). Ajuda banda, não latência. Útil pra clientes mobile/edge. ~1-2h de trabalho. Fora do plano de redução de latência stricto sensu.

### 3.2 Auditoria

Tudo abaixo escreve em `audit_events` (ou tabela paralela).

- **P1 — Admin audit**. Hoje admin actions (criar app, criar endpoint, conceder grant, rotacionar chave, login/logout) **não emitem nada**. Página Observability fica vazia até alguém disparar request de chat. Decisão pendente: estender `audit_events` com colunas opcionais (`admin_username`, `target_type`, `target_id`) **ou** criar `admin_audit_events` separada. Eventos: `application_created/updated/deleted`, `endpoint_created/updated/deleted`, `grant_granted/revoked`, `key_rotated`, `admin_login/logout/login_failed`.
- **P2 — Per-target audit**. Qual target específico atendeu cada request (hoje só endpoint é registrado). Útil pra debug de load balancing e provider failover.
- **P2 — Bootstrap login audit**. CLI `admin-create` deveria registrar a criação do primeiro admin com IP/timestamp.
- **P3 — Audit imutável em Azure Blob**. Replicar `audit_events` pra storage append-only (LGPD + compliance). Job batch a cada N minutos.
- **P3 — Retention configurável por categoria** de evento (move pra §3.6 Legalidade).

### 3.3 Segurança

- **P1 — Onda 4.5 — Target credentials no Key Vault**. Resolve a quebra de targets quando `DB_ENCRYPTION_KEY` rotaciona (você viveu isso). Schema: nova coluna `proxy_targets.kv_secret_name TEXT NULL`. Quando preenchida, gateway lê credencial do KV em vez de decifrar AES local. Coexiste com modo legacy via fallback. Migração: CLI `cmd/migrate-targets-to-kv` move targets existentes em batch. Vira **ADR-0020**.
- **P2 — Validação sistemática de inputs**. Auditoria dos handlers admin (`internal/api/admin/handlers/`): comprimento máximo, sanitização de slug, validação de URL de target, validação de hex, defesa contra SQL injection (`database/sql` + `microsoft/go-mssqldb` parameter mode `@p1, @p2, ...` já cobre — confirmar 100%).
- **P2 — IP allowlist por aplicação**. Tabela `application_ip_allowlist`. Auth middleware rejeita com 403 se IP origem não está na lista (vazia = permite tudo).
- **P3 — mTLS upstream opcional**. Target ganha campo `client_cert_pem` cifrado (KV). Transport per-target em vez do shared.
- **P3 — 2FA TOTP pra admins**. Lote D do console-roadmap.
- **P3 — Secret rotation automation**. Gateway sabe rotacionar Azure key sem deploy quando KV detecta versão nova.

### 3.4 Requisitos

Contratos, validação e padrões de erro.

- **P2 — Payload size limits configuráveis por endpoint**. Hoje hard-coded em 1MB no chat. Endpoint Azure pode precisar mais; custom genérico pode aceitar menos.
- **P2 — Error response standardization**. Hoje `/v1/chat/completions` retorna `{"error":{"message":...,"type":...}}` (OpenAI-style) e `/v1/proxy/*` retorna `{"error":{"code":...,"message":...}}`. Decisão de design: unificar ou manter dois (proxy precisa ser passthrough do upstream).
- **P3 — Request signing opcional** (HMAC) pra apps de alto risco. Headers `X-Gateway-Signature` + `X-Gateway-Timestamp`. Anti-replay com timestamp window.
- **P3 — Schema validation de body** via JSON Schema declarado no endpoint. Útil pra endpoints custom não-IA.

### 3.5 Dados

Dashboards nativos + retenção.

- **P1 — Nova página Dashboard** com gráficos timeseries. Rota `/ui/dashboard`. Cards/charts (lib **recharts**, leve, sem CDN):
  - Requests/min nas últimas 24h (timeseries)
  - Latência p50/p95/p99 por aplicação (timeseries)
  - Tokens consumidos por modelo (stacked area)
  - Custo BRL acumulado por aplicação (bar chart)
  - Top 10 apps por gasto no mês corrente
  - Distribuição por tier (pie chart)
  - Taxa de erro (% 4xx/5xx) nas últimas 24h
  Tudo alimentado das tabelas existentes (`usage_events`, `audit_events`, `budget_counters`) via endpoints `/admin/v1/dashboard/*` novos. Sem Prometheus (decisão explícita do owner: "apenas logs no banco").
- **P2 — Snapshot diário de KPIs** em tabela agregada (`daily_metrics`). Dashboards leem do agregado em vez de scan de `usage_events` (queries em 30M+ linhas matam latência). Job de boot/cron.
- **P2 — Export CSV/JSON** de usage/audit/budget por filtro. Lote E do console-roadmap.
- **P3 — Particionamento mensal** de `usage_events` e `audit_events`. Move pra §3.7 Escalabilidade (impacto operacional).

### 3.6 Legalidade (LGPD / governance)

- **P1 — Retenção configurável** por categoria de evento. Config nova:
  ```yaml
  retention:
    usage_events_days: 365
    audit_events_days: 730
    chat_payload_in_audit_days: 30  # se for guardar
  ```
  Job de boot + cron deleta linhas expiradas.
- **P2 — Anonimização automática** após N dias. `application_name` em audit antigo vira hash; `metadata` é redacted. Permite manter agregados sem dados pessoais.
- **P2 — Right-to-be-forgotten endpoint**. `DELETE /admin/v1/applications/{name}/data` apaga TODOS os registros relacionados àquela app (usage/audit/budget). Audit log da própria deleção (DPO action).
- **P2 — DPO export endpoint**. `GET /admin/v1/applications/{name}/data-export` retorna JSON com todos os eventos da app no período. Auditável.
- **P3 — Documento de compliance map**. `docs/lgpd-compliance.md` mapeando que parte do gateway cobre qual artigo da LGPD (Art. 18 portabilidade, Art. 16 eliminação, etc.).

### 3.7 Escalabilidade

Multi-instance + altíssima carga.

- **P1 — Redis rate limiter**. Substitui `golang.org/x/time/rate` in-memory. Interface `ratelimit.Limiter` já existe; basta nova impl. Permite múltiplas réplicas do gateway sem que cada uma tenha seu próprio bucket.
- **P1 — Redis budget cache**. Pre-check de budget hoje faz query SQL por request. Em alta carga, cria pressão no DB. Cache com TTL 60s elimina.
- **P2 — Particionamento mensal** de `gogateway.usage_events` e `gogateway.audit_events`. SQL Server table partitioning por `created_at` (partition function + scheme). Queries em janelas curtas (dashboard 24h) ficam triviais; cleanup é SWITCH partition + drop staging.
- **P2 — Stateless garantido**. Auditar que nenhum estado fica só no processo além de cache LRU local (que é OK perder em restart). Pré-requisito pra autoscaling.
- **P2 — DB read replicas + pool tuning**. Connection pool por replica, leitura em replica pra queries de dashboard.
- **P3 — Semantic cache de respostas** (semantic = hash exato do payload, não embedding). Redis com chave = SHA256(model + messages + temperature + max_tokens + ...). TTL configurável. Cache hit: response em <10ms, custo Azure zero. Trade-offs: invalidação por mudança de versão de modelo (rare), risco de resposta "velha" (mitigado por TTL curto), custo de manutenção do cluster Redis.
- **P3 — Health checks robustos pra autoscaling**. `/readyz` já existe mas precisa: warmup detection (não responde ready até connection pool estar pronto), drain mode (responde 503 após SIGTERM enquanto drena conexões).

---

## 4. Decisões pendentes

Frentes anotadas mas precisam de definição antes de virar PR.

### 4.1 Desacoplamento do frontend (separação de repos)

**Pedido do owner**: tirar `web/` do repo do gateway, virar projeto próprio, deploy independente (CDN/S3/Vercel). Gateway expulsa o `go:embed dist`. Cliente puro REST.

**O que precisa ser definido:**
- Como o frontend descobre o endpoint do gateway? (env var no build? config runtime?)
- Como tratar CORS? Hoje o gateway aceita `localhost:5173` em dev — em prod precisa configurar via env
- Versionamento independente: frontend pode estar à frente/atrás do schema da API. Como negociar?
- Deploy: GitHub Pages, S3 + CloudFront, Vercel, Cloudflare Pages — escolher uma e documentar
- Repo: monorepo separado ou repo fresh? Histórico migra ou começa do zero?

**Trade-offs:**
- (+) Deploy do frontend não bloqueia release do gateway e vice-versa
- (+) Frontend pode ter sua própria CI (TypeScript-only, mais rápida)
- (+) Backend perde 460KB de bundle embedado — binário Go fica menor
- (-) Operação cresce: dois repos, dois pipelines, dois deploys
- (-) Em ambiente single-server, perde a vantagem do binário único que o ADR-0014 buscava

**Recomendação minha**: virar ADR-0021 quando virar prioridade. Sem urgência hoje.

### 4.2 Observabilidade externa (Prometheus / OpenTelemetry)

Owner foi explícito: **fora do escopo agora** ("apenas logs no banco"). Mantido aqui só pra referência futura — quando precisar de SLOs/alertas externos, será uma frente nova.

### 4.3 Cache de prompts (semantic cache)

Decisão do owner: **hash exato** (não embedding). Já listado em §3.7 como P3 com escopo detalhado. Definição do que falta:
- Tier do cache: por endpoint? por aplicação? global?
- TTL default
- Headers de cache control (`Cache-Control: no-cache` força bypass?)
- Métricas: hit rate, savings em $/mês

---

## 5. Backlog técnico

Itens não funcionais. Não bloqueiam features, mas reduzem dívida.

- [ ] `go test -coverprofile`, meta > 80% nos pacotes com lógica de negócio
- [ ] CI/CD GitHub Actions (lint + test + build + push ACR)
- [ ] `testcontainers-go` pra testes contra SQL Server real (`mcr.microsoft.com/mssql/server`)
- [ ] Testes de contrato SSE (chunks vs schema OpenAI)
- [ ] Load test (`k6` ou `vegeta`) com Azure real
- [ ] Lint customizado bloqueando `unicode.IsLetter`/`IsDigit` em contexto que escreve em coluna `text` (regressão da Onda 1)
- [ ] CHANGELOG.md auto-gerado a partir do git log com conventional commits

---

## 6. Histórico de ondas

Ondas entregues indexadas pelos ADRs que decidiram cada uma.

| Onda | Tema | ADR principal | Status |
|---|---|---|---|
| 1 | Hardening de tokens ASCII | (sem ADR — bugfix) | ✅ |
| 2 | Path translation por `provider_kind` | ADR-0017 | ✅ |
| 3 | Azure Key Vault como provider de segredos | ADR-0018 | ✅ |
| 4 | Azure AI Language PII | ADR-0019 | ✅ |
| 5 | UI: form Azure + playground canônico + catálogo + alert/dialog | (sem ADR — UI) | ✅ |
| 6 | Latency breakdown observável | ADR-0021 | ✅ código; ⏳ validação |
| 7 | Troca emergencial PostgreSQL → SQL Server (schema gogateway) | ADR-0022 | ✅ código; ⏳ smoke test contra BRSPVPDEV003 |
| 4.5 | Target credentials no Key Vault | ADR-0020 a fazer | ⏳ planejada |

**Notas sobre a Onda 7 (troca de banco)**:
- Trigger: requisito de infra corporativa em homologação (SQL Server gerenciado pela TI, sem espaço pra Postgres alternativo).
- Escala: ~30 arquivos tocados (driver, pool, migrate, 4 repos, 4 writers, 1 handler, 2 routers, 2 cmd, config, gateway.yaml, CLAUDE.md, ADR-0022, todas as migrations T-SQL).
- Migrations PG legacy preservadas em `migrations/postgres-legacy/` (referência histórica, não rodam).
- Schema dedicado `gogateway` qualificado em toda query (defesa em profundidade — o banco corporativo é compartilhado).
- Senha do user de serviço vive exclusivamente no Key Vault (`AzureAIGateway-DB-Password-hom`).
- Suite verde (vet/build/test-race) ao fim do código; smoke test ao vivo é o último passo pra fechar a Onda.

ADRs livres a partir de **0023**.
