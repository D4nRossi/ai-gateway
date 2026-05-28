# Deploy Linux — nginx + Docker / systemd

> Manual operacional pra subir o AI Gateway em servidor Linux corporativo
> (air-gap parcial: Azure liberado, GitHub bloqueado). Cobre dois caminhos
> alternativos:
>
> - **Path A — Docker Compose** (recomendado): nginx + gateway containerizados,
>   um único `docker compose up -d` derruba e levanta tudo.
> - **Path B — systemd + nginx no host**: gateway como binário nativo
>   gerenciado por systemd, nginx instalado direto no host. Útil em
>   ambientes que proíbem Docker.
>
> Os dois paths usam o mesmo binário e a mesma config — você escolhe o
> envelope.

---

## 0. Pré-requisitos do servidor

| Item | Versão / requisito | Como verificar |
|---|---|---|
| OS | Ubuntu Server 22.04+ / RHEL 8+ / Debian 12+ | `cat /etc/os-release` |
| CPU | 2+ vCPU (recomendado 4) | `nproc` |
| RAM | 2 GiB mínimo, 4 GiB confortável | `free -h` |
| Disco | 10 GiB livres (binário + logs + nginx cache) | `df -h /` |
| Docker (Path A) | 24+ | `docker --version` |
| docker compose plugin (Path A) | v2 | `docker compose version` |
| nginx (Path B) | 1.22+ | `nginx -v` |
| systemd (Path B) | 245+ | `systemctl --version` |
| `migrate` CLI (admin runs migrations) | v4.18+ | `migrate -version` |
| `sqlcmd` ou cliente equivalente | qualquer | — |
| Cliente SSH | qualquer | — |

A **máquina de build (workstation)** precisa:

| Item | Versão |
|---|---|
| Go | 1.25+ |
| Node | 20+ |
| Docker (se for produzir imagem) | 24+ |
| Acesso à internet (sim, na workstation; **não no servidor**) | — |

---

## 1. Outbound permitido no firewall corporativo

Liberar **apenas** os endpoints abaixo no servidor de produção. Sem GitHub.
Sem `proxy.golang.org`. Sem npm registry.

### Obrigatório (gateway não sobe sem)

| Host | Porta | Motivo |
|---|---|---|
| `*.cognitiveservices.azure.com` | 443 | Azure OpenAI (chat + language) — proxy upstream |
| `*.vault.azure.net` | 443 | Key Vault — credenciais cifradas |
| `login.microsoftonline.com` | 443 | DefaultAzureCredential (auth pro KV) |
| `<sql-server-corp>:1433` | 1433 | SQL Server interno (DB de operação) |
| `<ntp-corp>` | 123 (UDP) | Sincronização de clock (audit / token expiry) |

### Opcional (depende do uso)

| Host | Porta | Motivo |
|---|---|---|
| Outros upstreams configurados em `proxy_targets.url` | 443 | Quando outros providers (Anthropic, OpenAI direto, custom APIs) forem cadastrados |

---

## 2. Build do artefato — feito na workstation, NÃO no servidor

A workstation tem internet; o servidor não. Produzimos artefatos auto-contidos
e copiamos pra produção.

### 2.1 Buildar o binário (sem Docker)

```bash
# Na workstation
cd ~/projects/ai-gateway

# Frontend embed (o Go vai consumir via go:embed)
cd web && npm ci && npm run build && cd ..

# Binário Linux estático, sem CGO. Funciona em qualquer distro x86_64 moderna.
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w -X main.Version=$(git rev-parse --short HEAD)" \
    -o dist/gateway \
    ./cmd/gateway

# Idem pra o CLI de migração de credenciais (opcional, Onda 4.5)
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -o dist/migrate-targets-to-kv \
    ./cmd/migrate-targets-to-kv

# Hash pra auditoria
sha256sum dist/gateway dist/migrate-targets-to-kv > dist/SHA256SUMS
```

Saída: `dist/gateway` (~30 MB estático), `dist/migrate-targets-to-kv`, `dist/SHA256SUMS`.

### 2.2 Buildar imagem Docker (Path A)

