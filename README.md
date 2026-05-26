# AI Gateway

Gateway HTTP corporativo em Go que media tráfego entre aplicações internas e Azure OpenAI (Azure AI Foundry). Aplica autenticação por Bearer token, políticas por aplicação, mascaramento de PII/PCI, rate limit, controle de budget e auditoria estruturada.

## Visão rápida

```
Aplicação interna
       │  POST /v1/chat/completions
       │  Authorization: Bearer gwk_<prefix>_<secret>
       ▼
┌─────────────────────────────────────┐
│            AI Gateway               │
│  Auth → Rate limit → Tier pipeline  │
│  → Budget check → Provider call     │
│  → Usage/Audit (async)              │
└─────────────────────────────────────┘
       │                    │
       ▼                    ▼
  Azure OpenAI          PostgreSQL
```

## Pré-requisitos

| Ferramenta | Versão mínima | Necessária para |
|---|---|---|
| Go | 1.25+ | backend |
| Docker | 24+ | Postgres local |
| Docker Compose | v2 | Postgres local |
| PostgreSQL | 17 (via Docker) | banco |
| Node.js | 20+ | console web (`web/`) |
| npm (ou pnpm 9+) | que vier com Node 20+ | console web |

Funciona em **Linux, macOS, WSL2 e Windows nativo**. Para instruções específicas
por SO (PowerShell, GoLand, WebStorm, gotchas do `curl` no Windows etc.), veja
[`docs/local-development.md`](docs/local-development.md).

## Console web (admin UI)

O console React+Vite vive em `web/` e é embedado no binário Go via `//go:embed`
(ADR-0014). Para desenvolvedores backend, o fluxo é:

```bash
# 1. Instalar dependências e gerar o bundle
cd web
npm install
npm run build                    # gera web/dist/

# 2. Voltar para a raiz e (re)buildar o Go
cd ..
go build ./cmd/gateway

# 3. Subir o gateway — UI fica em http://localhost:8080/ui
PROVIDER=mock ./gateway          # Linux/macOS/WSL
# ── ou no Windows PowerShell ──
# $env:PROVIDER="mock"; .\gateway.exe
```

> Prefere `pnpm`? `corepack enable pnpm` e troque `npm` por `pnpm` nos
> comandos acima — o `package.json` é compatível.

Modo de desenvolvimento com hot reload (rode em terminal separado):

```bash
cd web && npm run dev            # Vite em http://localhost:5173
                                 # proxia /admin e /v1 para o Go em :8080
```

### Criar o primeiro admin

O banco começa sem nenhum usuário admin — cada deploy define sua própria
credencial inicial. Use a CLI dedicada:

**Linux / macOS / WSL:**

```bash
# carrega DATABASE_URL e demais vars
set -a && source .env && set +a

# pergunta a senha sem ecoar (e pede confirmação)
go run ./cmd/admin-create -username daniel -role admin
```

**Windows PowerShell:**

```powershell
# carrega .env no processo atual
Get-Content .env | ForEach-Object {
  if ($_ -match '^\s*([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*)$') {
    [Environment]::SetEnvironmentVariable($matches[1], $matches[2], 'Process')
  }
}

go run ./cmd/admin-create -username daniel -role admin
```

Depois disso, faça login em `http://localhost:8080/ui/login` e crie os demais
usuários pela própria UI (papel `admin` pode gerenciar usuários, `operator`
pode CRUD de aplicações/endpoints, `viewer` é só leitura).

