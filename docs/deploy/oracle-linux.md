# Deploy — Oracle Linux 9 + Docker Compose + nginx

> Manual operacional **super detalhado** pra publicar o API Gateway em
> servidor Oracle Linux 9 (RHEL 9 compatible). Cobre desde a configuracao
> do host ate smoke test, troubleshooting e atualizacoes.
>
> Status: **escrito em 2026-06-02 com layout monorepo apps/ (ADR-0030)**.
> Substitui o `linux.md` antigo (pre-reorg). Para Windows, ver
> `windows.md` — mas **deploy Windows esta suspenso** desde 2026-06-02
> (memoria `deploy_windows_suspended.md`).

## Sumario

- [0. Versao Linux + visao geral](#0-versao-linux--visao-geral)
- [1. Pre-requisitos do servidor](#1-pre-requisitos-do-servidor)
- [2. Hardening inicial do host](#2-hardening-inicial-do-host)
- [3. Instalar Docker CE em Oracle Linux 9](#3-instalar-docker-ce-em-oracle-linux-9)
- [4. Alternativa: Podman + podman-compose](#4-alternativa-podman--podman-compose)
- [5. Criar usuario operacional](#5-criar-usuario-operacional)
- [6. Provisionar diretorios + certs TLS](#6-provisionar-diretorios--certs-tls)
- [7. Clonar repo e checkout](#7-clonar-repo-e-checkout)
- [8. Configurar Azure auth](#8-configurar-azure-auth)
- [9. Validar conectividade SQL Server](#9-validar-conectividade-sql-server)
- [10. .env + secrets do compose](#10-env--secrets-do-compose)
- [11. Primeiro boot](#11-primeiro-boot)
- [12. Smoke test end-to-end](#12-smoke-test-end-to-end)
- [13. Acesso ao admin console](#13-acesso-ao-admin-console)
- [14. Logs + monitoring](#14-logs--monitoring)
- [15. Atualizar versao](#15-atualizar-versao)
- [16. Rollback](#16-rollback)
- [17. Troubleshooting](#17-troubleshooting)
- [18. Manutencao recorrente](#18-manutencao-recorrente)
- [19. Apendice — checklist de aprovacao corp](#19-apendice--checklist-de-aprovacao-corp)

---

## 0. Versao Linux + visao geral

| Item | Valor |
|---|---|
| Distro alvo | **Oracle Linux 9.x** (RHEL 9 compatible) |
| Init system | systemd |
| Firewall | firewalld |
| Mandatory access control | SELinux **enforcing** |
| Container runtime | Docker CE 27+ (preferido) ou Podman 4+ |
| Reverse proxy / TLS | nginx (via container, infra/nginx/) |
| Banco de dados | SQL Server corp (BRSPVPDEV003.tpb.corp:1433) — externo |
| Secrets backend | Azure Key Vault (`${kv:...}` no `gateway.yaml`) |
| Idioma do gateway | Go 1.25 (data plane), .NET 10 (admin plane futuro — ADR-0027) |

### Topologia alvo

```
        ┌─────────────────────────────────────┐
        │  Servidor Oracle Linux 9            │
        │  ┌──────────┐  ┌──────────┐  ┌────┐│
        │  │ container│  │ container│  │ ...││
        │  │  nginx   │──│  gateway │  │    ││
        │  │  443/80  │  │  :8080   │  │    ││
        │  └──────────┘  └──────────┘  └────┘│
        │       │              │              │
        │       │ proxy /      │ KV + SQL     │
        │       ▼              ▼              │
        │  ┌──────────┐                       │
        │  │ container│                       │
        │  │  console │                       │
        │  │   :80    │                       │
        │  └──────────┘                       │
        └────────────────┼────────────────────┘
                         │
        VPN corp ───── BRSPVPDEV003 (SQL)
                  └── *.cognitiveservices.azure.com
                  └── *.vault.azure.net
```

---

## 1. Pre-requisitos do servidor

### 1.1 Hardware minimo

| Recurso | Minimo dev/hom | Recomendado prod |
|---|---|---|
| vCPU | 2 | 4 |
| RAM | 4 GB | 8 GB |
| Disk | 20 GB | 50 GB SSD (logs crescem) |

### 1.2 Acesso e permissoes corp

Antes de comecar, valide com a area de Infra corp:

- [ ] Servidor provisionado com OL 9.x atualizado (`sudo dnf update -y`)
- [ ] Acesso SSH com chave (nao senha)
- [ ] Usuario com `sudo` (membro do grupo `wheel`)
- [ ] **Firewall corp libera outbound TCP**:
  - 1433 → `BRSPVPDEV003.tpb.corp` (SQL Server)
  - 443 → `*.vault.azure.net`, `login.microsoftonline.com`, `*.cognitiveservices.azure.com`
  - 443 → registry de container (docker.io ou registry corp interno)
  - 22 → GitHub (so se for buildar local; pode ser pull de tarball)
- [ ] **Firewall corp libera inbound TCP**:
  - 443 (HTTPS principal)
  - 80 (HTTP → HTTPS redirect; opcional se nao quiser HSTS)
- [ ] DNS interno aponta `api-gateway.tpb.corp` (ou nome escolhido) pro IP do servidor
- [ ] CA corp emitiu cert TLS pra esse hostname

### 1.3 SSH config recomendada

No seu workstation, em `~/.ssh/config`:

```ssh-config
Host api-gateway-prod
    HostName 10.x.x.x        # ou nome DNS interno
    User opadmin
    Port 22
    IdentityFile ~/.ssh/api-gateway-prod
    IdentitiesOnly yes
    ServerAliveInterval 60
    ServerAliveCountMax 3
```

---

## 2. Hardening inicial do host

Logue como usuario com `sudo` e siga em ordem.

### 2.1 Atualizar pacotes

```bash
sudo dnf upgrade -y
sudo dnf install -y vim curl wget git tar unzip jq lsof bind-utils
```

### 2.2 Time sync (chrony)

OL 9 ja vem com chrony; conferir:

```bash
sudo systemctl status chronyd
chronyc tracking
```

Pra usar NTP corp:

```bash
sudo vim /etc/chrony.conf
# Substituir os pool padrao pelos servidores NTP corp:
#   server ntp1.tpb.corp iburst
#   server ntp2.tpb.corp iburst
sudo systemctl restart chronyd
chronyc sources -v
```

### 2.3 SELinux

OL 9 vem com SELinux em modo **enforcing**. Manter assim. Containers Docker
funcionam OK porque tem o tipo `container_t` configurado. Bind mounts podem
precisar de relabel — vou cobrir isso na §6.

Conferir status:
```bash
getenforce      # esperado: Enforcing
sestatus
```

### 2.4 Firewall (firewalld)

```bash
sudo systemctl enable --now firewalld
sudo firewall-cmd --state          # esperado: running

# Permitir HTTPS + HTTP
sudo firewall-cmd --permanent --add-service=https
sudo firewall-cmd --permanent --add-service=http
sudo firewall-cmd --reload

# Conferir zonas ativas e portas abertas
sudo firewall-cmd --list-all
```

Se houver redes internas separadas (zona `trusted` ou similar), ajuste a
zona ativa antes de adicionar services.

### 2.5 Limites de sistema

Pra suportar alta concorrencia em SSE + chat completions, ajustar nofile:

```bash
sudo bash -c 'cat >> /etc/security/limits.d/99-api-gateway.conf <<EOF
*  soft  nofile  65536
*  hard  nofile  65536
EOF'
```

Verificar com `ulimit -n` apos relogar.

### 2.6 Swap

Em VM com 4 GB+ RAM, swap eh opcional mas evita OOM acidental:

```bash
sudo fallocate -l 4G /swapfile
sudo chmod 600 /swapfile
sudo mkswap /swapfile
sudo swapon /swapfile
echo '/swapfile none swap sw 0 0' | sudo tee -a /etc/fstab
```

### 2.7 Conferir conectividade outbound

Antes de instalar Docker, validar que o servidor alcanca o que precisa:

```bash
# SQL Server
nc -vz BRSPVPDEV003.tpb.corp 1433

# Azure
curl -fsSL -I https://danieldev.vault.azure.net | head -1
curl -fsSL -I https://login.microsoftonline.com | head -1

# Container registry
curl -fsSL -I https://registry-1.docker.io/v2/ | head -1
```

Se qualquer um falhar, voltar pra Infra corp pra liberar.

---

## 3. Instalar Docker CE em Oracle Linux 9

Oracle Linux nao tem Docker CE no repo oficial. Tem dois caminhos:

**Caminho A (recomendado):** instalar via repo Docker CE da CentOS — funciona
identico em OL.

**Caminho B (alternativa):** usar Podman, que **ja vem instalado em OL** e
tem CLI compativel. Ver §4 abaixo.

### 3.1 Adicionar repo Docker

```bash
sudo dnf -y install dnf-plugins-core
sudo dnf config-manager --add-repo \
    https://download.docker.com/linux/centos/docker-ce.repo
```

### 3.2 Remover Podman se houver conflito

Docker e Podman podem coexistir em namespaces diferentes, mas `docker compose`
e `podman-compose` podem confundir. Decidir um:

```bash
# Listar o que esta instalado
rpm -qa | grep -E 'podman|docker'

# Se for usar Docker exclusivamente, remover Podman:
sudo dnf -y remove podman buildah skopeo
```

### 3.3 Instalar Docker

```bash
sudo dnf -y install \
    docker-ce \
    docker-ce-cli \
    containerd.io \
    docker-buildx-plugin \
    docker-compose-plugin

sudo systemctl enable --now docker
docker --version           # esperado: Docker version 27.x ou superior
docker compose version     # esperado: Docker Compose version v2.x
```

### 3.4 Validar Docker

```bash
sudo docker run --rm hello-world
```

Esperar `Hello from Docker!`. Se falhar, conferir:

```bash
sudo journalctl -u docker.service --since "5 minutes ago"
```

### 3.5 Docker daemon — config recomendada

```bash
sudo mkdir -p /etc/docker
sudo bash -c 'cat > /etc/docker/daemon.json <<EOF
{
  "log-driver": "json-file",
  "log-opts": {
    "max-size": "100m",
    "max-file": "5"
  },
  "live-restore": true,
  "default-ulimits": {
    "nofile": { "Name": "nofile", "Hard": 65536, "Soft": 65536 }
  }
}
EOF'

sudo systemctl restart docker
```

`live-restore` permite manter containers rodando durante restart do daemon.
`log-opts` evita disco cheio por log infinito.

---

## 4. Alternativa: Podman + podman-compose

Se a politica corp obrigar Podman (Oracle preferencia oficial):

```bash
sudo dnf -y install podman podman-compose

# Confirmar
podman --version            # 4.x ou superior
podman-compose --version    # 1.x

# Habilitar socket pra `docker` CLI compat (opcional)
sudo systemctl enable --now podman.socket
```

**Cuidados com Podman**:

- `docker-compose.yml` desta deploy usa features nivel 3.8+, suportado por
  `podman-compose` >= 1.0.6.
- Rootless mode: rodar containers como usuario normal funciona, MAS o user
  precisa ter `subuid`/`subgid` configurados (`/etc/subuid`, `/etc/subgid`).
- Bind mount com SELinux: usar suffix `:Z` (private) ou `:z` (shared) nos
  volumes pra evitar permission denied. Compose tem `selinux:Z` em volume
  spec; nginx.conf etc. ja foram pensados pra isso.

No restante deste manual eu vou referenciar `docker compose`; pra Podman,
substituir por `podman-compose`.

---

## 5. Criar usuario operacional

Por seguranca, NAO rodar containers como root. Criar usuario dedicado:

```bash
sudo useradd -m -s /bin/bash -c "API Gateway operator" gateway-ops
sudo usermod -aG docker gateway-ops      # so se for Docker (nao Podman)
sudo passwd gateway-ops                  # opcional, pra sudo su
```

Para `gateway-ops` poder rodar comandos sem sudo:

```bash
# /etc/sudoers.d/gateway-ops
sudo bash -c 'cat > /etc/sudoers.d/gateway-ops <<EOF
gateway-ops ALL=(ALL) NOPASSWD: /usr/bin/systemctl restart docker
gateway-ops ALL=(ALL) NOPASSWD: /usr/bin/systemctl status docker
EOF'
sudo chmod 440 /etc/sudoers.d/gateway-ops
```

Logar como `gateway-ops` daqui pra frente:

```bash
sudo -iu gateway-ops
```

---

## 6. Provisionar diretorios + certs TLS

### 6.1 Layout no host

| Path | Permissao | Conteudo |
|---|---|---|
| `/opt/api-gateway/` | `gateway-ops:gateway-ops 750` | Clone do repo |
| `/etc/api-gateway/` | `root:gateway-ops 750` | Configs + secrets |
| `/etc/api-gateway/certs/` | `root:gateway-ops 750` | Certs TLS |
| `/etc/api-gateway/.env` | `root:gateway-ops 640` | Env vars do compose |
| `/var/log/api-gateway/` | `gateway-ops:gateway-ops 755` | Logs |

```bash
sudo mkdir -p /etc/api-gateway/certs /var/log/api-gateway/{nginx,gateway}
sudo chown -R root:gateway-ops /etc/api-gateway
sudo chmod 750 /etc/api-gateway /etc/api-gateway/certs
sudo chown -R gateway-ops:gateway-ops /var/log/api-gateway
```

### 6.2 SELinux relabel pros bind mounts

Containers veem essas paths via bind mount. SELinux precisa marcar como
`svirt_sandbox_file_t` (Podman) ou container-friendly:

```bash
sudo dnf -y install policycoreutils-python-utils

sudo semanage fcontext -a -t container_file_t '/etc/api-gateway(/.*)?'
sudo semanage fcontext -a -t container_file_t '/var/log/api-gateway(/.*)?'
sudo restorecon -Rv /etc/api-gateway /var/log/api-gateway
```

Verificar:
```bash
ls -lZ /etc/api-gateway
```

A label deve ser `container_file_t`.

### 6.3 Popular certs TLS

Seguir `infra/nginx/certs/README.md` (no repo) pra fluxo CSR → CA corp →
copiar `tls.crt`, `tls.key`, `ca.crt`. Resumo:

```bash
sudo openssl req -new -newkey rsa:4096 -nodes \
    -keyout /etc/api-gateway/certs/tls.key \
    -out /etc/api-gateway/certs/api-gateway.csr \
    -subj "/CN=api-gateway.tpb.corp/O=Teleperformance Brasil/C=BR" \
    -addext "subjectAltName=DNS:api-gateway.tpb.corp"

# Enviar api-gateway.csr pra time PKI corp → recebe tls.crt + ca.crt
# Copiar pro host:
sudo cp ~/tls.crt /etc/api-gateway/certs/
sudo cp ~/ca.crt  /etc/api-gateway/certs/
sudo chmod 644 /etc/api-gateway/certs/{tls.crt,ca.crt}
sudo chmod 600 /etc/api-gateway/certs/tls.key
sudo restorecon -Rv /etc/api-gateway/certs
```

Conferir validade:
```bash
sudo openssl x509 -in /etc/api-gateway/certs/tls.crt -noout -subject -issuer -dates
```

---

## 7. Clonar repo e checkout

### 7.1 Clonar (via SSH)

Como `gateway-ops`:

```bash
cd /opt
sudo chown gateway-ops:gateway-ops .
git clone git@github.com:D4nRossi/ai-gateway.git api-gateway
cd api-gateway
git checkout main         # ou release tag especifica
```

⚠️ Se GitHub estiver bloqueado pela politica corp: usar bundle. Workflow:

1. Workstation: `git bundle create api-gateway.bundle main`
2. `scp api-gateway.bundle gateway-ops@host:/opt/`
3. No host: `git clone /opt/api-gateway.bundle api-gateway && cd api-gateway && git checkout main`

### 7.2 Validar layout

```bash
ls apps/
# esperado: admin-api  console  gateway

ls infra/
# esperado: docker  nginx  legacy-windows

cat infra/docker/.env.example | head -5
```

Se nao tiver pastas `apps/` ou `infra/`, o checkout esta numa branch antiga
(pre 2026-06-02). Trocar pra `main`.

---

## 8. Configurar Azure auth

Gateway Go le KV no boot (resolve `${kv:NAME}` em `gateway.yaml`).

### 8.1 Opcao A — Managed Identity (VM Azure)

Se servidor for VM Azure com Managed Identity habilitada:

1. Portal Azure → VM → Identity → System assigned → On
2. Vault `danieldev` → Access policies → Add → Permitir `Get` em Secrets pra
   esta MI
3. **Nenhuma env var precisa ser setada no host** — SDK detecta automatico

### 8.2 Opcao B — Service Principal (host nao-Azure)

Mais comum em servidor on-premise corp.

1. **No portal** ou via CLI, criar SP:
   ```bash
   az ad sp create-for-rbac \
       --name "api-gateway-prod" \
       --skip-assignment \
       --tenant c050c98c-b463-4591-ac3b-deb782c0ba6e
   ```
   Anotar `appId`, `password`, `tenant`.

2. **Dar acesso ao vault**:
   ```bash
   az keyvault set-policy \
       --vault-name danieldev \
       --spn <appId> \
       --secret-permissions get list
   ```

3. **Popular `.env` do compose** (proxima §):
   ```env
   AZURE_TENANT_ID=c050c98c-b463-4591-ac3b-deb782c0ba6e
   AZURE_CLIENT_ID=<appId>
   AZURE_CLIENT_SECRET=<password>
   ```

⚠️ Senha do SP eh **secret**. Em prod corp ideal eh tambem ela ir pra um
secret manager (KV separado, HashiCorp Vault) e ser injetada via wrapper
no boot do compose. Pra fase atual, o .env com `chmod 640` jah eh aceitavel
desde que servidor seja dedicado.

### 8.3 Validar resolucao de KV

Antes de subir o compose, testar do host:

```bash
# Instalar az CLI temporariamente (opcional)
sudo dnf -y install dnf-plugins-core
sudo rpm --import https://packages.microsoft.com/keys/microsoft.asc
sudo dnf -y install azure-cli

# Login com SP
az login --service-principal \
    --username $AZURE_CLIENT_ID \
    --password $AZURE_CLIENT_SECRET \
    --tenant $AZURE_TENANT_ID

# Listar secrets
az keyvault secret list --vault-name danieldev --query "[].name" -o tsv
# esperado: AZURE-OPENAI-API-KEY, AzureAIGateway-DB-Password-hom, DB-ENCRYPTION-KEY, ...
```

---

## 9. Validar conectividade SQL Server

Container `gateway` precisa alcancar `BRSPVPDEV003.tpb.corp:1433`. Testar do host:

```bash
nc -vz BRSPVPDEV003.tpb.corp 1433
# Connection to BRSPVPDEV003.tpb.corp 1433 port [tcp/*] succeeded!
```

Se falhar, voltar pra Infra corp pra liberar.

Testar de DENTRO do container (apos compose up):

```bash
docker compose exec gateway sh -c \
    'wget -q -O- http://localhost:8080/readyz | head'
```

Esperado: JSON com `status: ok` ou similar.

---

## 10. .env + secrets do compose

```bash
cd /opt/api-gateway/infra/docker
cp .env.example .env

# Permissoes restritas — senha SP esta dentro
sudo chown root:gateway-ops .env
sudo chmod 640 .env
sudo restorecon -v .env

# Editar
sudo vim .env
```

Preencher minimamente:

```env
KEYVAULT_URI=https://danieldev.vault.azure.net/
AZURE_TENANT_ID=c050c98c-b463-4591-ac3b-deb782c0ba6e
AZURE_CLIENT_ID=<appId-do-SP>
AZURE_CLIENT_SECRET=<password-do-SP>
AZURE_OPENAI_ENDPOINT=https://danie-mc4ryviy-westeurope.cognitiveservices.azure.com
PROVIDER=azure
LOG_LEVEL=info
MIGRATIONS_AUTO_APPLY=true
CERTS_DIR=/etc/api-gateway/certs
LOG_DIR=/var/log/api-gateway
```

Validar sem subir nada:

```bash
docker compose --env-file .env config -q
# saida vazia + exit 0 = OK
```

Conferir que segredos nao aparecem nos logs do compose (se aparecerem,
revisar):

```bash
docker compose --env-file .env config | grep -i secret
```

---

## 11. Primeiro boot

### 11.1 Build (primeira vez, ~5-10 min)

```bash
cd /opt/api-gateway/infra/docker
docker compose --env-file .env build --pull
```

Builda 3 imagens:
- `api-gateway-gateway` (apps/gateway/Dockerfile, Go binary)
- `api-gateway-console` (apps/console/Dockerfile, Vite build + nginx)
- nginx terminator usa imagem `nginx:1.27-alpine` direto

### 11.2 Subir

```bash
docker compose --env-file .env up -d
```

Acompanhar:

```bash
docker compose ps
docker compose logs -f gateway      # ver bootstrap sequence
```

Esperado dentro de 30s:

```
gateway-1  | {"level":"info","msg":"ai gateway starting",...}
gateway-1  | {"level":"info","msg":"sqlserver connected","host":"BRSPVPDEV003.tpb.corp",...}
gateway-1  | {"level":"info","msg":"migrations applied"...}
gateway-1  | {"level":"info","msg":"http server listening","addr":":8080"}
nginx-1    | nginx ready
```

`Ctrl+C` pra sair dos logs (containers seguem rodando).

### 11.3 Conferir status

```bash
docker compose ps
# Todos os 3 servicos com STATUS = "Up X seconds (healthy)"
```

Se algum estiver `unhealthy` ou `restarting`, ver §17 (troubleshooting).

---

## 12. Smoke test end-to-end

### 12.1 nginx terminator

```bash
# HTTP redirect pra HTTPS (esperado: 301 Moved Permanently)
curl -I http://api-gateway.tpb.corp

# HTTPS handshake + health
curl -sSf https://api-gateway.tpb.corp/__nginx_healthz
# esperado: ok
```

Se browser de workstation aceita o cert, navegar pra
`https://api-gateway.tpb.corp/`.

### 12.2 Gateway data plane

```bash
# Healthz publico (sem auth)
curl -sSf https://api-gateway.tpb.corp/healthz | jq .

# Readyz (testa DB)
curl -sSf https://api-gateway.tpb.corp/readyz | jq .
```

### 12.3 Console

Browser em `https://api-gateway.tpb.corp/` — tela de login do admin console.

### 12.4 Login admin (root)

Apos migration 010, existe um usuario `root` com senha temporaria
`Adm!nGogateway2026`. Trocar IMEDIATAMENTE no primeiro login.

Fluxo:
1. Browser: <https://api-gateway.tpb.corp/ui/login>
2. Logar com `root` / `Adm!nGogateway2026`
3. Trocar senha; idealmente criar admin pessoal e desativar `root`

### 12.5 Chat completion via API

```bash
# Token de uma application cadastrada (gwk_*)
TOKEN=gwk_yourapp_realkey

curl -sSf -X POST https://api-gateway.tpb.corp/v1/chat/completions \
    -H "Authorization: Bearer $TOKEN" \
    -H "Content-Type: application/json" \
    -d '{
        "model": "gpt-4.1-mini",
        "messages": [{"role":"user","content":"diga oi"}]
    }' | jq .
```

Esperado: response OpenAI-compatible com `choices[0].message.content`.

### 12.6 SSE streaming

```bash
curl -N -X POST https://api-gateway.tpb.corp/v1/chat/completions \
    -H "Authorization: Bearer $TOKEN" \
    -H "Content-Type: application/json" \
    -d '{"model":"gpt-4.1-mini","messages":[{"role":"user","content":"oi"}],"stream":true}'
```

Esperado: chunks `data: {"...}` chegando progressivamente. Se vier tudo de
uma vez no final, nginx esta bufferizando (ver §17).

### 12.7 Validar usage no DB

Connect com cliente SQL ao SQL Server e:

```sql
USE AzureAI_Gateway_hom;
SELECT TOP 5 request_id, application_name, model, status_code, latency_ms, cost_brl
FROM gogateway.usage_events
ORDER BY created_at DESC;
```

Linha de cada request acima deve aparecer.

---

## 13. Acesso ao admin console

URL: `https://api-gateway.tpb.corp/ui/`

Funcionalidades disponiveis (provider Go atual; admin-api .NET migra slice
a slice — ADR-0027):

- `/ui/applications` — CRUD apps + show-once API key
- `/ui/endpoints` — proxy endpoints + targets
- `/ui/users` — admin users
- `/ui/observability` — usage / audit / budget
- `/ui/dashboard` — timeseries + breakdown
- `/ui/playground` — testar chat ou proxy

---

## 14. Logs + monitoring

### 14.1 Logs estruturados

JSON estruturado vai pra stdout/stderr de cada container, capturado pelo
log driver `json-file`:

```bash
# Real-time
docker compose logs -f --tail=200 gateway
docker compose logs -f --tail=200 nginx

# Grep request_id
docker compose logs gateway | jq -R 'fromjson? | select(.request_id == "abc-123")'
```

### 14.2 Bind mount logs

`infra/docker/docker-compose.yml` faz bind de `/var/log/api-gateway/nginx`
e `/gateway`. Pra agregador externo (filebeat, fluentbit, vector), apontar
pra esses paths.

### 14.3 Metricas

Ainda sem Prometheus integrado (fica pra ondas futuras). Por enquanto:

- Header `X-Gateway-Latency-Breakdown` em cada response (ADR-0021)
- Linha em `usage_events` por request

### 14.4 Healthchecks

Compose monitora `/healthz` (cada 30s, 3 retries). Se um servico ficar
`unhealthy`, `restart: unless-stopped` derruba e sobe de novo.

---

## 15. Atualizar versao

Fluxo seguro de upgrade:

```bash
cd /opt/api-gateway
git fetch
git checkout v2.5.0          # ou commit/branch alvo
cd infra/docker
docker compose --env-file .env pull        # se imagens vierem de registry
docker compose --env-file .env build       # rebuild local
docker compose --env-file .env up -d       # zero-ish downtime se ja healthy
```

Reload nginx sem downtime (se so config mudou):
```bash
docker compose exec nginx nginx -t \
    && docker compose exec nginx nginx -s reload
```

### 15.1 Migration nova

Por padrao `MIGRATIONS_AUTO_APPLY=true` — gateway aplica no boot. Em prod
hardening, setar `false` no `.env` e rodar manual:

```bash
# Modo manual (ADR-0025)
docker compose --env-file .env stop gateway
DATABASE_URL='sqlserver://...' \
  migrate -database "$DATABASE_URL" -path /opt/api-gateway/apps/gateway/migrations up
docker compose --env-file .env start gateway
```

(O cliente `migrate` precisa ser instalado no host — `go install`
`github.com/golang-migrate/migrate/v4/cmd/migrate@latest`.)

---

## 16. Rollback

### 16.1 Codigo + compose

```bash
cd /opt/api-gateway
git checkout <previous-tag>
cd infra/docker
docker compose --env-file .env up -d --build
```

### 16.2 Schema

`golang-migrate` suporta down, mas em prod NUNCA rodar em janela curta;
abrir incidente e revisar. Comando seria:

```bash
migrate -database "$DATABASE_URL" -path apps/gateway/migrations down 1
```

⚠️ Cada down pode perder dados. Conferir o `.down.sql` antes.

---

## 17. Troubleshooting

### 17.1 Container `gateway` em `Restarting`

```bash
docker compose logs gateway | tail -100
```

Causas comuns:

| Erro no log | Causa | Solucao |
|---|---|---|
| `loading config: ... no such file` | `CONFIG_PATH` errado ou yaml nao montado | Conferir bind mount e `CONFIG_PATH` no `.env` |
| `KEYVAULT_URI ... ${kv:NAME} resolve falhou` | SP sem permissao no vault | Refazer `az keyvault set-policy` |
| `connecting to sqlserver ... i/o timeout` | Firewall corp / VPN nao alcanca SQL | `nc -vz BRSPVPDEV003 1433` do host |
| `connecting to sqlserver ... login failed` | Senha SQL errada no KV | `az keyvault secret show --vault-name danieldev --name AzureAIGateway-DB-Password-hom` |
| `schema check: dirty=1` | Migration anterior parou no meio | Ver `Comandos/Limpar-Dirty-Migration` ou rodar manual |

### 17.2 nginx `unhealthy`

```bash
docker compose exec nginx nginx -t
```

Erros de sintaxe / cert errado:

```
nginx: [emerg] cannot load certificate "/etc/nginx/certs/tls.crt"
```

Conferir que bind mount esta correto e cert existe:
```bash
ls -lZ /etc/api-gateway/certs/
```

SELinux: se output mostrar label diferente de `container_file_t`, rodar
`sudo restorecon -Rv /etc/api-gateway/certs`.

### 17.3 Browser: ERR_SSL_VERSION_OR_CIPHER_MISMATCH

Cert TLS muito antigo ou cifra nao suportada pelo client. Confirmar:

```bash
openssl s_client -connect api-gateway.tpb.corp:443 -servername api-gateway.tpb.corp
```

`Protocol` deve mostrar TLSv1.2 ou TLSv1.3. Se TLS 1.0/1.1, atualizar
nginx.conf (`ssl_protocols`) — ja esta corretto neste manual.

### 17.4 Browser: NET::ERR_CERT_AUTHORITY_INVALID

CA corp nao esta no trust store do browser. Distribuir `ca.crt` via GPO
(Windows) ou perfil corp (Mac/Linux). Pra workstation pessoal, importar
manualmente.

### 17.5 SSE chega em bloco no final (nao streaming)

```bash
docker compose exec nginx grep -A 5 'location /v1/' /etc/nginx/nginx.conf
```

Conferir `proxy_buffering off;`. Se faltar, recolocar e `nginx -s reload`.

### 17.6 401 Unauthorized em `/v1/...`

```bash
# Conferir que token existe no DB
echo -n 'gwk_yourapp_realkey' | sha256sum | cut -d' ' -f1
# Comparar com api_keys.key_hash na tabela
```

### 17.7 Healthcheck do compose marca gateway como unhealthy mesmo respondendo

`wget` pode estar ausente na imagem alpine — checar Dockerfile (`apk add`
inclui `wget`?). Como fallback temporario, mudar healthcheck pra `curl`
ou `nc`.

### 17.8 Logs cheios de `bind: address already in use`

Outro processo escutando 80/443 no host:

```bash
sudo ss -tlnp | grep -E ':(80|443) '
```

Provavelmente `httpd` ou `nginx` antigo. Parar / desinstalar:

```bash
sudo systemctl stop httpd nginx 2>/dev/null
sudo systemctl disable httpd nginx 2>/dev/null
```

### 17.9 SELinux block (denial logs)

```bash
sudo ausearch -m AVC -ts recent
```

Se ver denials apontando pros bind mounts:

```bash
sudo audit2allow -a
```

Idealmente: relabel com `restorecon` (§6.2). Em ultimo caso e POR
**tempo limitado**, `setenforce 0` pra debugar, NUNCA deixar em permissive
em prod.

---

## 18. Manutencao recorrente

### 18.1 Rotacao de logs

`json-file` driver ja rotaciona (`max-size: 100m`, `max-file: 5`). Pra
nginx logs no bind mount:

```bash
sudo bash -c 'cat > /etc/logrotate.d/api-gateway-nginx <<EOF
/var/log/api-gateway/nginx/*.log {
    daily
    rotate 14
    missingok
    notifempty
    compress
    delaycompress
    sharedscripts
    postrotate
        docker compose -f /opt/api-gateway/infra/docker/docker-compose.yml exec nginx nginx -s reopen
    endscript
}
EOF'
```

### 18.2 Limpeza Docker

Mensal:

```bash
docker system prune -af --filter "until=720h"
docker volume prune
```

### 18.3 Renovacao de cert

30 dias antes do expire (ver §6.3 do `infra/nginx/certs/README.md`).

### 18.4 Backup

SQL Server eh corp + backup pelo time de DBA. Ainda assim, anotar:

- Schema autoritativa: `migrations/` no repo (versionado)
- KV: backup automatico Azure
- `.env` em `/etc/api-gateway/`: backup manual em vault corp (NAO commitar!)

### 18.5 Audit/usage retention

`usage_events` cresce ~1 linha por request. Em volume alto, considerar
retencao via job SQL Server:

```sql
DELETE FROM gogateway.usage_events
WHERE created_at < DATEADD(month, -6, SYSUTCDATETIME());
```

(Rotinar via SQL Agent — fora do escopo deste manual.)

---

## 19. Apendice — checklist de aprovacao corp

Pra entregar pra time de seguranca/compliance:

- [ ] Servidor com OL 9 patched (`sudo dnf updateinfo list security`)
- [ ] SELinux enforcing (`getenforce` = `Enforcing`)
- [ ] Firewall ativo, so 80+443 abertos (`firewall-cmd --list-all`)
- [ ] Cert TLS valido, emitido pela CA corp
- [ ] TLS 1.2+ apenas, ciphers modernas (`openssl s_client`)
- [ ] HSTS habilitado (`curl -I https://...` mostra `Strict-Transport-Security`)
- [ ] Container rodando como usuario nao-root (verificar `USER` no Dockerfile)
- [ ] Logs estruturados JSON (sem prompts/tokens em claro)
- [ ] Rate limit em `/admin/v1/auth/login`
- [ ] Segredos via KV (NAO no .env em texto plano onde possivel)
- [ ] Migrations testadas em homolog antes de prod
- [ ] Backup do .env do compose vault corp
- [ ] Smoke test passou (`/healthz`, `/readyz`, login, chat, SSE)

---

## Referencias

- ADR-0022 — SQL Server corporativo (`docs/adrs/0022-troca-postgres-sqlserver.md`)
- ADR-0025 — `MIGRATIONS_AUTO_APPLY` toggle
- ADR-0027 — admin plane .NET (rolagem futura slice a slice)
- ADR-0028 — frontend extraido do binario
- ADR-0030 — monorepo `apps/` + `infra/`
- `infra/docker/README.md` — visao operacional do compose
- `infra/nginx/certs/README.md` — fluxo de cert TLS
- Memoria `architecture_pivot_2026_06.md` — virada arquitetural completa
- Oracle Linux 9 docs: https://docs.oracle.com/en/operating-systems/oracle-linux/9/
- Docker CE docs (instalacao em CentOS-like): https://docs.docker.com/engine/install/centos/