```bash
# Na workstation — produz imagem auto-contida
docker build -t ai-gateway:$(git rev-parse --short HEAD) .

# Exportar pra arquivo .tar (pra transportar pro servidor sem registry)
docker save ai-gateway:$(git rev-parse --short HEAD) -o dist/ai-gateway-image.tar
sha256sum dist/ai-gateway-image.tar >> dist/SHA256SUMS
```

> O `Dockerfile` na raiz é multi-stage (build em `golang:1.25-alpine`, runtime
> em `alpine:3.21` minimalista) — versões pinadas conforme CLAUDE.md §4.4.

### 2.3 Empacotar configs

```bash
# Tudo que o servidor precisa, agrupado
mkdir -p dist/configs dist/migrations
cp configs/gateway.yaml dist/configs/
cp -r migrations/*.sql dist/migrations/
cp .env.example dist/.env.template

# tar final
cd dist && tar -czf ../ai-gateway-deploy.tar.gz . && cd ..
sha256sum ai-gateway-deploy.tar.gz
```

---

## 3. Transporte pro servidor

```bash
# Da workstation pro servidor
scp ai-gateway-deploy.tar.gz operator@gateway-prod:/tmp/
# OU via storage corporativo (S3 interno, file share, etc)
```

No servidor, **valide o hash** antes de extrair:

```bash
cd /tmp
sha256sum -c <<< "$(cat ai-gateway-deploy.tar.gz.sha256)  ai-gateway-deploy.tar.gz"
# OK: "ai-gateway-deploy.tar.gz: OK"
```

---

## 4. Setup do host

### 4.1 Usuário e diretórios

```bash
sudo useradd --system --shell /usr/sbin/nologin --home-dir /opt/ai-gateway --create-home gateway

sudo mkdir -p /opt/ai-gateway/{bin,configs,migrations,logs}
sudo mkdir -p /etc/ai-gateway
sudo chown -R gateway:gateway /opt/ai-gateway
sudo chmod 750 /opt/ai-gateway
```

### 4.2 Extrair artefatos

```bash
sudo tar -xzf /tmp/ai-gateway-deploy.tar.gz -C /opt/ai-gateway/
sudo mv /opt/ai-gateway/gateway /opt/ai-gateway/bin/
sudo mv /opt/ai-gateway/migrate-targets-to-kv /opt/ai-gateway/bin/
sudo chmod +x /opt/ai-gateway/bin/gateway /opt/ai-gateway/bin/migrate-targets-to-kv
sudo chown -R gateway:gateway /opt/ai-gateway
```

### 4.3 TLS cert (CA corporativa)

Copiar o cert + chave fornecidos pelo time de PKI corporativo:

```bash
sudo mkdir -p /etc/ai-gateway/tls
sudo cp gateway.crt gateway.key /etc/ai-gateway/tls/
sudo chmod 600 /etc/ai-gateway/tls/gateway.key
sudo chmod 644 /etc/ai-gateway/tls/gateway.crt
sudo chown root:gateway /etc/ai-gateway/tls/*
```

A CA root corporativa precisa estar no trust store do servidor — geralmente
já está em ambiente corp; valide com:

```bash
update-ca-certificates -v   # Debian/Ubuntu
# ou
update-ca-trust extract     # RHEL/CentOS
```

---

## 5. Configuração — `gateway.yaml` + `.env`

### 5.1 `gateway.yaml`

```bash
sudo cp /opt/ai-gateway/configs/gateway.yaml /etc/ai-gateway/gateway.yaml
sudo nano /etc/ai-gateway/gateway.yaml
```

Pontos críticos pra revisar:

- `server.port` — manter `8080` (nginx faz proxy reverso na 443)
- `database.host`, `database.database`, `database.user`, `database.schema` — apontar pro SQL Server corp
- `database.encrypt: true` + `database.trust_server_certificate: false` em prod (true só pra dev com cert self-signed)
- `database.max_conns`, `database.min_conns` — sizing conforme carga esperada
- `azure_openai.endpoint` — endpoint do Azure OpenAI corp
- Segredos via `${kv:NOME}` apontando pro KV corporativo
- `logging.format: json` em prod
- `logging.raw_prompt_logging: false` em qualquer ambiente não-dev

### 5.2 `.env` (variáveis de ambiente)

