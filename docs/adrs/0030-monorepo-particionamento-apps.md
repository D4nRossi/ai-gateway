# ADR-0030: Monorepo particionado em `apps/`, `db/`, `contracts/`, `infra/`

- **Status**: proposed
- **Date**: 2026-06-02
- **Decision makers**: Daniel (owner)
- **Consulted**: Claude Opus 4.7
- **Supersedes**: nenhum (complementa ADR-0015)
- **Aplica-se a**: estrutura inteira do repo

## Context

O repo hoje é flat com Go ocupando a raiz: `cmd/`, `internal/`, `web/`,
`migrations/`, `configs/`, `docs/`, scripts de build na raiz. Funcionou
enquanto havia 1 quantum operacional ([[KB/AI-Gateway/Melhorias-Arquitetura]] §1).

A virada arquitetural de 2026-06-02 (ADRs 0027, 0028, 0029) introduz:
- 3 apps deployáveis (gateway Go, admin-api .NET, console React)
- Schema management movido pra admin-api .NET (mas migrations T-SQL legacy
  precisam ficar acessíveis como referência)
- Contratos versionados (OpenAPI gerado pela admin-api)
- Infra mudou de Windows/IIS pra Linux/Docker/nginx

O layout atual não comporta isso sem ficar confuso: onde mora o
`docker-compose.yml`? Onde fica o `.csproj` do .NET? Onde fica o nginx.conf?
Esta ADR define a estrutura nova.

## Decision

Adotar layout **monorepo** com 4 pastas top-level: `apps/`, `db/`,
`contracts/`, `infra/`. Cada `apps/<nome>/` é um deployable independente com
build/Dockerfile próprios.

```
api-gateway/
├── apps/
│   ├── gateway/          ← Go (data plane: /v1/* + healthz)
│   ├── admin-api/        ← .NET 10 + Dapper (admin plane: /admin/v1/*)
│   └── console/          ← React+Vite (SPA: /ui ou /)
├── db/
│   ├── migrations-go-legacy/  ← snapshot read-only das 12 migrations originais
│   └── schema.dbml             ← visualização ER (opcional)
├── contracts/
│   ├── admin-api.openapi.yaml ← gerado pelo Swashbuckle, comitado
│   └── README.md
├── infra/
│   ├── nginx/
│   │   ├── nginx.conf
│   │   └── certs/             ← .gitignore
│   └── docker/
│       ├── docker-compose.yml
│       ├── docker-compose.prod.yml
│       └── .env.example
├── docs/
│   ├── adrs/                  ← 0001..0030
│   ├── deploy/
│   ├── handoff.md
│   ├── how-it-works.md
│   ├── roadmap.md
│   └── SPEC.md
├── CLAUDE.md
├── README.md
└── SPEC.md
```

**Decisões pontuais:**

| Item | Decisão |
|---|---|
| Repo | Único (monorepo). Polirepo fica pra quando 2+ squads assumirem. |
| Path filter CI/CD | Fora de escopo desta ADR; quando reentrar CI, filters por `apps/*/` no GitHub Actions |
| `go.mod` | Move pra `apps/gateway/go.mod` (caminho de import muda pra `github.com/D4nRossi/api-gateway/apps/gateway/...`) |
| `.csproj`/`.sln` | Mora em `apps/admin-api/` |
| `package.json` | Mora em `apps/console/` |
| `migrations/` autoritativa | Move pra `apps/admin-api/src/APIGateway.Admin.Api/Infrastructure/Migrations/Scripts/` |
| `migrations/` legacy | Cópia read-only em `db/migrations-go-legacy/` pra referência histórica |
| Scripts antigos da raiz | `build-windows-deploy.sh`, `gateway-service.xml`, `gateway.yaml.deploy` movem pra `infra/legacy-windows/` (deprecated; preservados pra referência) |
| `configs/gateway.yaml` | Move pra `apps/gateway/configs/` (continua sendo lido pelo binário Go) |
| `docs/` | Fica top-level — não pertence a nenhuma app específica |
| `CLAUDE.md`, `SPEC.md`, `README.md` | Top-level (contratos do repo inteiro) |

**Ordem de execução (Fase 1 do plano):**

1. PR mecânico: `git mv` de `cmd/` + `internal/` + `configs/` pra `apps/gateway/`; ajustar imports + `go.mod`
2. PR: criar `apps/console/` movendo `web/` (sem `embed.go`)
3. PR: criar `apps/admin-api/` esqueleto + DbUp + slice piloto Auth
4. PR: criar `db/migrations-go-legacy/` com snapshot
5. PR: criar `infra/nginx/` + `infra/docker/` com docker-compose

