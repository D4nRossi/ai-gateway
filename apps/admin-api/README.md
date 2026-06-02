# apps/admin-api — admin plane (.NET 10 + Dapper + DbUp)

Implementação .NET do admin plane do API Gateway (ADR-0027, ADR-0029).
Substitui o admin plane Go (`apps/gateway/internal/api/admin/`) slice a slice
sob `/admin/v1/*`. Stack pinada em [`CLAUDE.md §4.4`](../../CLAUDE.md).

## Estado atual (Fase 3 — esqueleto + Auth piloto)

Implementado:

- Minimal API .NET 10 com Serilog (JSON compacto pra stdout)
- Dapper + `Microsoft.Data.SqlClient` em `Infrastructure/Database/`
- DbUp como pipeline de migrations (`Infrastructure/Migrations/MigrationRunner.cs`)
  rodando como **CLI separado** (ADR-0025), bookkeeping em `gogateway.SchemaVersions`
- Os 12 scripts T-SQL atuais portados como `EmbeddedResource` em
  `Infrastructure/Migrations/Scripts/`
- Vertical slice **Auth** com paridade exata do handler Go:
  - `POST /admin/v1/auth/login` — bcrypt cost=12 + token opaco 32B hex
  - `DELETE /admin/v1/auth/logout`
  - `SessionAuthFilter` (endpoint filter equivalente ao middleware Go)
- `OpaqueTokenGenerator` — 32 bytes random → hex (`Convert.ToHexStringLower`)
  + SHA-256 hex pra persistir
- `AuditEventWriter` — INSERT em `gogateway.audit_events` paralelo ao Go
- Healthchecks `/healthz` (liveness) e `/readyz` (SELECT 1 em SQL)
- Dockerfile multi-stage (`sdk:10.0` → `aspnet:10.0`)

Não implementado ainda (próximos PRs slices):

- Users / Applications / RotateKey+Grants / ProxyEndpoints / Targets /
  MigrateTargetToKV / Observability / Dashboard
- Tests (xUnit + Testcontainers — Fase 3.3)
- nginx routing (Fase 4)

## Estrutura de pastas

```
apps/admin-api/
├── APIGateway.Admin.sln
├── Directory.Build.props           (TargetFramework, Nullable, TWE)
├── Dockerfile + .dockerignore
└── src/
    └── APIGateway.Admin.Api/
        ├── APIGateway.Admin.Api.csproj
        ├── Program.cs              (Minimal API + CLI bifurcação migrate)
        ├── appsettings*.json
        ├── Features/
        │   └── Auth/
        │       ├── Login.cs
        │       ├── Logout.cs
        │       └── SessionAuthFilter.cs
        ├── Infrastructure/
        │   ├── Database/ConnectionFactory.cs
        │   ├── Migrations/
        │   │   ├── MigrationRunner.cs
        │   │   └── Scripts/001..012_*.up.sql
        │   ├── Crypto/OpaqueTokenGenerator.cs
        │   ├── Healthchecks/SqlServerHealthCheck.cs
        │   ├── Auditing/AuditEventWriter.cs
        │   └── Http/ApiError.cs
        └── Domain/
            └── Admin/
                ├── AdminUser.cs
                └── AdminSession.cs
```

## Operar em dev local

```bash
# Restaura + sobe o Kestrel em http://localhost:5070 (ou ASPNETCORE_URLS configurada)
cd apps/admin-api
dotnet run --project src/APIGateway.Admin.Api -- \
  --environment Development

# Aplica migrations (preserva ADR-0025: NÃO roda no startup do Kestrel)
ConnectionStrings__Gateway='Server=...;Database=AzureAI_Gateway_hom;...' \
  dotnet run --project src/APIGateway.Admin.Api -- migrate up

# Lista scripts embedded
dotnet run --project src/APIGateway.Admin.Api -- migrate status
```

## Container

```bash
# Build (contexto = apps/admin-api/)
docker build -t api-gateway-admin:dev apps/admin-api

# Migrations
docker run --rm \
  -e ConnectionStrings__Gateway="Server=...;Encrypt=true;..." \
  api-gateway-admin:dev \
  migrate up

# Web
docker run --rm -p 8080:8080 \
  -e ConnectionStrings__Gateway="Server=...;Encrypt=true;..." \
  -e ASPNETCORE_ENVIRONMENT=Production \
  api-gateway-admin:dev
```

## Connection string esperada

Padrão `Microsoft.Data.SqlClient` — `Encrypt=true`, `TrustServerCertificate=false`,
`Application Name=api-gateway-admin`. Exemplo redacted:

```
Server=BRSPVPDEV003.tpb.corp;Database=AzureAI_Gateway_hom;User Id=usr_sist_AzureAI_Gateway_hom;Password=<from KV>;Encrypt=true;TrustServerCertificate=false;Application Name=api-gateway-admin
```

Em produção, valor vem do Key Vault `danieldev/AzureAIGateway-DB-Password-hom`
(ADR-0018) resolvido fora do container e injetado como env var.

## Paridade com gateway Go

Toda slice migrada deve manter:

| Item | Paridade obrigatória |
|---|---|
| Schema SQL | Mesmas tabelas/colunas/tipos — DbUp mantém os mesmos scripts |
| Response body | Mesmo JSON shape, mesmas chaves, mesmo casing |
| Token shape | 64 hex chars (raw + hash) |
| bcrypt | cost=12, `BCrypt.Net.BCrypt.Verify` |
| Audit | `event_type` mesma taxonomia (`admin_login_succeeded`, etc.) |
| Errors | Envelope `{ "error": { "code", "message", "details? } }` |

Quando uma slice for migrada, a rota correspondente é **removida** do Go no
mesmo PR e o nginx terminator (`infra/nginx/`) passa a despachar pro container
.NET. Frontend não muda.

## Referências

- [ADR-0027](../../docs/adrs/0027-admin-plane-dotnet-dapper.md) — esta migração
- [ADR-0011](../../docs/adrs/0011-admin-auth-opaque-sessions.md) — sessões opacas
- [ADR-0024](../../docs/adrs/0024-usage-tracking-proxy-plane.md) — audit taxonomy
- [ADR-0025](../../docs/adrs/0025-migrations-auto-apply-toggle.md) — migrations CLI
- [ADR-0030](../../docs/adrs/0030-monorepo-particionamento-apps.md) — layout monorepo
- Plano: `/home/daniel/.claude/plans/n-o-quero-usa-ef-glowing-locket.md`