```bash
sudo nano /etc/ai-gateway/gateway.env
```

```env
# Endpoints — substitua pelos do seu ambiente
KEYVAULT_URI=https://prod-vault.vault.azure.net/
AZURE_OPENAI_ENDPOINT=https://corp-openai.cognitiveservices.azure.com
AZURE_LANGUAGE_ENDPOINT=https://corp-language.cognitiveservices.azure.com

# Logging
LOG_LEVEL=info

# ADR-0025: prod usa MANUAL migration mode.
# Gateway só boota se schema_migrations.version == max(migrations/*.up.sql).
# Operator (DBA) aplica `migrate up` ANTES de subir a versão nova do binário.
MIGRATIONS_AUTO_APPLY=false

# Provider — manter "azure" em prod. "mock" só pra debug local.
PROVIDER=azure

# Path da config — não altere sem motivo
CONFIG_PATH=/etc/ai-gateway/gateway.yaml
```

```bash
sudo chown root:gateway /etc/ai-gateway/gateway.yaml /etc/ai-gateway/gateway.env
sudo chmod 640 /etc/ai-gateway/gateway.yaml /etc/ai-gateway/gateway.env
```

### 5.3 Auth pro Azure (Managed Identity em prod)

Em prod corporativo a recomendação é **Managed Identity** atribuída à VM,
não `az login`. O `DefaultAzureCredential` no gateway detecta MI
automaticamente — **nenhuma config no .env** é necessária.

Pré-requisito: o time de Azure precisa atribuir Managed Identity à VM e
conceder ao principal: `Key Vault Secrets User` no KV de produção.

Pra ambientes ainda sem MI configurada, ou em testes offline, valem
service principals via env vars (`AZURE_TENANT_ID`, `AZURE_CLIENT_ID`,
`AZURE_CLIENT_SECRET`).

---

## 6. Database — aplicação manual de migrations (ADR-0025)

**Não confunda**: o `MIGRATIONS_AUTO_APPLY=false` impede o gateway de
mexer no schema. O **operator** (você, ou o DBA) aplica manualmente
**antes** de subir o binário novo.

### 6.1 Fluxo padrão

```bash
# Na workstation (ou bastion com acesso ao SQL Server), NÃO no servidor de app
DATABASE_URL='sqlserver://usr_app:<SENHA>@sql-corp:1433?database=AIGateway_prod&encrypt=true&trustServerCertificate=false'

# Aplicar tudo até a última versão presente em migrations/
migrate -database "$DATABASE_URL" -path migrations up

# Verificar versão final
migrate -database "$DATABASE_URL" -path migrations version
# saída: 11 (exemplo)
```

### 6.2 Procedimento de rollback (raro)

```bash
# Voltar 1 versão
migrate -database "$DATABASE_URL" -path migrations down 1

# Voltar pra versão específica
migrate -database "$DATABASE_URL" -path migrations goto 10
```

### 6.3 Resolver `dirty=1` (migration interrompida)

```bash
# Inspecionar
sqlcmd -S sql-corp -d AIGateway_prod -U usr_app -Q "SELECT * FROM dbo.schema_migrations"

# Se ALTER rodou mas índice falhou: re-aplica manualmente o que faltou,
# depois marca como aplicado e limpa dirty
sqlcmd -S sql-corp -d AIGateway_prod -U usr_app -Q "UPDATE dbo.schema_migrations SET dirty = 0 WHERE version = N"

# Se ALTER nem rodou (parse error): regredir versão E limpar dirty
sqlcmd -S sql-corp -d AIGateway_prod -U usr_app -Q "UPDATE dbo.schema_migrations SET version = N-1, dirty = 0"
# depois retry: migrate up
```

> **Importante:** quando apenas `dirty = 0` é setado sem regredir a versão,
> o gateway acredita que a migration está aplicada — mas as colunas talvez
> NÃO estejam criadas (parse error aborta o batch). Veja a sessão de
> 2026-05-28 pra contexto.

---

## Path A — Docker Compose

### A1. Importar a imagem

```bash
sudo docker load -i /tmp/ai-gateway-image.tar
sudo docker images | grep ai-gateway
```

### A2. `docker-compose.prod.yaml`

`/opt/ai-gateway/docker-compose.prod.yaml`:

