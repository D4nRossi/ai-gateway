# Deployment — sem Kubernetes

Topologia recomendada para Fase 1 (demo / piloto):

```
┌──────────┐    443/TLS    ┌──────────┐    8080/HTTP    ┌──────────┐
│ Internet │ ────────────> │  Caddy   │ ──────────────> │ gateway  │
│          │               │ (TLS PE) │                 │ (Go)     │
└──────────┘               └──────────┘                 └──────┬───┘
                                                              │
                                                       ┌──────▼──────┐
                                                       │  postgres   │
                                                       │ (Docker ou  │
                                                       │  managed)   │
                                                       └─────────────┘
```

Componentes:

- **Caddy** (ou Nginx) faz TLS termination com cert automático (Let's Encrypt)
  e proxy reverso para o serviço `gateway`. Single binary, zero-config.
- **gateway** roda como container Docker, sem porta exposta para internet —
  só Caddy enxerga.
- **postgres** em container Docker no mesmo host, ou managed (Azure Database
  / RDS / Aurora) para produção real.

## 1. Preparação do host

Requisitos:
- Linux x86_64
- Docker 24+ e `docker compose` v2
- Domínio apontado para o IP do host (necessário pro Let's Encrypt)
- Portas 80 e 443 abertas

## 2. Variáveis de ambiente (`.env`)

```bash
# Azure OpenAI
AZURE_OPENAI_ENDPOINT=https://seu-recurso.cognitiveservices.azure.com
AZURE_OPENAI_API_KEY=…

# Postgres — TROCAR EM PRODUÇÃO
POSTGRES_USER=gateway
POSTGRES_PASSWORD=$(openssl rand -base64 24)   # 32+ chars, fora do histórico
POSTGRES_DB=gateway

# AES-256-GCM para criptografar TargetAuth (ADR-0012)
# Esta chave é IRRECUPERÁVEL — perda significa re-cadastrar todos os targets
DB_ENCRYPTION_KEY=$(openssl rand -hex 32)

# Provider
PROVIDER=azure
```

> **Segredos:** prefira injetar via mecanismo do seu cloud provider
> (Azure Key Vault → App Configuration, AWS Secrets Manager, etc.) e
> renderizar `.env` no boot. Para piloto simples, o `.env` no host com
> permissão `chmod 600` é aceitável.

## 3. Endurecer o `docker-compose.yml` para produção

Crie `docker-compose.prod.yml` com overrides:

```yaml
services:
  postgres:
    # Remove a exposição pública da 5432 — gateway acessa via rede interna.
    ports: !reset []

  gateway:
    # Não expõe 8080 diretamente — Caddy é o único que enxerga.
    ports: !reset []
    expose:
      - "8080"
```

Subir:

```bash
docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d --build
```

## 4. Caddy como TLS terminator

`Caddyfile`:

```caddy
gateway.seudominio.com.br {
    encode gzip zstd

    # Repassa client-real-ip para o gateway (o LoginLimiter usa isso).
    reverse_proxy gateway:8080 {
        header_up X-Forwarded-Proto {scheme}
        header_up X-Forwarded-For {remote_host}
    }

    # Negar acesso direto às migrations / configs caso acidentalmente expostos.
    @sensitive {
        path /migrations/* /configs/*
    }
    handle @sensitive { respond 404 }
}
```

Adicione o serviço no `docker-compose.prod.yml`:

```yaml
services:
  caddy:
    image: caddy:2-alpine
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - ./Caddyfile:/etc/caddy/Caddyfile:ro
      - caddy-data:/data
      - caddy-config:/config
    depends_on:
      - gateway
    restart: unless-stopped

volumes:
  caddy-data:
  caddy-config:
```

> Caddy obtém certificado Let's Encrypt automaticamente no primeiro boot
> (precisa de DNS apontando e porta 80/443 reachable). Renova sozinho.

## 5. Bootstrap inicial — primeiro admin

```bash
# 1. Subir tudo (migrations rodam no boot do gateway):
docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d --build

# 2. Verificar logs até "migrations applied":
docker compose logs -f gateway

# 3. Criar o primeiro admin — DATABASE_URL precisa apontar para o postgres
#    do compose (use 127.0.0.1:5432 com a porta exposta temporariamente, ou
#    rode dentro da rede do compose via `docker compose run`):
set -a && source .env && set +a
DATABASE_URL="postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@localhost:5432/${POSTGRES_DB}?sslmode=disable" \
  go run ./cmd/admin-create -username daniel -role admin
```

Console: `https://gateway.seudominio.com.br/ui/login`.

## 6. Backup de Postgres

Mínimo aceitável: pg_dump diário via cron.

```bash
# /etc/cron.daily/backup-gateway
#!/bin/bash
set -euo pipefail
TS=$(date +%Y%m%d-%H%M)
docker compose exec -T postgres pg_dump -U gateway gateway | gzip > /var/backups/gateway-${TS}.sql.gz
find /var/backups -name 'gateway-*.sql.gz' -mtime +30 -delete
```

**Crucial:** a chave `DB_ENCRYPTION_KEY` precisa ser backupada **fora** do
mesmo lugar dos dumps. Sem ela, os targets criptografados são inúteis.

## 7. Monitoramento

Sinais a observar (todos em logs JSON estruturados):

| Evento | Onde | O que indica |
|---|---|---|
| `event_type=auth_failed` repetido para um `application_name` | logs do gateway | tentativa de uso de token expirado/revogado |
| `event_type=rate_limited` | logs | aplicação encostando no teto de RPM |
| `event_type=budget_exceeded` | logs | aplicação estourou orçamento do mês |
| `event_type=panic_recovered` | logs | bug — investigar imediatamente |
| HTTP 429 em `/admin/v1/auth/login` | logs do Caddy/gateway | possível brute-force |
| `/readyz` retornando 503 | qualquer probe externo | dependência (DB ou Azure) indisponível |

Para coleta:
- **Local / piloto:** `docker compose logs -f gateway | jq` direto
- **Produção:** Loki + Grafana, CloudWatch Logs, Azure Monitor — escolha um
  destino e configure o Docker logging driver:
  ```yaml
  services:
    gateway:
      logging:
        driver: json-file
        options:
          max-size: "100m"
          max-file: "10"
  ```

## 8. Escalabilidade — limites atuais

Para escalar horizontalmente (múltiplas réplicas), você vai precisar resolver
três limitações conhecidas (todas documentadas em ADR-0006 e ADR-0013):

1. **Rate limit é per-processo.** Réplica A não sabe que a réplica B já
   serviu requests da mesma aplicação. Solução: trocar `ratelimit.Manager`
   e `LoginLimiter` por implementações Redis-backed.

2. **Balancer `least_connections` é per-processo.** Cada réplica vê apenas
   seu próprio in-flight. Solução: usar `round_robin` ou
   `weighted_round_robin` (que são stateless) até migrar contadores para
   storage compartilhado.

3. **Session lookup vai ao DB a cada request.** Em volume alto, virá
   gargalo de leitura. Solução: cache TTL curto (~30s) em memória local
   ou em Redis.

Até resolver os três, **fique em single-instance** com vertical scaling
(mais CPU/RAM no host).

## 9. Atualizações sem downtime

Single-instance + restart graceful:

```bash
git pull
docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d --build gateway
# Caddy mantém conexões em-flight; o gateway tem graceful shutdown de 5s.
```

> O graceful shutdown está em `cmd/gateway/main.go` — 5s para drenar
> requests em vôo + cancelamento dos writers async.

## 10. Verificações pós-deploy

```bash
# Health checks
curl -sf https://gateway.seudominio.com.br/healthz
curl -sf https://gateway.seudominio.com.br/readyz

# UI acessível
curl -sI https://gateway.seudominio.com.br/ui/

# Login (esperado 401 com mensagem em pt-BR)
curl -s -X POST https://gateway.seudominio.com.br/admin/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"x","password":"y"}'

# Headers de segurança presentes
curl -sI https://gateway.seudominio.com.br/healthz | grep -E \
  "Strict-Transport-Security|X-Content-Type-Options|X-Frame-Options|Permissions-Policy"
```
