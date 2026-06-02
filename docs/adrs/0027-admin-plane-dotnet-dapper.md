# ADR-0027: Migração do admin plane de Go para .NET 10 + Dapper

- **Status**: proposed
- **Date**: 2026-06-02
- **Decision makers**: Daniel (owner)
- **Consulted**: Claude Opus 4.7 (análise Richards/Ford em `KB/AI-Gateway/Melhorias-Arquitetura.md`)
- **Supersedes**: nenhum
- **Aplica-se a**: `internal/api/admin/*`, `internal/app/adminservice/*`, console React (consumidor)

## Context

O AI Gateway hoje é **1 quantum operacional** ([[KB/AI-Gateway/Melhorias-Arquitetura]] §1):
binário Go único contendo data plane `/v1/*`, admin plane `/admin/v1/*` e console
React embedado via `go:embed`. Diagnóstico Richards/Ford (Cap. 7 — [[Acoplamento
estatico vs dinamico]]) confirma: frontend sobe junto com binário; admin e data
plane compartilham processo, schema e ciclo de vida. Toda mudança em CRUD
configuracional força rebuild + redeploy do `gateway.exe`.

**Pain points concretos coletados de `docs/handoff.md` + `docs/roadmap.md`:**

- Mudança de CSS no console força redeploy do gateway
- Bug do dashboard ("Fase A polish") consumiu tempo de quem mexe no proxy
- Onda 4.5 (target credentials KV) precisou tocar resolver + UI + migration + CLI
  porque é cross-cutting num quantum único
- Time único na mesma base — não há separação de equipe pra evolução paralela

**Drivers organizacionais (Conway, Lei de Conway):**

Teleperformance Brasil tem time .NET corporativo muito mais numeroso que o time Go
(efetivamente o owner sozinho no Go). Manter admin plane em Go bloqueia evolução
porque qualquer feature nova depende exclusivamente do owner. Migrar admin pra
.NET permite uma squad .NET assumir manutenção quando o produto estabilizar.

**Driving characteristics** declaradas pelo owner em 2026-06-02 pra admin-api:

| Característica | Justificativa |
|---|---|
| **Availability** (replicas + LB futuros) | Admin não pode cair durante operação multi-tenant |
| **Performance / latência baixa** | UI responsiva; bench alvo p99 < 100ms |
| **Auditability** | Loggar todas as ações — todo mutator emite `audit_event` |
| **Security (camada extra)** | bcrypt cost=12 + AES-256-GCM + KV + TLS terminator + sessão opaca |

## Decision

Migrar o admin plane (27 endpoints sob `/admin/v1/*`) de Go para uma aplicação
**.NET 10** standalone, com **Dapper** como micro-ORM e **DbUp** como ferramenta
de migrations. Migração é **slice a slice** (não big-bang), com coexistência
via nginx routing durante a transição. Go permanece responsável pelo data
plane (`/v1/*`), emissão de `usage_events` + `audit_events`, lookup de
`applications` / `api_keys` / `application_endpoint_grants` no hot path
(direct DB read), resolver KV, mascaramento PII, streaming SSE.

**Estado-alvo: 2 quanta lógicos** com SQL Server compartilhado. Pelo critério
rigoroso de [[Quantum Arquitetural]] isso ainda é "1 quantum disfarçado de 2"
(acoplamento estático via DB), mas o ganho declarado é **organizacional**:
permite uma squad .NET assumir admin sem precisar de Go expertise. Evolução pra
3 quanta reais (schemas separados + eventos assíncronos) fica como Opção D
futura quando volume justificar.

**Stack fixada:**