```yaml
services:
  gateway:
    image: ai-gateway:abc1234        # tag conforme `docker images`
    restart: unless-stopped
    user: "1001:1001"                # uid:gid do user "gateway"
    env_file:
      - /etc/ai-gateway/gateway.env
    volumes:
      - /etc/ai-gateway/gateway.yaml:/app/configs/gateway.yaml:ro
      - /opt/ai-gateway/logs:/app/logs
    expose:
      - "8080"                       # interno apenas — nginx termina TLS
    healthcheck:
      test: ["CMD", "wget", "-q", "--spider", "http://localhost:8080/healthz"]
      interval: 30s
      timeout: 5s
      retries: 3
      start_period: 20s
    logging:
      driver: json-file
      options:
        max-size: "50m"
        max-file: "5"

  nginx:
    image: nginx:1.27-alpine
    restart: unless-stopped
    depends_on:
      gateway:
        condition: service_healthy
    ports:
      - "443:443"
      - "80:80"
    volumes:
      - /etc/ai-gateway/nginx.conf:/etc/nginx/nginx.conf:ro
      - /etc/ai-gateway/tls:/etc/nginx/tls:ro
      - /opt/ai-gateway/logs/nginx:/var/log/nginx
    logging:
      driver: json-file
      options:
        max-size: "50m"
        max-file: "5"
```

### A3. `nginx.conf` (proxy + TLS + SSE-friendly)

`/etc/ai-gateway/nginx.conf`:

```nginx
worker_processes auto;
events { worker_connections 4096; }

http {
    sendfile on;
    keepalive_timeout 65;
    server_tokens off;

    # Log format pra auditoria
    log_format main '$remote_addr - $remote_user [$time_local] "$request" '
                    '$status $body_bytes_sent "$http_referer" '
                    '"$http_user_agent" rt=$request_time uct=$upstream_connect_time '
                    'urt=$upstream_response_time';

    access_log /var/log/nginx/access.log main;
    error_log  /var/log/nginx/error.log warn;

    # Redirect HTTP → HTTPS
    server {
        listen 80;
        server_name _;
        return 301 https://$host$request_uri;
    }

    upstream gateway_upstream {
        server gateway:8080;
        keepalive 64;
    }

    server {
        listen 443 ssl http2;
        server_name _;

        ssl_certificate     /etc/nginx/tls/gateway.crt;
        ssl_certificate_key /etc/nginx/tls/gateway.key;
        ssl_protocols       TLSv1.2 TLSv1.3;
        ssl_ciphers         HIGH:!aNULL:!MD5;
        ssl_prefer_server_ciphers on;
        ssl_session_cache   shared:SSL:10m;

        # Tamanho de body — chat completions pode ter prompt grande
        client_max_body_size 5m;

        location / {
            proxy_pass http://gateway_upstream;
            proxy_http_version 1.1;
            proxy_set_header Host $host;
            proxy_set_header X-Real-IP $remote_addr;
            proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
            proxy_set_header X-Forwarded-Proto https;
            proxy_set_header Connection "";

            # SSE-friendly: NÃO buffer + read timeout longo
            proxy_buffering off;
            proxy_cache off;
            proxy_read_timeout 600s;
            proxy_send_timeout 600s;
        }

        # /healthz e /readyz expostos sem auth — gateway já decide o que mostrar
        # mas não logamos pra reduzir ruído
        location ~ ^/(healthz|readyz)$ {
            proxy_pass http://gateway_upstream;
            access_log off;
        }
    }
}
```

### A4. Subir o compose

```bash
cd /opt/ai-gateway
sudo docker compose -f docker-compose.prod.yaml up -d

# Verificar
sudo docker compose -f docker-compose.prod.yaml ps
sudo docker compose -f docker-compose.prod.yaml logs gateway --tail 50
```

### A5. Rolling restart (deploy de nova versão)

```bash
# 1. DBA aplica migrations pendentes (ver §6)
# 2. Importar imagem nova
sudo docker load -i /tmp/ai-gateway-image-NOVAVERSAO.tar
# 3. Editar tag no docker-compose.prod.yaml
sudo sed -i 's|ai-gateway:abc1234|ai-gateway:def5678|' /opt/ai-gateway/docker-compose.prod.yaml
# 4. Recriar só o gateway (nginx fica)
sudo docker compose -f /opt/ai-gateway/docker-compose.prod.yaml up -d gateway
# 5. Conferir health
sleep 10 && curl -sk https://localhost/healthz && echo
```

