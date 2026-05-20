# AI Gateway V2 — Documento de Alinhamento

> Gerado em: 2026-05-20  
> Status: **Aguardando confirmações finais antes de implementar**  
> Branch alvo: `v2` (ainda não criada)

---

## Contexto

O gateway Phase 1 (AI-only, YAML-backed) está funcionalmente completo e com build limpo.  
Este documento registra as decisões de arquitetura para a V2, que adiciona:
- Admin CRUD API (apps, chaves, endpoints via REST + banco)
- Generic HTTP Proxy (qualquer endpoint externo, não só AI)
- Frontend admin (React embebido no binário Go)

---

## Respostas do humano às perguntas de alinhamento

### A — Como proteger o admin?
**Resposta:** Auth separada no banco.  
**Interpretação:** Tabela `admin_users` (bcrypt) + sessões opacas (`admin_sessions`). Token de sessão de 32 bytes, hash armazenado no banco, expiry configurável. Sem JWT para não adicionar dependência.

### B — Gateway gera tokens ou operador traz o hash?
**Resposta:** Vamos pelo mais seguro, sem impactar muito a latência.  
**Interpretação:** Gateway gera `gwk_{prefix}_{32bytesRandom}`, exibe raw **uma única vez** na resposta do POST, armazena só o SHA-256. Latência zero no data plane — geração acontece só em create/rotate.

### C — Versionamento de chave (múltiplas chaves por app)?
**Resposta:** Não. Uma app, uma chave. Pode rotacionar, mas sempre uma por app.  
**Interpretação:** `api_keys` com FK para `applications`, UNIQUE por `application_id`. Rotação: gera nova → ativa → invalida anterior. Janela de 0ms de downtime (swap atômico).

### D — Métodos HTTP no proxy genérico?
**Resposta:** Todos. Precisamos ser agnósticos.  
**Interpretação:** Proxy completamente agnóstico ao método (GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS).

### E — Tipos de auth para o destino?
**Resposta:** Todos. Precisamos ser agnósticos.  
**Interpretação:** `none` / `bearer_token` / `api_key_header` (header + valor configurável) / `basic_auth` (user+pass). Credentials encriptados em repouso com AES-256-GCM via chave do env.

### F — Body verbatim ou com transformação?
**Resposta:** Qual o melhor caminho, mais seguro que não atrapalhe muito na latência.  
**Interpretação:** Streaming via `io.Copy` sem buffer intermediário. Sanitiza hop-by-hop headers (Connection, Transfer-Encoding, TE, Trailers, Upgrade) per RFC 7230. Substitui `Authorization` pela auth do destino. Adiciona X-Forwarded-For, X-Request-Id. Custo: ~zero de latência adicional.

### G — Streaming de resposta (incluindo voz)?
**Resposta:** Sim, streaming também, precisamos pensar em voz.  
**Interpretação:**
- SSE e chunked transfer: suportado nativamente pelo proxy verbatim.
- Voz HTTP binário (Azure Speech, ElevenLabs REST): suportado — são só bytes.
- **WebSocket**: protocolo diferente, requer decisão separada (ver Confirmações Pendentes).

### H — Guardrails no proxy genérico?
**Resposta:** Não sei onde se encaixariam no genérico, mas se precisar e não atrapalhar a latência, dá pra seguir.  
**Interpretação:** Guardrails ficam exclusivos do path `/v1/chat/completions`. O proxy genérico é transparente. Flag opcional por endpoint pode ser adicionada no futuro.

### I — Budget e usage para proxy genérico?
**Resposta:** Sim, contabilizar sem tokens. No cadastro: limite de requests, RPS, tipo de load balancing.  
**Interpretação:** Contabilizar `request_count`, `bytes_in`, `bytes_out`. Limites por endpoint: `max_rps`, `max_monthly_requests`. LB: plugável por estratégia (Round Robin, Weighted RR, Random, Least Connections, IP Hash).

### J — Refatorar estrutura toda ou evolutionary approach?
**Resposta:** Crie uma branch nova para a V2, vamos seguir isoladamente.  
**Interpretação:** Branch `v2` criada no início da implementação. Phase 1 continua em `main` intocada.

---

## Frontend — Decisão Confirmada ✅

**Stack:** React + Vite + TypeScript + TailwindCSS + shadcn/ui  
**Confirmado por:** Danirek em 2026-05-20

**Razão decisiva:** shadcn/ui tem DataTable com paginação/filtro server-side, Form com validação por schema, e Chart (Recharts) — exatamente o que precisamos. O build é estático e vai para `cmd/gateway/static/` → embebido no binário Go com `//go:embed static`. **Deploy continua sendo um único binário.**

