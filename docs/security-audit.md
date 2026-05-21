# Security Audit — 2026-05-21

Auditoria executada antes do deploy de demo. Cobertura:

- Backend Go (auth, criptografia, SQL, logs, concorrência)
- Frontend React (token storage, XSS, CSP)
- Headers HTTP
- Surface de ataque (rate-limit, brute-force)
- Tratamento de erros e dados sensíveis

## TL;DR

| Severidade | Achados | Status |
|---|---:|---|
| Críticos | 0 | — |
| Altos | 1 | Corrigido (brute-force login) |
| Médios | 3 | Corrigidos (headers + Docker + scan bug) |
| Baixos / informativos | 5 | Documentados como aceitos para Fase 1 |

Build e `go vet ./...` limpos. `npm audit` retorna **0 vulnerabilities**.

---

## Achados detalhados

### 🟥 Alto — Brute-force no login (CORRIGIDO)

**Origem:** `/admin/v1/auth/login` aceitava tentativas ilimitadas por origem.
Combinado com bcrypt cost=12 (~250 ms/tentativa), um atacante conseguiria
testar ~14 mil senhas/hora por conexão paralela.

**Vetor:** atacante automatizado mira contas conhecidas (`admin`, `root`,
`daniel`, etc.) com dicionário.

**Fix aplicado:**
[`internal/api/admin/middleware/loginlimit.go`](../internal/api/admin/middleware/loginlimit.go) —
token bucket por IP (5 tentativas, refill 1 token/minuto). Tanto sucesso
quanto falha consomem token (impede oracle "ip-bloqueado vs senha-errada").
Retorna 429 com `Retry-After`.

**Limitação:** contador é per-processo. Em multi-replica, atacante pode
distribuir tentativas entre réplicas; mitigação em Redis fica para Fase 2
(ver ADR-0006).

---

### 🟧 Médio — Headers de segurança aplicados apenas no SPA (CORRIGIDO)

**Origem:** CSP / `X-Frame-Options` / `nosniff` viviam apenas em
`web/embed.go`. Endpoints JSON e `/healthz` saíam sem hardening.

**Fix aplicado:**
[`internal/api/middleware/securityheaders.go`](../internal/api/middleware/securityheaders.go) —
middleware global aplicado como camada mais externa do router. Garante que
mesmo respostas de erro (401, 429, 500) levam:

- `X-Content-Type-Options: nosniff`
- `X-Frame-Options: DENY`
- `Referrer-Policy: strict-origin-when-cross-origin`
- `Permissions-Policy: accelerometer=(), camera=(), geolocation=()…` (lock-down)
- `Strict-Transport-Security: max-age=15552000; includeSubDomains` *somente
  quando atrás de TLS* (detecta via `r.TLS` ou `X-Forwarded-Proto`)

CSP intencionalmente **não** é aplicada nos endpoints JSON — CSP só faz
sentido em documentos renderizados pelo browser. O SPA mantém sua CSP
estrita própria em `web/embed.go`.

---

### 🟧 Médio — Dockerfile + compose desatualizados (CORRIGIDO)

**Origem:**
- `Dockerfile` assumia que `web/dist` já existia (frontend buildado fora do
  container) → reprodutibilidade frágil
- `Dockerfile` runtime sem `ca-certificates` → TLS contra Azure pode falhar
- `docker-compose.yml` faltava `DB_ENCRYPTION_KEY` (campo novo, Bloco 2) →
  container não sobe
- Sem healthcheck no service `gateway`

**Fix aplicado:** Dockerfile reestruturado em 3 stages (Node → Go → runtime)
com cache de deps por stage. `ca-certificates` + `tzdata` instalados no
runtime. Compose passa `DB_ENCRYPTION_KEY` do `.env`, ambos serviços têm
healthcheck + restart policy.

---

### 🟧 Médio — Bug TIMESTAMPTZ scan em `string` (CORRIGIDO)

**Origem:** `cmd/admin-create/main.go` declarava `var createdAt string` mas
a coluna é `TIMESTAMPTZ`. pgx rejeita o scan binário com
`cannot scan timestamptz (OID 1184) in binary format into *string`.

**Fix aplicado:** remover o scan desnecessário (não usávamos o valor).
Auditoria grep nos demais repositórios confirmou que todos usam
`&domain.X.CreatedAt` apontando para `time.Time` — bug era isolado nesta
CLI.

---

### 🟦 Informativo — Token em sessionStorage (ACEITO)

**Decisão:** o token de sessão admin fica em `sessionStorage` (não
localStorage, não HttpOnly cookie).

**Trade-off:**
- ✅ Limpa ao fechar a aba (window-scoped)
- ✅ Não vai pra outros tabs / extensions de cross-site
- ❌ XSS no SPA tem acesso ao token (mesmo destino de qualquer JS-readable
  storage)