---

## Path B — systemd + nginx no host

### B1. Instalar nginx

```bash
# Ubuntu/Debian
sudo apt update && sudo apt install -y nginx

# RHEL/CentOS
sudo dnf install -y nginx
```

### B2. `nginx.conf` — copiar do Path A

Mesmo conteúdo da seção A3, ajustando o `upstream` pra apontar em
`127.0.0.1:8080`:

```nginx
upstream gateway_upstream {
    server 127.0.0.1:8080;
    keepalive 64;
}
```

Salvar em `/etc/nginx/conf.d/ai-gateway.conf` (ou substituir o default).

```bash
sudo nginx -t                    # valida config
sudo systemctl reload nginx
```

### B3. systemd unit

`/etc/systemd/system/ai-gateway.service`:

```ini
[Unit]
Description=AI Gateway
Documentation=https://internal-docs/ai-gateway
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=gateway
Group=gateway
WorkingDirectory=/opt/ai-gateway

# Carregar env vars do arquivo (CONFIG_PATH, KEYVAULT_URI, MIGRATIONS_AUTO_APPLY, etc)
EnvironmentFile=/etc/ai-gateway/gateway.env

# Binário + config
ExecStart=/opt/ai-gateway/bin/gateway

# Logs vão pro journald (jq via journalctl) E pra arquivo (StandardOutput=append)
StandardOutput=append:/opt/ai-gateway/logs/gateway.log
StandardError=append:/opt/ai-gateway/logs/gateway-error.log

# Restart automático em falha (max 5 tentativas em 60s)
Restart=on-failure
RestartSec=5s
StartLimitBurst=5
StartLimitIntervalSec=60s

# Hardening
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/opt/ai-gateway/logs
CapabilityBoundingSet=
AmbientCapabilities=

# Limits razoáveis
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now ai-gateway

# Status + logs ao vivo
sudo systemctl status ai-gateway
sudo journalctl -u ai-gateway -f
```

### B4. Rotação de logs no host

Como `StandardOutput=append`, os arquivos crescem indefinidamente. Adicionar
logrotate:

`/etc/logrotate.d/ai-gateway`:

```
/opt/ai-gateway/logs/*.log {
    daily
    rotate 14
    compress
    delaycompress
    missingok
    notifempty
    create 0640 gateway gateway
    sharedscripts
    postrotate
        systemctl reload ai-gateway 2>/dev/null || true
    endscript
}
```

---

## 7. Smoke test pós-deploy

Roteiro mínimo. Falhou em qualquer ponto, abortar e analisar logs.

```bash
# 1. Liveness
curl -sk https://gateway-prod/healthz
# esperado: 200 OK com {"status":"ok"} ou similar

# 2. Readiness (DB + KV)
curl -sk https://gateway-prod/readyz
# esperado: 200 OK; se DB ou KV down: 503

# 3. Login admin
curl -sk -X POST https://gateway-prod/admin/v1/auth/login \
    -H 'Content-Type: application/json' \
    -d '{"username":"<admin_user>","password":"<senha>"}'
# esperado: {"token":"...","role":"admin","expires_at":"..."}

# 4. Chat completion com app token válido
curl -sk -X POST https://gateway-prod/v1/chat/completions \
    -H "Authorization: Bearer <APP_TOKEN>" \
    -H 'Content-Type: application/json' \
    -d '{"model":"gpt-4.1-nano","messages":[{"role":"user","content":"diga oi"}],"max_tokens":20}'
# esperado: 200 + JSON com choices[0].message.content

# 5. Conferir usage_events foi gravado
sqlcmd -S sql-corp -d AIGateway_prod -U usr_app -Q "SELECT TOP 5 created_at, application_name, model, total_tokens, status_code FROM gogateway.usage_events ORDER BY created_at DESC"
# esperado: linha nova com a chamada acima

# 6. Conferir audit_events (request_started + request_completed)
sqlcmd -S sql-corp -d AIGateway_prod -U usr_app -Q "SELECT TOP 5 created_at, event_type, severity FROM gogateway.audit_events ORDER BY created_at DESC"
```