**Estrutura:** projeto separado em `/web/` na raiz. Build artifact embebido.

**Features necessárias na UI:**
- CRUD de aplicações com geração de token (show-once modal)
- CRUD de endpoints proxy (targets, auth, LB config)
- Dashboard de uso (gráficos de requisições, custo, latência)
- Viewer de audit log com filtros
- Tracker de orçamento por aplicação

---

### Design System (sem genérico)

**Tema:** Dark-first, modo claro disponível. Admin dashboards vivem no escuro — reduz fadiga visual em operação.

**Paleta:**
- Background: `zinc-950` (#09090b) — mais rico que o cinza morto do shadcn padrão
- Surface: `zinc-900` (#18181b) para cards e sidebar
- Borda: `zinc-800` (#27272a) — sutil, não distrai
- Accent primário: `violet-500` (#8b5cf6) — autoridade sem ser genérico
- Texto primário: `zinc-50` (#fafafa)
- Texto secundário: `zinc-400` (#a1a1aa)
- Sucesso: `emerald-500` / Erro: `red-500` / Aviso: `amber-500`

**Tipografia:** Inter (carregado via Fontsource, não CDN — funciona offline no OnPrem)

**Layout:**
```
┌─────────────────────────────────────────────────────┐
│ ▪ AI Gateway          [notification] [user menu]     │ ← top bar 56px
├──────────┬──────────────────────────────────────────┤
│          │                                           │
│  sidebar │  content area (scroll independente)       │
│  200px   │                                           │
│  ─────   │  Page header (title + actions)            │
│  Dashboard│  ─────────────────────────────────────  │
│  Apps    │  Cards / Table / Chart                    │
│  Endpoints│                                          │
│  Usage   │                                           │
│  Audit   │                                           │
│  Budget  │                                           │
│          │                                           │
└──────────┴──────────────────────────────────────────┘
```

**Componentes-chave:**
- `DataTable` (TanStack Table + shadcn) com sort, filter, pagination server-side
- `ShowOnceModal` para exibir token gerado — campo copiável, aviso vermelho de "não mostramos de novo"
- `MetricCard` — número grande + trend (seta + %) + sparkline
- `AuditFeed` — feed cronológico estilo timeline com badges por `event_type`
- `BudgetGauge` — radial chart (Recharts) por app, cor muda em 75%/90%/100%

**Bibliotecas adicionais ao shadcn:**
- `@tanstack/react-table` v8 — DataTable
- `@tanstack/react-query` v5 — cache de chamadas à Admin API
- `react-router-dom` v7 — SPA routing
- `recharts` v2 — charts (já é dep transitiva do shadcn/chart)
- `@fontsource/inter` — tipografia offline
- `date-fns` — formatação de datas nos logs
- `zod` — validação de forms (já é dep do shadcn/form)

---

### Como rodar o frontend (guia para dev backend)

**Pré-requisitos — instalar uma vez:**

```bash
# 1. Node.js LTS via nvm (melhor que instalar direto no sistema)
curl -o- https://raw.githubusercontent.com/nvm-sh/nvm/v0.40.3/install.sh | bash
# feche e reabra o terminal, depois:
nvm install --lts
nvm use --lts
node --version   # deve ser 22.x ou 20.x

# 2. pnpm (gerenciador de pacotes — mais rápido e determinístico que npm)
npm install -g pnpm
pnpm --version   # deve ser 9.x ou 10.x
```

**Criar o projeto (feito uma vez pelo Claude Code no Bloco 4):**

```bash
# Na raiz do repo
pnpm create vite web --template react-ts
cd web
pnpm install
pnpm add tailwindcss @tailwindcss/vite
# shadcn e demais libs adicionados na hora da implementação
```

**Rodar em desenvolvimento (dois terminais):**

```bash
# Terminal 1 — backend Go (igual ao que você já faz)
go run ./cmd/gateway

# Terminal 2 — frontend React
cd web
pnpm dev
# Abre em http://localhost:5173
# Vite faz proxy de /api/* → http://localhost:8080 automaticamente
# Hot reload funciona — salvar um .tsx já recarrega no browser
```

**Build para produção (integrado ao Go):**

```bash
# 1. Build do frontend (gera web/dist/)
cd web && pnpm build

# 2. O Go pega o dist/ via go:embed (configurado no Bloco 4)
#    Nenhum passo extra — um único binário serve o frontend + API
go build ./cmd/gateway
./bin/ai-gateway
# Frontend disponível em http://localhost:8080/admin/
```

**Resumo mental para um backend dev:**
- `pnpm dev` = servidor de desenvolvimento com hot reload (só desenvolvimento)
- `pnpm build` = compila para arquivos estáticos (igual a `go build` para Go)
- O Vite é análogo ao compilador Go — você não o inclui no binário final
- `web/dist/` é análogo a `bin/ai-gateway` — é o artefato, não o código-fonte
- O `go:embed` costura os dois mundos: você roda `pnpm build` antes de `go build`

---

## Arquitetura V2 — Estrutura de diretórios

```
ai-gateway/
├── cmd/gateway/main.go            (mesmo, expandido)
│
├── internal/
│   ├── domain/                    (NOVO — entidades puras, sem deps externas)
│   │   ├── application/           Application, APIKey, TierLevel (value objects)
│   │   ├── endpoint/              ProxyEndpoint, Target, LBStrategy, TargetAuth
│   │   └── admin/                 AdminUser, AdminSession
│   │
│   ├── app/                       (NOVO — casos de uso / application services)
│   │   ├── adminservice/          CreateApp, RotateKey, CreateEndpoint, ...
│   │   └── proxyservice/          RouteRequest, SelectTarget, ApplyAuth
│   │
│   ├── infra/
│   │   ├── postgres/              (NOVO — repositórios pgx)
│   │   │   ├── applicationrepo.go
│   │   │   ├── endpointrepo.go
│   │   │   └── adminrepo.go
│   │   └── crypto/                (NOVO — AES-256-GCM para credentials em repouso)
│   │
│   ├── api/
│   │   ├── admin/                 (NOVO — rotas /admin/v1/...)
│   │   │   ├── router.go
│   │   │   ├── middleware/        admin auth middleware
│   │   │   └── handlers/          applications, endpoints, usage, audit, budget
│   │   └── gateway/               (atual internal/api — renomear)
│   │       ├── router.go
│   │       ├── handlers/          chat, models, health, proxy (NOVO)
│   │       └── middleware/
│   │
│   ├── proxy/                     (NOVO — engine do proxy HTTP genérico)
│   │   ├── director.go            header sanitization + auth injection
│   │   ├── loadbalancer.go        Round Robin, Weighted RR, Random, Least Connections
│   │   └── transport.go           http.RoundTripper com timeout por target
│   │
│   │   ... (pacotes atuais permanecem: auth, audit, budget, config, db,
│   │        observability, providers, ratelimit, security, tiers, usage)
│
├── web/                           (NOVO — frontend React)
│   ├── src/
│   └── dist/                      (embebido no binário via go:embed)
│
└── migrations/
    ├── 001_init.up.sql            (atual — sem alteração)
    ├── 002_admin_users.up.sql     (NOVO)
    ├── 003_applications_db.up.sql (NOVO — move apps do YAML pro banco)
    └── 004_proxy_endpoints.up.sql (NOVO)
```

---

## Rotas V2

### Admin plane (auth: session token de admin)

```
POST   /admin/v1/auth/login
DELETE /admin/v1/auth/logout

GET    /admin/v1/applications
POST   /admin/v1/applications          → gera token, retorna raw UMA VEZ
GET    /admin/v1/applications/{id}
PUT    /admin/v1/applications/{id}
DELETE /admin/v1/applications/{id}
POST   /admin/v1/applications/{id}/rotate-key

GET    /admin/v1/endpoints
POST   /admin/v1/endpoints
GET    /admin/v1/endpoints/{id}
PUT    /admin/v1/endpoints/{id}
DELETE /admin/v1/endpoints/{id}

GET    /admin/v1/usage?app=&from=&to=
GET    /admin/v1/audit?app=&type=&from=&to=
GET    /admin/v1/budget?period=
```

### Data plane (auth: bearer token de consumer app)

```
POST /v1/chat/completions              (existente — AI path)
GET  /v1/models                        (existente)
GET  /healthz                          (existente)
GET  /readyz                           (existente)

{METHOD} /v1/proxy/{endpoint_slug}     (NOVO — proxy genérico)
```

---

## Load Balancing — Estratégias

Baseado em System Design Interview (Alex Xu):

| Estratégia | Quando usar | Complexidade | V2? |
|---|---|---|---|
| **Round Robin** | Upstreams homogêneos | Mínima — contador atômico | ✅ |
| **Weighted Round Robin** | Upstreams com capacidades diferentes | Baixa — peso no config | ✅ |
| **Random** | Alta disponibilidade, baixo estado | Mínima | ✅ |
| **Least Connections** | Requests de duração variável (ex.: LLM calls) | Média — contador por target | ✅ |
| **IP Hash** | Sticky sessions necessárias | Média — hash do IP | ✅ |
| **Consistent Hashing** | Caches distribuídos | Alta | ❌ não aplicável |

**V2:** Round Robin + Weighted RR + Random + Least Connections + IP Hash (todos implementados in-memory).  
**V3:** Redis-backed Least Connections (quando múltiplas instâncias do gateway).

O operador configura por endpoint no CRUD. Default: `round_robin`.

---

## Schema do banco — Overview (sem DDL ainda)

Novas tabelas a criar nas migrations:

| Tabela | Propósito |
|---|---|
| `admin_users` | Credenciais de admin (bcrypt), role (`admin`/`operator`/`viewer`) |
| `admin_sessions` | Tokens de sessão opacas com hash + expiry |
| `applications` | Move do YAML pro banco (mantém os mesmos campos + `active`) |
| `proxy_endpoints` | Slug, name, lb_strategy, limites, ativo/inativo |
| `proxy_targets` | URLs de destino com peso e auth encriptada (AES-256-GCM) |
| `application_endpoint_grants` | Quais apps podem chamar quais endpoints |

**Roles de admin:**
- `admin` — gerencia outros admins + tudo
- `operator` — cria/edita apps e endpoints, não toca em outros admins
- `viewer` — somente leitura (logs, usage, budget)

---

## ADRs necessários antes de implementar

| ADR | Decisão |
|---|---|
| **ADR-0009** | Evolução para DB-backed admin plane (move apps do YAML) |
| **ADR-0010** | Generic HTTP proxy engine — verbatim forwarding + header sanitization |
| **ADR-0011** | Auth de admin — sessões opacas vs JWT |
| **ADR-0012** | Encryption at rest para target credentials (AES-256-GCM) |
| **ADR-0013** | Load balancing strategies — Round Robin + Weighted + Least Connections + IP Hash |
| **ADR-0014** | Frontend stack — React + Vite embebido no Go binary |
| **ADR-0015** | Reestruturação de `internal/` com camadas domain/app/infra |

---

## Sequência de trabalho (branch `v2`)

### Bloco 0 — Correções menores Fase 1 (em `main` antes do corte)
- Injetar `Masker` como dependência em `ChatDeps` (construído uma vez no bootstrap, compilação de regex não acontece por request)
- Confirmar interfaces `Emitter` corretas nos pacotes `usage`/`audit`

### Bloco 1 — Fundação V2 (branch `v2`)
- Criar ADRs 0009–0015
- Migrations 002–004
- Pacotes `internal/domain/` e `internal/infra/postgres/`

### Bloco 2 — Admin API
- `internal/api/admin/` com middleware de auth de admin
- CRUD completo de aplicações (com geração de token — show once)
- CRUD de endpoints proxy
- Roles de acesso (admin/operator/viewer)

### Bloco 3 — Proxy engine
- `internal/proxy/` — director, transport, load balancer
- Handler `{METHOD} /v1/proxy/{slug}` no router
- Accounting de requests/bytes no usage writer
- Todos os tipos de auth de destino (none, bearer, api_key_header, basic)
- Credentials encriptados em repouso

### Bloco 4 — Frontend
- Projeto `web/` com React + Vite + TS + TailwindCSS + shadcn/ui
- Integra com Admin API
- `go:embed static` no binário Go

### Bloco 5 — Integração e hardening
- Rate limiter dinâmico (recarrega do banco sem restart)
- Graceful reload de config (sem SIGKILL)
- Testes de integração do proxy engine

---

## Confirmações — todas resolvidas ✅

1. ~~**WebSocket na V2 ou V3?**~~ ✅ **V2 — suporte completo. Entra no Bloco 3 (proxy engine).**
2. ~~**Frontend React confirmado?**~~ ✅ **Confirmado: React + Vite + TS + Tailwind + shadcn/ui**
3. ~~**Três roles de admin**~~ ✅ **Confirmado: `admin` / `operator` / `viewer`**
4. ~~**Corrigir gaps Fase 1 primeiro?**~~ ✅ **Confirmado: corrigir em `main` antes de abrir `v2`**

---

## Gaps menores identificados na Fase 1 (não bloqueadores)

1. `masking.NewMasker(policy.Tier)` é recriado a cada request em `internal/api/handlers/chat.go:97` — deveria ser construído uma vez no bootstrap e injetado como dependência (compilação de regex ocorre por chamada).
2. `Masker` não está em `ChatDeps` — quebra o princípio de injeção de dependência do CLAUDE.md §14.
3. Esses dois pontos são o mesmo problema — correção simples, Bloco 0.

---

*Fim do documento. Retomar implementação após confirmação das 4 perguntas pendentes.*