Mitigações já em vigor:
- CSP estrita (`script-src 'self'`) bloqueia inline JS e CDN externos
- React 18 escapa interpolações por padrão; sem `dangerouslySetInnerHTML`
- Frontend é code-reviewed e bundled localmente (sem deps de runtime
  remotas)

**Caminho alternativo para Fase 2:** trocar para cookie `HttpOnly` +
`SameSite=Strict` + token CSRF separado. Custo: implementar CSRF no
backend (~150 LOC) e adaptar todos os fetches no front.

---

### 🟦 Informativo — Rate-limit per-process (ACEITO, ADR-0006)

Tanto `ratelimit.Manager` (por aplicação) quanto `LoginLimiter` (por IP) são
in-memory. Multi-replica precisa de Redis backend. ADR-0006 cobre isso.

---

### 🟦 Informativo — Least-connections balancer per-process (ACEITO, ADR-0013)

Balancer `least_connections` mantém contadores de in-flight por processo.
Em multi-replica cada réplica enxerga apenas seu próprio load. Documentado
em ADR-0013.

---

### 🟦 Informativo — Lookup de session/app a cada request (ACEITO)

Cada request no proxy plane faz 2 queries (api_key por prefix + application
por id). Cada request admin faz 1 query (session por token hash). Sem cache.

**Impacto:** para a demo (single-host, baixa concorrência) o overhead é
desprezível (<2ms por query com Postgres local + pool quente). Em escala
de produção (>1k RPS) o caching em memória com TTL curto (~30s) reduz
~99% da carga de leitura.

**Trade-off:** invalidação imediata de credenciais revogadas vs. latência.
Para demo, leitura sempre é a escolha segura.

---

### 🟦 Informativo — Senha mínima de 8 caracteres (ACEITO)

`cmd/admin-create` exige ≥8 caracteres mas não impõe complexidade
(maiúscula/dígito/símbolo). A política NIST SP 800-63B atual recomenda
comprimento sobre complexidade. Para Fase 2: integrar `zxcvbn` ou
equivalente para reject de senhas de dicionário.

---

### 🟦 Informativo — Senha do Postgres em compose (ACEITO em dev)

`POSTGRES_PASSWORD=gateway` é o default do compose. Aceitável em dev local
(o serviço também só expõe na rede do compose). Em produção, sobrescrever
via `POSTGRES_PASSWORD` no `.env` ou usar managed Postgres (RDS, Aurora,
Azure Database). Documentado em `docs/deployment.md`.

---

## Itens verificados sem achados

- ✅ Logs **nunca** carregam token completo, senha, prompt bruto, resposta
  bruta — só `key_prefix`, IDs, contadores e severidade. Grep confirma.
- ✅ Toda comparação de credencial usa `crypto/subtle.ConstantTimeCompare`
  (auth.go) ou `bcrypt.CompareHashAndPassword`. Nenhum `==` ou `bytes.Equal`.
- ✅ Todas as queries SQL usam parâmetros pgx (`$1, $2`). Nenhum
  `fmt.Sprintf` em SQL. Grep confirma.
- ✅ `go vet -race ./...` limpo.
- ✅ Toda função que faz I/O recebe `context.Context`. Nenhum
  `context.Background()` em handlers.
- ✅ Goroutines de I/O recebem ctx via `WithTimeout` (health probes) ou
  cancel via channel (writers de usage/audit).
- ✅ `defer cancel()` após todo `context.WithTimeout` (verificado).
- ✅ Erros sempre wrapped com `fmt.Errorf("contexto: %w", err)`.
  Grep não achou `_ = err` ou `_, _ := func()`.
- ✅ TargetAuth (creds upstream) criptografada com AES-256-GCM em repouso
  (ADR-0012), decifrada apenas em memória.
- ✅ Tokens admin: 32 bytes random → SHA-256 → DB. Raw nunca persistido.
- ✅ Migrations idempotentes via golang-migrate (`ErrNoChange`).
- ✅ `npm audit`: 0 vulnerabilidades.
- ✅ React 18.3 — escape automático nos templates JSX. Sem
  `dangerouslySetInnerHTML` no código.

---

## Validação executada

```bash
go vet ./...                # 0 issues
go vet -race ./...          # 0 issues
go build ./...              # OK
(cd web && npm audit)       # 0 vulnerabilities
(cd web && npm run build)   # OK (394 KB JS, 30 KB CSS)
```

## Próximos passos sugeridos (Fase 2)

1. Tokens em cookie `HttpOnly` + CSRF token
2. Redis-backed rate limiters (multi-replica)
3. Cache TTL para session/app/endpoint lookups
4. Métricas Prometheus em `/metrics`
5. Política de senha com `zxcvbn` ou similar
6. Rotação automática de `DB_ENCRYPTION_KEY` (envelope encryption com KMS)