---

## 8. Logs e troubleshooting

### Onde ficam

| Componente | Path Path A (Docker) | Path Path B (systemd) |
|---|---|---|
| Gateway stdout | `docker compose logs gateway` ou `/opt/ai-gateway/logs/` (volume) | `/opt/ai-gateway/logs/gateway.log` + journald |
| Gateway erros | idem | `/opt/ai-gateway/logs/gateway-error.log` + journald |
| nginx access | `/opt/ai-gateway/logs/nginx/access.log` | `/var/log/nginx/access.log` |
| nginx error | `/opt/ai-gateway/logs/nginx/error.log` | `/var/log/nginx/error.log` |
| SQL Server logs | banco corp (DBA) | banco corp (DBA) |

### Cenários comuns

**Gateway não sobe — `KEYVAULT_URI is empty`:**
- `.env` não foi carregado. Path A: revisar `env_file`. Path B: verificar `EnvironmentFile` no unit + `sudo systemctl daemon-reload`.

**Boot falha com `ErrSchemaOutOfDate`:**
- DBA não aplicou migration pendente. Rode `migrate up` (§6) e reinicie o gateway.

**Boot falha com `ErrSchemaDirty`:**
- Migration anterior travou. Procedimento em §6.3.

**`DefaultAzureCredential: failed to acquire a token`:**
- Managed Identity não atribuída à VM, ou principal não tem permissão no KV. Verifique com o time de Azure.

**`net/http: TLS handshake timeout` pro vault:**
- Firewall corporativo bloqueando `*.vault.azure.net`. Veja §1.

**`mssql: Login failed for user`:**
- Senha do KV expirou ou conta foi revogada. Rotacionar `AzureAIGateway-DB-Password-hom` no KV e reiniciar gateway.

**Streaming SSE quebra no nginx:**
- Confirmar `proxy_buffering off` no location relevante (§A3 / §B2).

---

## 9. Manutenção operacional

### 9.1 Rotação de credencial Azure OpenAI

```bash
# Time Azure rotaciona key no portal → nova versão do secret no KV
# Gateway pega automaticamente após TTL de 5min do cache (ADR-0018)
# Pra forçar refresh imediato: reinicia
sudo systemctl restart ai-gateway     # Path B
# OU
sudo docker compose -f /opt/ai-gateway/docker-compose.prod.yaml restart gateway   # Path A
```

### 9.2 Rotação de DB_ENCRYPTION_KEY

Quebra targets em mode=aes. Solução: migrar todos pra mode=both ou mode=kv
ANTES de rotacionar a master key (Onda 4.5 / ADR-0020).

```bash
# Pra cada target ainda em mode=aes:
/opt/ai-gateway/bin/migrate-targets-to-kv -target-id N -mode both
```

### 9.3 Backup / DR

- DB: cobertos pela política do SQL Server corporativo (Always On AG + backup nightly)
- Logs: arquivar `*.log.gz` rotacionados em storage durável (rsync pra share interno, etc)
- KV: snapshot manual periódico do `az keyvault secret list` é uma camada extra

### 9.4 Rolling upgrade

1. **DBA** aplica migrations pendentes (`migrate up`)
2. **Operator** copia binário/imagem nova pro `/tmp` do servidor (validar hash)
3. **Path A**: `docker compose up -d gateway` recria container; nginx mantém
4. **Path B**: substituir `/opt/ai-gateway/bin/gateway` + `sudo systemctl restart ai-gateway`
5. **Smoke test** (§7) — se falhar, rollback (§10)

---

## 10. Rollback procedure

**Manter sempre N-1 disponível no servidor**: nas duas paths, copie a versão
anterior pra `/opt/ai-gateway/bin/gateway.previous` (Path B) ou guarde a
imagem antiga (Path A).