| Componente | Versão | Função |
|---|---|---|
| .NET SDK | 10.0.x | Runtime/framework |
| `Dapper` | 2.x latest | Micro-ORM (SQL direto, sem abstrações) |
| `Microsoft.Data.SqlClient` | latest stable | SQL Server driver |
| `dbup-sqlserver` | latest stable | Migrations runner (lê SQL embedded) |
| `BCrypt.Net-Next` | 4.x | bcrypt cost=12 (paridade ADR-0011) |
| `Serilog.AspNetCore` + `Serilog.Sinks.Console` | latest | JSON structured logging |
| `Azure.Identity` | latest stable | KV auth (DefaultAzureCredential) |
| `Azure.Security.KeyVault.Secrets` | latest stable | Necessário pra `MigrateTargetToKV` |
| `Microsoft.AspNetCore.OpenApi` + `Swashbuckle.AspNetCore` | latest | Gera `contracts/admin-api.openapi.yaml` |
| `Microsoft.Extensions.Diagnostics.HealthChecks` | inbox | `/healthz`, `/readyz` |
| (tests) `xUnit` + `Testcontainers.MsSql` + `FluentAssertions` | latest | Integration tests |

**Padrão arquitetural: Vertical Slice.** Cada feature mora em
`Features/<Domain>/<Action>.cs` (ex.: `Features/Applications/Create.cs`). Cada
slice tem Request/Response/Handler no mesmo arquivo. Compartilha apenas
`Infrastructure/` (connection factory, DbUp runner, AES cipher, KV writer,
audit writer).

**Auth admin mantém status quo da ADR-0011** (sessão opaca + bcrypt, SHA-256 do
token persistido em `admin_sessions`). Sem JWT, sem Entra ID nessa fase — fica
pra ADR separada futura.

**Schema ownership: admin-api .NET é dono autoritativo.** As 12 migrations
T-SQL atuais são portadas como **arquivos `.sql` embedded** em
`Infrastructure/Migrations/Scripts/`. DbUp roda como **CLI separado**
(`dotnet apigateway-admin migrate up`), preservando ADR-0025
(`MIGRATIONS_AUTO_APPLY=false` em prod — DBA aplica manualmente). Go continua
com acesso read-only via Dapper-like queries em `internal/infra/mssql/` —
deixa de poder rodar `migrate up` por si só.

**Comunicação Go ↔ admin-api:** Direct DB read na V1. Hot path do Go (auth de
bearer token) continua lendo `applications` + `api_keys` direto do SQL —
mesma performance (~1-5ms). Conscientemente acoplado por schema entre os dois
quanta. Caminho de evolução pra V2 (cache local Go + eventos publicados pela
admin-api via Service Bus) fica documentado em [[Melhorias-Arquitetura]] §4.4
como Opção D — só quando volume ou multi-instance justificar.

**Sequência slice a slice** (Fase 3 do plano `/home/daniel/.claude/plans/n-o-quero-usa-ef-glowing-locket.md`):

1. Esqueleto + DbUp + Auth (Login/Logout) — slice piloto
2. Users
3. Applications (CRUD)
4. RotateKey + Grants
5. ProxyEndpoints (CRUD com validação ProviderConfig)
6. Targets (CRUD com AES-256-GCM — paridade exata ADR-0012)
7. MigrateTargetToKV (Azure KV SDK)
8. Observability (Usage/Audit/Budget — read-only views)
9. Dashboard (Timeseries + Breakdown — agregações T-SQL)

A cada slice: PR remove rota do Go, PR adiciona rota no .NET, nginx rola a
`location` correspondente. Frontend não muda — contratos REST preservados
byte-a-byte.

## Options considered

### Option 1: Status quo (1 quantum Go)
- **Pros**: zero esforço; estado provado funcionando; single binary
- **Cons**: Conway desalinhado; release coupling forte (qualquer mudança força redeploy do gateway); time único; admin não escala independentemente
- **Why not**: bloqueia evolução paralela; TP tem capacidade .NET subutilizada

### Option 2: Extrair só frontend, manter admin no Go
- **Pros**: quick win; resolve o pior do release coupling (UI redeploy independente); fração do esforço
- **Cons**: admin continua refém do owner Go; squad .NET continua sem participar
- **Why not chosen alone**: não atende o driver Conway. **Mas é aplicada em paralelo** via ADR-0028 — separar frontend é pré-requisito da migração de admin

