# apps/admin-api — admin plane (.NET 10 + Dapper + DbUp)

Implementação .NET do admin plane do API Gateway (ADR-0027, ADR-0029).
Substitui o admin plane Go (`apps/gateway/internal/api/admin/`) slice a slice
sob `/admin/v1/*`. Stack pinada em [`CLAUDE.md §4.4`](../../CLAUDE.md).

## Estado atual (Fase 3 completa — 27/27 endpoints, Fase 4d nginx routing)

Implementado (atualizado 2026-06-02):

- Minimal API .NET 10 com Serilog (JSON compacto pra stdout)
- Dapper + `Microsoft.Data.SqlClient` em `Infrastructure/Database/`
- DbUp como pipeline de migrations (`Infrastructure/Migrations/MigrationRunner.cs`)
  rodando como **CLI separado** (ADR-0025), bookkeeping em `gogateway.SchemaVersions`
- Os 12 scripts T-SQL atuais portados como `EmbeddedResource` em
  `Infrastructure/Migrations/Scripts/`
- **AES-256-GCM cipher** (`AesGcmCipher`) com paridade BIT-A-BIT ADR-0012
  (nonce 12B + ciphertext + tag 16B, AAD null) + `TargetAuthPlaintext`
  com custom converter pra ordem PascalCase paridade Go json.Marshal
- **Azure Key Vault writer** condicional (`IKeyVaultSecretWriter`) via
  `DefaultAzureCredential` — registrado quando `KeyVault:Uri` setado;
  stub `UnavailableKeyVaultWriter` retorna 503 quando não
- **`OpaqueTokenGenerator`** — 32B random → hex + SHA-256 hex (paridade Go)
- **`KeyPrefixDeriver`** — algoritmo bit-a-bit `gwk_{name24}` com Go
- **`AuditEventWriter`** — INSERT em `gogateway.audit_events` paralelo
- **`SessionAuthFilter`** + **`RequireRoleFilter`** — auth + RBAC viewer/operator/admin
- **`ProviderConfigValidator`** — validação per-kind (azure_openai exige
  api_version + model_to_deployment)
- Healthchecks `/healthz` (liveness) e `/readyz` (SELECT 1)
- Dockerfile multi-stage (`sdk:10.0` → `aspnet:10.0`) — instala wget
  pra HEALTHCHECK Docker

### 9 slices, 27 endpoints

| Slice | Endpoints | Role |
|---|---|---|
| **Auth** | POST `/auth/login`, DELETE `/auth/logout` | anônimo / sessão |
| **Users** | GET / POST `/users`, DELETE `/users/{id}` | admin |
| **Applications** | List / Get / Create / Update / Delete | operator |
| **Applications RotateKey + Grants** | POST `/{id}/rotate-key`, GET `/{id}/grants`, POST/DELETE `/{id}/grants/{epID}` | operator |
| **Endpoints** | List / Get / Create / Update / Delete | operator |
| **Targets** | POST `/{id}/targets`, PUT / DELETE `/{id}/targets/{tid}`, POST `/{id}/targets/{tid}/migrate-to-kv` | operator |
| **Observability** | GET `/usage`, GET `/audit`, GET `/budget` | viewer |
| **Dashboard** | GET `/dashboard/timeseries`, GET `/dashboard/breakdown` | viewer |

### Tests scaffold (Fase 3.y)

- `tests/APIGateway.Admin.UnitTests/` — xUnit + FluentAssertions, sem
  container. Cobre cipher, token, key-prefix, validators, role filter,
  JSON converter (~40 tests)
- `tests/APIGateway.Admin.IntegrationTests/` — Testcontainers.MsSql 2022
  + `WebApplicationFactory<Program>` + `SqlServerFixture` rodando DbUp.
  Piloto: 6 tests E2E do slice Auth

### Runtime status

- nginx (`infra/nginx/nginx.conf`) roteia **`/admin/v1/auth/{login,logout}` pra cá**
- Demais `/admin/v1/*` continuam no gateway Go até **Fase 5** desligar admin Go
- Migration ownership compartilhado durante a transição (Go aplica via
  golang-migrate; .NET via DbUp CLI separado, bookkeeping isolado)

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

### ⚠️ Primeira execução de `migrate up` durante a transição

Durante a transição (gateway Go ainda no ar com `MIGRATIONS_AUTO_APPLY=true`),
o DbUp do admin-api **sempre vai dizer "applied 12 scripts"** na primeira
execução — não é bug:

- Gateway Go aplica via `golang-migrate` e marca em `gogateway.schema_migrations`
- admin-api .NET aplica via DbUp e marca em `gogateway.SchemaVersions`
- **Os dois bookkeepings são independentes** — a primeira execução do .NET
  não vê que o Go já aplicou tudo, então tenta tudo de novo
- As 12 migrations são **idempotentes** (`IF OBJECT_ID IS NULL`,
  `IF NOT EXISTS`, `IF COL_LENGTH IS NULL`) — os SQLs rodam mas viram noop

Resultado esperado no log: 12 scripts "applied" + zero impacto no schema.
Quando Fase 5 desligar o admin Go, o admin-api .NET vira dono único.

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