```bash
# Path B
sudo cp /opt/ai-gateway/bin/gateway /opt/ai-gateway/bin/gateway.failed
sudo cp /opt/ai-gateway/bin/gateway.previous /opt/ai-gateway/bin/gateway
sudo systemctl restart ai-gateway

# Path A
sudo sed -i 's|ai-gateway:def5678|ai-gateway:abc1234|' /opt/ai-gateway/docker-compose.prod.yaml
sudo docker compose -f /opt/ai-gateway/docker-compose.prod.yaml up -d gateway
```

**Se a migration nova quebrou compat com binário antigo:** rollback do schema:

```bash
# DBA executa
migrate -database "$DATABASE_URL" -path migrations down 1
```

> Risco: migrations down podem perder dados (drop column). Sempre revisar o
> `.down.sql` da migration mais recente antes de regredir.

---

## 11. Apêndices

### 11.1 Catálogo de env vars

| Var | Obrigatória | Default | Descrição |
|---|---|---|---|
| `KEYVAULT_URI` | sim (se config usa `${kv:...}`) | — | URL do KV corporativo |
| `AZURE_OPENAI_ENDPOINT` | sim | — | Endpoint do Azure OpenAI |
| `AZURE_LANGUAGE_ENDPOINT` | sim (se Tier 2/3 PII via Azure) | — | Endpoint Azure Language |
| `MIGRATIONS_AUTO_APPLY` | **recomendado `false` em prod** | `true` | ADR-0025 |
| `LOG_LEVEL` | não | `info` | `debug` / `info` / `warn` / `error` |
| `PROVIDER` | não | `azure` | `azure` / `mock` |
| `CONFIG_PATH` | não | `configs/gateway.yaml` | Caminho do yaml |

### 11.2 Portas e endpoints expostos

| Porta | Externamente acessível | Quem responde | TLS |
|---|---|---|---|
| 443 | Sim | nginx → gateway | Sim |
| 80 | Sim | nginx (redirect 301 → 443) | — |
| 8080 | Não (loopback ou rede docker) | gateway direto | Não |

### 11.3 Paths importantes

| Path | Conteúdo | Permissão |
|---|---|---|
| `/opt/ai-gateway/bin/` | Binários | `gateway:gateway 0755` |
| `/opt/ai-gateway/logs/` | Logs do app + nginx | `gateway:gateway 0750` |
| `/etc/ai-gateway/gateway.yaml` | Config | `root:gateway 0640` |
| `/etc/ai-gateway/gateway.env` | Env vars | `root:gateway 0640` |
| `/etc/ai-gateway/tls/gateway.crt` | Cert público | `root:gateway 0644` |
| `/etc/ai-gateway/tls/gateway.key` | Chave privada | `root:gateway 0600` |
| `/etc/ai-gateway/nginx.conf` | Nginx config | `root:root 0644` |

### 11.4 Comandos úteis de runtime

```bash
# Tail dos logs em produção
sudo journalctl -u ai-gateway -f                            # Path B
sudo docker compose -f /opt/ai-gateway/docker-compose.prod.yaml logs -f gateway   # Path A

# Health rápido
curl -sk https://localhost/healthz https://localhost/readyz

# Conferir versão deployada
sudo /opt/ai-gateway/bin/gateway -version 2>&1 || true     # se exposto via flag
# OU via header (futuro): curl -sIk https://localhost/healthz | grep -i version

# Conferir schema_migrations
sqlcmd -S sql-corp -d AIGateway_prod -U usr_app -Q "SELECT version, dirty FROM dbo.schema_migrations"

# Ler 10 últimas chamadas
sqlcmd -S sql-corp -d AIGateway_prod -U usr_app -Q "SELECT TOP 10 created_at, application_name, model, latency_ms, status_code, total_tokens FROM gogateway.usage_events ORDER BY created_at DESC"
```

---

## 12. Referências

- ADR-0010 — generic HTTP proxy engine
- ADR-0018 — Key Vault provider
- ADR-0020 — credential storage mode per target
- ADR-0022 — troca PG → SQL Server
- ADR-0024 — usage tracking no proxy plane
- ADR-0025 — MIGRATIONS_AUTO_APPLY toggle
- `docs/deploy/windows.md` — manual equivalente Windows + IIS + WinSW
- CLAUDE.md §4.4 — versões pinadas (golang:1.25-alpine, alpine:3.21)
- `docker-compose.yml` (raiz) — referência pra dev local