> No GoLand, prefira rodar pela Run Configuration — ela carrega o `.env`
> automaticamente e evita o ritual acima. Detalhes em
> [`docs/local-development.md` §3](docs/local-development.md#3-rodar-via-goland-e-webstorm-recomendado).

## Início rápido (modo mock — sem Azure)

**Linux / macOS / WSL:**

```bash
# 1. Subir o banco
docker compose up -d postgres

# 2. Copiar e configurar variáveis
cp .env.example .env

# 3. Carregar variáveis no shell
set -a && source .env && set +a

# 4. Rodar com provider mock
PROVIDER=mock go run ./cmd/gateway

# 5. Testar (em outro terminal)
curl -s http://localhost:8080/healthz

curl -s -X POST http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer gwk_basic_k9mxqr7tz2wn3vfp" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-4.1-nano","messages":[{"role":"user","content":"Olá!"}]}'
```

**Windows PowerShell:**

```powershell
# 1. Subir o banco
docker compose up -d postgres

# 2. Copiar e configurar variáveis
Copy-Item .env.example .env

# 3. Carregar variáveis no processo atual
Get-Content .env | ForEach-Object {
  if ($_ -match '^\s*([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*)$') {
    [Environment]::SetEnvironmentVariable($matches[1], $matches[2], 'Process')
  }
}

# 4. Rodar com provider mock
$env:PROVIDER = "mock"
go run ./cmd/gateway

# 5. Testar (em outro terminal — `curl` puro do PowerShell NÃO funciona, use curl.exe)
curl.exe http://localhost:8080/healthz

curl.exe -X POST http://localhost:8080/v1/chat/completions `
  -H "Authorization: Bearer gwk_basic_k9mxqr7tz2wn3vfp" `
  -H "Content-Type: application/json" `
  -d '{\"model\":\"gpt-4.1-nano\",\"messages\":[{\"role\":\"user\",\"content\":\"Olá!\"}]}'
```

## Aplicações de teste disponíveis

Três aplicações genéricas pré-configuradas em `configs/gateway.yaml`:

| App | Token (dev) | Tier | Modelos | Streaming | RPM |
|---|---|---|---|---|---|
| AppBasico | `gwk_basic_k9mxqr7tz2wn3vfp` | tier_1 | gpt-4.1-nano | não | 600 |
| AppPro | `gwk_pro_n4vwlp8fy6hkjcqm` | tier_2 | mini + nano | sim | 300 |
| AppVault | `gwk_vault_j3hsbn2cq1xdtzer` | tier_3 | gpt-4.1-mini | não | 60 |

> **Atenção:** esses tokens são de desenvolvimento/homologação. Para produção, gere tokens novos com `openssl rand -hex 24`.

## Documentação completa

| Documento | Conteúdo |
|---|---|
| [Como funciona](docs/how-it-works.md) | Arquitetura, fluxo de request, mapa de pacotes |
| [Desenvolvimento local](docs/local-development.md) | Setup detalhado, tokens, testes manuais de cada endpoint |
| [Suite de testes](docs/testing.md) | Como rodar testes, benchmarks, o que cada arquivo cobre |
| [Deploy em produção](docs/production-deploy.md) | Docker, variáveis, segurança, checklist Azure |
| [Manutenção](docs/maintenance.md) | Adicionar apps, rotacionar chaves, migrations, SQL de monitoramento |
| [Azure Key Vault](docs/keyvault-setup.md) | Setup do KV, permissionamento, sintaxe `${kv:NAME}` no YAML |
| [Roadmap](docs/roadmap.md) | Estado atual, ondas em execução, frentes futuras |
| [ADRs](docs/adrs/) | Decisões arquiteturais registradas (ADR-0001 a ADR-0018) |

## Endpoints

```
GET  /healthz                  → 200 sempre (liveness)
GET  /readyz                   → 200 se DB + Azure ok, 503 caso contrário (readiness)
GET  /v1/models                → modelos da aplicação autenticada
POST /v1/chat/completions      → chat (stream e non-stream, compatível OpenAI)
```

## Variáveis de ambiente

| Variável | Obrigatória | Descrição |
|---|---|---|
| `DATABASE_URL` | Sim | `postgres://gateway:gateway@localhost:5432/gateway?sslmode=disable` |
| `AZURE_OPENAI_ENDPOINT` | Sim* | Endpoint Azure OpenAI (ex: `https://nome.cognitiveservices.azure.com`) |
| `AZURE_OPENAI_API_KEY` | Sim* | Chave da API Azure OpenAI |
| `AZURE_CS_ENDPOINT` | Não | Endpoint Content Safety (Tier 3) |
| `AZURE_CS_API_KEY` | Não | Chave Content Safety (Tier 3) |
| `PROVIDER` | Não | `azure` (padrão) ou `mock` (sem Azure) |
| `CONFIG_PATH` | Não | Caminho do YAML (padrão: `configs/gateway.yaml`) |

*Não necessárias com `PROVIDER=mock`.

## Comandos úteis

Cross-platform (idênticos no Linux/macOS/Windows):

```bash
# Infra
docker compose up -d postgres       # sobe só o banco
docker compose up                   # sobe banco + gateway em container

# Testes e build
go test ./...                       # rodar toda a suite de testes
go test -race ./...                 # rodar com detector de race conditions
go test -bench=. -benchmem ./...    # rodar benchmarks com memória
go build ./cmd/gateway              # binário local (gateway / gateway.exe)
docker build -t ai-gateway:dev .

# Migration manual (precisa do migrate CLI: github.com/golang-migrate/migrate)
migrate -database "$DATABASE_URL" -path migrations up
migrate -database "$DATABASE_URL" -path migrations down 1
```

Específicos por SO (rodar gateway sem Azure):

```bash
# Linux / macOS / WSL
PROVIDER=mock go run ./cmd/gateway
```

```powershell
# Windows PowerShell
$env:PROVIDER = "mock"; go run ./cmd/gateway
```

Build de release Linux estático (para imagem Docker), funciona dos dois SOs:

```bash
# Linux / macOS
CGO_ENABLED=0 GOOS=linux go build -o bin/ai-gateway ./cmd/gateway
```

```powershell
# Windows PowerShell — cross-compilando para Linux
$env:CGO_ENABLED = "0"; $env:GOOS = "linux"; go build -o bin/ai-gateway ./cmd/gateway
Remove-Item Env:CGO_ENABLED, Env:GOOS   # limpa depois pra não vazar pras próximas builds
```

Gerar token + hash para nova aplicação: veja
[`docs/local-development.md` §10](docs/local-development.md#10-gerar-um-novo-token--hash)
(traz a receita pra bash *e* PowerShell puro, sem dependências externas).
