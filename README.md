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

| Ferramenta | Versão mínima |
|---|---|
| Go | 1.25+ |
| Docker | 24+ |
| Docker Compose | v2 |
| PostgreSQL | 17 (via Docker) |

## Início rápido (modo mock — sem Azure)

```bash
# 1. Subir o banco
docker compose up -d postgres

# 2. Copiar e configurar variáveis
cp .env.example .env
# edite .env se necessário (DATABASE_URL já preenchida para dev local)

# 3. Carregar variáveis no shell
set -a && source .env && set +a

# 4. Rodar com provider mock (sem precisar de credenciais Azure)
PROVIDER=mock go run ./cmd/gateway

# 5. Testar (em outro terminal)
curl -s http://localhost:8080/healthz

curl -s -X POST http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer gwk_basic_k9mxqr7tz2wn3vfp" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-4.1-nano","messages":[{"role":"user","content":"Olá!"}]}'
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
| [Roadmap](docs/roadmap.md) | O que está feito, o que vem na Phase 2 |
| [ADRs](docs/adrs/) | Decisões arquiteturais registradas (ADR-0001 a ADR-0008) |

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

```bash
# Infra
docker compose up -d postgres       # sobe só o banco
docker compose up                   # sobe banco + gateway em container

# Desenvolvimento
PROVIDER=mock go run ./cmd/gateway  # rodar sem Azure
go test ./...                       # rodar toda a suite de testes
go test -race ./...                 # rodar com detector de race conditions
go test -bench=. -benchmem ./...    # rodar benchmarks com memória

# Build
CGO_ENABLED=0 GOOS=linux go build -o bin/ai-gateway ./cmd/gateway
docker build -t ai-gateway:dev .

# Gerar token + hash para nova aplicação
TOKEN="gwk_novaapp_$(openssl rand -hex 24)"
echo "Token (distribuir): $TOKEN"
echo "Hash (gateway.yaml): $(echo -n "$TOKEN" | sha256sum | cut -d' ' -f1)"

# Migration manual
migrate -database "$DATABASE_URL" -path migrations up
migrate -database "$DATABASE_URL" -path migrations down 1
```