Cada PR é isolado e revisável. Não há big-bang.

## Options considered

### Option 1: Manter flat (status quo) com `apps/` opcional
- **Pros**: zero esforço; menor disrupção
- **Cons**: 3 apps disputando raiz; `Dockerfile`, `docker-compose.yml`, `.csproj`, `package.json`, `go.mod` ficam todos no top-level; ilegível
- **Why not**: não escala pra multi-app

### Option 2: Layout DDD-like (`src/`, `tests/`, `tools/`)
- **Pros**: idioma da casa .NET
- **Cons**: amarra um único idioma stack; Go não usa `src/`; conflita com `apps/` interna
- **Why not**: 3 stacks diferentes; precisa de envelope agnóstico

### Option 3: Layout `services/`, `web/`, `db/`, `infra/` (Nx-style)
- **Pros**: padrão Nx; cada `services/<n>` é um microsserviço
- **Cons**: vocabulário "services" sugere quantum próprio que não temos (compartilhamos SQL); confunde com [[Componente library e service]]
- **Why not**: naming impreciso

### Option 4: Layout `apps/`, `db/`, `contracts/`, `infra/` (CHOSEN)
- **Pros**: `apps/` é neutro (não promete quantum); `db/` separa contrato de schema; `contracts/` separa contratos REST gerados; `infra/` consolida nginx + docker + (futuro) k8s; layout reconhecível por devs de qualquer stack
- **Cons**: muda paths conhecidos do projeto; PRs mecânicos
- **Why chosen**: melhor relação clareza/esforço; padrão hipster mas com semântica honesta

### Option 5: Polirepo (3 repos GitHub)
- **Pros**: separação física; CI rápido por repo
- **Cons**: 3 PRs ordenados pra cada mudança de contrato; complica review enquanto há 1 owner
- **Why not now**: monorepo é a escolha de fase; polirepo só quando 2+ squads assumirem

## Consequences

### Positive

- Layout escala pra 3+ apps sem disputa de raiz
- Cada `apps/<n>/Dockerfile` é declarativo e visível
- `db/migrations-go-legacy/` preserva história sem confundir com schema autoritativo
- `contracts/admin-api.openapi.yaml` vira artefato comitado e revisável em PR
- `infra/` consolida tudo de operação (nginx, docker compose, futuras configs k8s)
- Tools de cada stack continuam funcionando (Go `go.mod` por pasta, .NET `dotnet build` por solution, Vite `npm run build`)
- Layout permite extrair pra polirepo no futuro com `git filter-branch` por pasta

### Negative / Trade-offs

- Reorg disruptiva — todos paths conhecidos mudam de uma vez
- Import paths Go mudam (`github.com/D4nRossi/ai-gateway/internal/...` → `github.com/D4nRossi/api-gateway/apps/gateway/internal/...`)
- IDEs/Editores precisam reabrir workspace
- `docker compose` exige paths relativos cuidadosos (`../../apps/admin-api`)

### Mitigations

- PRs mecânicos: 1 PR move código sem mudar lógica + 1 PR ajusta imports — diffs revisáveis
- Documentar paths antigos vs novos em `README.md` no commit do rename
- IDE workspaces (GoLand, Rider, VSCode) ganham `.code-workspace` ou config equivalente comitada em `infra/` pra setup rápido
- `docker compose` ganha helper script `infra/docker/up.sh` que faz `cd infra/docker && docker compose up`

## References

- ADR-0015 — `internal/` em domain/app/infra (esta ADR complementa: domain/app/infra continua dentro de `apps/gateway/internal/`)
- ADR-0027 — Admin .NET (consumidor desta estrutura)
- ADR-0028 — Frontend extraído (consumidor desta estrutura)
- ADR-0029 — Rebrand API Gateway (motiva renomear pasta raiz quando possível)
- Plano: `/home/daniel/.claude/plans/n-o-quero-usa-ef-glowing-locket.md` §"Estrutura final do monorepo"
- [[KB/AI-Gateway/Melhorias-Arquitetura]] §"Opção C" e §"Opção E"
- Monorepo patterns no Go: https://go.dev/ref/mod#workspaces (workspaces opcionais se precisar)
- nx monorepo conventions (referência conceitual): https://nx.dev/concepts/decisions/why-monorepos
