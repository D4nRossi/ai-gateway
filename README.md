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
# 1. Clonar e entrar no diretório
cd ai-gateway

# 2. Subir o banco
docker compose up -d postgres

# 3. Gerar um hash para o token de teste
echo -n "gwk_leve_meutokendeteste123" | sha256sum | cut -d' ' -f1
# → cole o resultado em configs/gateway.yaml → applications[0].key_hash

# 4. Exportar variável mínima
export DATABASE_URL="postgres://gateway:gateway@localhost:5432/gateway?sslmode=disable"

# 5. Rodar com provider mock (sem Azure)
PROVIDER=mock go run ./cmd/gateway

# 6. Testar
curl -s http://localhost:8080/healthz
curl -s -X POST http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer gwk_leve_meutokendeteste123" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-4.1-nano","messages":[{"role":"user","content":"Olá!"}]}'
```

## Documentação completa

| Documento | Conteúdo |
|---|---|
| [Como funciona](docs/how-it-works.md) | Arquitetura, fluxo de request, mapa de pacotes |
| [Desenvolvimento local](docs/local-development.md) | Setup detalhado, geração de tokens, testes manuais |
| [Deploy em produção](docs/production-deploy.md) | Docker, variáveis de ambiente, segurança, Azure setup |
| [Manutenção](docs/maintenance.md) | Adicionar apps, rotacionar chaves, migrations, monitoramento |
| [ADRs](docs/adrs/) | Decisões arquiteturais registradas (ADR-0001 a ADR-0008) |

## Endpoints

```
GET  /healthz                  → 200 sempre (liveness)
GET  /readyz                   → 200 se DB ok, 503 caso contrário (readiness)
GET  /v1/models                → lista de modelos da aplicação autenticada
POST /v1/chat/completions      → chat (stream e non-stream, compatível OpenAI)
```

## Variáveis de ambiente

| Variável | Obrigatória | Descrição |
|---|---|---|
| `DATABASE_URL` | Sim | URL PostgreSQL |
| `AZURE_OPENAI_ENDPOINT` | Sim* | Endpoint Azure OpenAI |
| `AZURE_OPENAI_API_KEY` | Sim* | Chave Azure OpenAI |
| `AZURE_CS_ENDPOINT` | Não | Endpoint Content Safety (Tier 3) |
| `AZURE_CS_API_KEY` | Não | Chave Content Safety (Tier 3) |
| `PROVIDER` | Não | `azure` (padrão) ou `mock` |
| `CONFIG_PATH` | Não | Caminho do YAML (padrão: `configs/gateway.yaml`) |

*Não necessárias com `PROVIDER=mock`.

## Comandos úteis

```bash
# Subir infra
docker compose up -d postgres

# Rodar localmente
go run ./cmd/gateway

# Build binário estático
CGO_ENABLED=0 GOOS=linux go build -o bin/ai-gateway ./cmd/gateway

# Build imagem Docker
docker build -t ai-gateway:dev .

# Subir tudo (inclui gateway em container)
docker compose up

# Migration manual (rollback 1 passo)
migrate -database "$DATABASE_URL" -path migrations down 1

# Gerar hash de um bearer token
echo -n "gwk_appname_segredoaqui" | sha256sum | cut -d' ' -f1
```