### Option 3: Admin .NET com Entity Framework Core
- **Pros**: stack canônica .NET; tooling de migrations builtin; LINQ
- **Cons**: owner rejeitou em 2026-06-02 — preferência explícita por micro-ORM (Dapper) com SQL direto; menor overhead, controle total sobre queries; menos magia
- **Why not**: contra direção do owner

### Option 4: Admin .NET com Dapper + EF Migrations (sem DbContext de runtime)
- **Pros**: EF só pra gerar migrations; runtime usa Dapper
- **Cons**: duas ferramentas de schema management no mesmo projeto; conceitualmente confuso
- **Why not**: DbUp dá o mesmo benefício sem trazer EF como dependência

### Option 5: Admin .NET com Dapper + DbUp (CHOSEN)
- **Pros**: SQL puro nas migrations (paridade com os 12 .sql atuais); CLI separado preserva ADR-0025; Dapper sem EF; idempotente; integra com Resources embedded; padrão consolidado na comunidade .NET
- **Cons**: migrations rodam em runtime separado do app (precisa orquestrar no deploy)
- **Why chosen**: alinhado com a preferência do owner por micro-ORM e SQL declarativo

### Option 6: Admin .NET + schemas separados (Opção D rigorosa)
- **Pros**: 3 quanta reais (Richards/Ford); acoplamento dinâmico assíncrono entre Go e .NET via eventos; evolução independente verdadeira
- **Cons**: muito alto custo agora; precisa de Service Bus / RabbitMQ; sincronização eventual de `applications`/`api_keys` quebra hot path inicialmente; rework de auth lookup Go
- **Why not now**: declarado em [[Melhorias-Arquitetura]] §4.4 — fica pra V2 quando multi-instance for realidade ou volume justificar

### Option 7: Polirepo (cada quantum num repo separado)
- **Pros**: separação clara; CI rápido por repo; aderente a estrutura de squad
- **Cons**: atomic changes em contratos (REST + schema) viram 3 PRs ordenados; complica enquanto owner for único dev
- **Why not now**: monorepo é a escolha de fase (ADR-0030); polirepo só quando 2+ equipes assumirem

## Consequences

### Positive

- Squad .NET pode assumir admin sem precisar de Go expertise — Conway alinhado
- Frontend redeploy independente (Opção B aplicada em ADR-0028 antes desta)
- Admin scaleable horizontalmente (sessions já são SQL-backed em `admin_sessions` — totalmente stateless)
- Manutenibilidade ↑ (stack .NET é majoritário na TP; Dapper aceita SQL direto, sem mistério)
- DbUp + ADR-0025 mantidos: DBA continua aplicando schema manualmente em janela controlada
- Audit emission paralela (Go e .NET emitem) com mesmo schema e taxonomia — `audit_events` continua sendo a fonte única
- Stack moderna (.NET 10, Minimal API, Vertical Slice) — onboarding atrativo
- Caminho pavimentado pra Opção D (schemas separados + eventos) quando volume justificar

### Negative / Trade-offs

- **2 quanta lógicos mas 1 quantum técnico** — SQL compartilhado entre Go e .NET viola [[Quantum Arquitetural]] §7 rigorosamente; mudança de schema continua afetando ambos; conascência de schema entre os quanta
- Stack heterogêneo (Go + .NET) aumenta carga cognitiva enquanto owner é único dev (até squad .NET assumir)
- DbUp não tem rollback automático (down migrations) — owner aceita; já era assim com `golang-migrate` em prod (ADR-0025: prod só roda `up`)
- Sem cache no Go: hot path continua batendo SQL a cada request — performance preservada mas TTL+invalidação fica como follow-up
- Migração tem janela com 2 admin planes coexistindo (Go ainda serve algumas rotas, .NET serve outras) — risco de divergência de comportamento; mitigação: integration tests cobrem ambos

### Mitigations

