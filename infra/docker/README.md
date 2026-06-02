# infra/docker — Compose multi-app

Orquestracao de container do API Gateway (ADR-0030). Sobe quatro servicos:
nginx (terminator) + gateway (Go) + console (SPA) + admin-api (.NET,
comentado durante transicao).

## Operacao basica

```bash
cd infra/docker
cp .env.example .env
nano .env                       # preencher KEYVAULT_URI, AZURE_*, etc.

# Build + start
docker compose up -d --build

# Status
docker compose ps
docker compose logs -f nginx
docker compose logs -f gateway

# Reload nginx sem downtime (apos editar nginx.conf)
docker compose exec nginx nginx -t
docker compose exec nginx nginx -s reload

# Tear down (preserva volumes — nao tem volumes aqui)
docker compose down

# Tear down completo (apaga images locais)
docker compose down --rmi local
```

## Pre-requisitos no host

- Docker Engine 24+ ou Podman 4+ com `podman compose`
- Conectividade VPN corp (SQL Server em BRSPVPDEV003.tpb.corp)
- Acesso ao Azure Key Vault (Managed Identity ou Service Principal)
- Certs TLS em `/etc/api-gateway/certs/` (ver `../nginx/certs/README.md`)
- Portas 80 + 443 livres no host

## Topologia de rede

```
Internet/Intranet
       │
       │ 443 (HTTPS) / 80 (redirect)
       ▼
    nginx ──────────────────────────┐
       │                            │
       │ proxy_pass http://         │
       │                            │
   ┌───┼─────────────────┬──────────┘
   │   │                 │
   ▼   ▼                 ▼
gateway:8080          console:80           admin-api:8080
   │                                       (transicao: NAO ativo)
   │
   │ Microsoft.Data.SqlClient (1433)
   ▼
BRSPVPDEV003.tpb.corp  ← VPN corp
   │
   │ Azure SDK
   ▼
*.vault.azure.net      ← Managed Identity ou SP
*.cognitiveservices.azure.com
```

## Healthchecks

| Servico | URL interna | Frequencia | Falha = ? |
|---|---|---|---|
| nginx | `http://localhost/__nginx_healthz` | 30s | restart=unless-stopped reinicia |
| gateway | `http://localhost:8080/healthz` | 30s (start_period=30s) | idem |
| console | `http://localhost/healthz` | 30s | idem |

## SSE (chat streaming)

`/v1/chat/completions` com `stream:true` usa Server-Sent Events. nginx ja esta
configurado em `nginx.conf` com:

- `proxy_buffering off` (nao bufferiza chunks)
- `proxy_read_timeout 3600s` (conexao longa OK)
- `proxy_set_header Connection ""` + `proxy_http_version 1.1`

Validar com:
```bash
curl -N -H "Authorization: Bearer <gwk_...>" \
     -H "Content-Type: application/json" \
     -d '{"model":"gpt-4.1-mini","messages":[{"role":"user","content":"oi"}],"stream":true}' \
     https://api-gateway.tpb.corp/v1/chat/completions
```

Eventos `data:` devem chegar progressivamente.

## Rolagem slice a slice da admin-api .NET

Estado em 2026-06-02 (ADR-0027 Fase 4d):

- **27 endpoints implementados** no admin-api .NET
- `admin-api` container **ativo** (build + start junto com os outros)
- nginx roteia **apenas `/admin/v1/auth/{login,logout}`** pro container .NET
- Demais paths `/admin/v1/*` continuam no gateway Go

Rolagem por slice (uma de cada vez, valida em homolog antes da proxima):

| Path | Container atual | Quando rolar |
|---|---|---|
| `/admin/v1/auth/login` | admin-api .NET ✅ | rolou hoje |
| `/admin/v1/auth/logout` | admin-api .NET ✅ | rolou hoje |
| `/admin/v1/users/*` | gateway Go | adicionar location no nginx + reload |
| `/admin/v1/applications/*` | gateway Go | idem |
| `/admin/v1/endpoints/*` | gateway Go | idem |
| `/admin/v1/usage` | gateway Go | idem |
| `/admin/v1/audit` | gateway Go | idem |
| `/admin/v1/budget` | gateway Go | idem |
| `/admin/v1/dashboard/*` | gateway Go | idem |

**Procedimento padrao pra rolar uma slice:**

1. Editar `infra/nginx/nginx.conf` — adicionar/mover `location /admin/v1/<slice>/`
   apontando pra `http://admin_api_upstream`
2. Smoke test em homolog com 1 chamada pra cada endpoint da slice
3. Comparar response shape (curl + jq) contra a versao Go
4. Validar audit_events: linha emitida pelo `admin-api` deve aparecer
5. `docker compose exec nginx nginx -t && nginx -s reload` (zero downtime)
6. Se quebrar, reverter mudanca e investigar

Quando **todas** as 9 slices estiverem rolando .NET, abrir PR pra Fase 5:
remover `apps/gateway/internal/api/admin/`, `internal/app/adminservice/`,
`cmd/admin-create/` do gateway Go.

## Migrations: ownership compartilhado durante a transicao

Hoje:

- **Gateway Go** roda golang-migrate no boot (`MIGRATIONS_AUTO_APPLY=true`)
  contra `gogateway.schema_migrations`
- **Admin-api .NET** roda DbUp via CLI separado (preserva ADR-0025) contra
  `gogateway.SchemaVersions`. Nao roda no boot do Kestrel

Os dois bookkeepings sao independentes. Migrations `.up.sql` sao idempotentes
(`IF OBJECT_ID IS NULL`/`IF NOT EXISTS`), entao rodar ambos contra o mesmo
schema nao quebra — primeiro a rodar aplica, segundo descobre que ja existe.

Pra aplicar migrations via .NET manualmente:

```bash
docker compose run --rm admin-api dotnet APIGateway.Admin.Api.dll migrate up
```

Quando Fase 5 desligar o gateway Go admin, a .NET passa a ser **dona unica**
do schema.

## Manutencao

```bash
# Limpar logs antigos
sudo find /var/log/api-gateway -name "*.log" -mtime +30 -delete

# Conferir conexao SQL Server do container
docker compose exec gateway sh -c 'wget -q -O- http://localhost:8080/readyz; echo'

# Rolling restart sem downtime (servicos com replicas > 1; nao se aplica
# nessa fase porque gateway/console rodam single-replica).
docker compose up -d --no-deps --build gateway
```

## Diferenca pro compose antigo

O `docker-compose.yml` antigo (raiz do repo, removido nesta fase) usava
Postgres + `DATABASE_URL=postgres://...`. Estava obsoleto desde ADR-0022
(migracao pra SQL Server) mas nunca foi atualizado. Este novo compose:

- Sem Postgres container (SQL Server eh externo)
- Sem `DATABASE_URL` (gateway.yaml usa `${kv:...}` resolvido em runtime)
- nginx + console + admin-api adicionados
- Bind mounts de certs e logs externalizados

## Referencias

- ADR-0022 — SQL Server corporativo
- ADR-0025 — MIGRATIONS_AUTO_APPLY toggle
- ADR-0027 — admin plane .NET (rolagem futura)
- ADR-0028 — frontend extraido do binario
- ADR-0030 — monorepo apps/
- `../nginx/nginx.conf` — config completa
- `../nginx/certs/README.md` — popular certs TLS
- `../../docs/deploy/oracle-linux.md` — guia OL 9 super detalhado