- **Trade-off SQL compartilhado**: documentar em SPEC.md que é limitação consciente da V1; abrir item em `docs/roadmap.md` pra reavaliar quando admin virar multi-instance
- **Carga cognitiva stack heterogêneo**: CLAUDE.md §4 lista ambas stacks com versões pinadas; cada slice .NET porta literal do handler Go correspondente (mesmo INSERT, mesma validação, mesmo response shape)
- **Divergência durante coexistência**: cada slice merge fecha imediatamente a rota Go correspondente; nginx config no PR de migração rola a `location` no mesmo deploy
- **Falta de cache no Go**: fitness function alvo p99 do hot path < 50ms; se degradar, abrir ADR de cache (TTL 30s no `application_endpoint_grants` por exemplo)

## References

### Plano e contexto interno

- Plano aprovado: `/home/daniel/.claude/plans/n-o-quero-usa-ef-glowing-locket.md`
- Análise prévia: `KB/AI-Gateway/Melhorias-Arquitetura.md` — opções A/B/C/D/E discutidas
- Inventário de handlers admin: gerado por Explore agent em 2026-06-02 (relatório no chat)
- ADR-0010 — Proxy genérico (já suporta passthrough; admin .NET não precisa mudar schema pra aceitar REST não-IA)
- ADR-0011 — Admin auth opaca + bcrypt (mantida)
- ADR-0012 — AES-256-GCM target credentials (paridade obrigatória no port)
- ADR-0014 — Frontend embedado (superseded por ADR-0028)
- ADR-0015 — `domain`/`app`/`infra` layering Go (será complementada por reorg de domínio)
- ADR-0018 — Azure Key Vault (KV continua sendo provedor da admin .NET via `DefaultAzureCredential`)
- ADR-0020 — Target credentials no KV (`MigrateTargetToKV` handler migra com lógica preservada)
- ADR-0024 — Usage tracking no proxy plane (continua emitido pelo Go)
- ADR-0025 — `MIGRATIONS_AUTO_APPLY` toggle (preservada; DbUp roda via CLI separado)
- ADR-0028 — Frontend extraído do binário (companion ADR, mesma onda)
- ADR-0029 — Rebrand "API Gateway" (companion ADR)
- ADR-0030 — Monorepo `apps/`/`db/`/`contracts/`/`infra/` (companion ADR)

### Conceitos Richards/Ford (vault `KB/Arquitetura-Software`)

- [[Quantum Arquitetural - unidade de medida da arquitetura]] — §7 anti-padrão "microservices com banco compartilhado"
- [[Acoplamento estatico vs dinamico]] — base do diagnóstico "1 quantum"
- [[Conascencia - acoplamento entre componentes]] — conascência de schema entre quanta novos
- [[Particionamento tecnico vs de dominio]] — reorg Go por domínio é Opção E paralela
- [[Driving characteristics]] — 4 declaradas pelo owner
- [[Lei de Conway e Inverse Conway Maneuver]] — driver organizacional principal
- [[Componente library e service]] — admin .NET vira `service`, gateway Go fica `service` também
- [[Funcoes de Aptidao - fitness functions]] — fitness functions sugeridas pra cada slice

### Documentação externa autorizada

- .NET 10 Minimal API: https://learn.microsoft.com/en-us/aspnet/core/fundamentals/minimal-apis (resolver versão exata ao iniciar Fase 3)
- Dapper: https://github.com/DapperLib/Dapper
- DbUp: https://dbup.readthedocs.io/
- Microsoft.Data.SqlClient: https://learn.microsoft.com/en-us/sql/connect/ado-net/microsoft-ado-net-sql-server
- BCrypt.Net-Next: https://github.com/BcryptNet/bcrypt.net
- Serilog ASP.NET Core: https://github.com/serilog/serilog-aspnetcore
- Azure SDK for .NET (Identity + KeyVault.Secrets): https://learn.microsoft.com/en-us/dotnet/azure/sdk/
- Testcontainers .NET (SQL Server): https://dotnet.testcontainers.org/modules/mssql/
