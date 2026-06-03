# infra/scripts — automação de deploy

Scripts auxiliares pra operação do API Gateway. **Source of truth** continua
no guia manual (`docs/deploy/oracle-linux.md`) — esse script só automatiza o
que dá pra automatizar.

## `deploy-oracle-linux.sh`

Bash idempotente que cobre as fases do guia em Oracle Linux 9. Cada função
checa estado antes de agir; rodar 2x não quebra.

### Uso típico

```bash
# Primeira execução completa, com confirmação em cada destrutivo
sudo ./deploy-oracle-linux.sh

# Modo "tô com pressa, autorizo tudo"
sudo ./deploy-oracle-linux.sh --yes

# Dry-run pra revisar o que vai rodar sem executar
sudo ./deploy-oracle-linux.sh --dry-run

# Pular fases que você já fez à mão (ex.: hardening já feito por outro script)
sudo ./deploy-oracle-linux.sh --skip harden_host,install_docker

# Só validar pré-requisitos + outbound
sudo ./deploy-oracle-linux.sh --only check_prereqs,validate_outbound

# Deploy de uma tag específica
sudo ./deploy-oracle-linux.sh --release v2.5.0 --yes
```

### Fases disponíveis (ordem de execução)

| # | Fase | O que faz | Idempotente? |
|---|---|---|---|
| 1 | `check_prereqs` | Detecta OS (warn se não OL9), valida outbound básico, ferramentas | sim |
| 2 | `harden_host` | dnf upgrade, chrony, firewalld https/http, SELinux check, ulimits | sim |
| 3 | `install_docker` | Repo Docker CE, instala packages, daemon config, hello-world | sim (pula se Docker já presente) |
| 4 | `create_ops_user` | `gateway-ops` user + group docker + sudoers.d | sim |
| 5 | `provision_dirs` | `/etc/api-gateway/`, `/var/log/api-gateway/`, SELinux relabel | sim |
| 6 | `check_certs` | Valida `tls.crt`/`tls.key`/`ca.crt` em `/etc/api-gateway/certs/`. Se faltam, **gera CSR e para** pedindo intervenção CA corp | sim |
| 7 | `clone_or_update` | git clone ou fetch + checkout de `--release` se passada; valida layout monorepo | sim |
| 8 | `azure_check` | Instala az CLI, tenta `az login --service-principal` se vars setadas, lista KV (warn em falha) | sim |
| 9 | `validate_outbound` | `nc` SQL Server, `curl` KV, `login.microsoftonline.com` | sim |
| 10 | `check_env` | Valida `.env` presente + keys obrigatórias preenchidas (rejeita `__FROM_KV__` placeholders) | sim |
| 11 | `build_and_start` | `docker compose build --pull && up -d` | sim |
| 12 | `wait_healthy` | Polling até os 4 containers ficarem `(healthy)`. Timeout 5 min | n/a (loop) |
| 13 | `run_migrations_dotnet` | `docker compose run --rm admin-api migrate up` (opcional; preserva ADR-0025) | sim |
| 14 | `smoke_test` | Curl em `/healthz`, `/readyz`, login admin via .NET. Falha o script se 500 | n/a (read) |

### O que o script **NÃO** faz (intencionalmente)

- **Não cria Service Principal Azure** — operação privilegiada que precisa
  decisão sobre escopo/permissões. Owner cria via portal/`az ad sp create-for-rbac`
  e popula `.env` antes de rodar a fase `azure_check`
- **Não envia CSR pra CA corp** — apenas gera o CSR e para com mensagem
  pedindo pra você submeter externamente
- **Não popula `.env` com secrets** — só valida que existe e está preenchido.
  Owner resolve via `az keyvault secret show ...` manual
- **Não rola nginx routing entre slices** — Fase 4d (admin .NET tem só Auth
  rolado) é o estado vigente do `infra/nginx/nginx.conf`. Pra rolar mais
  slices, editar nginx.conf e `docker compose exec nginx nginx -s reload`
  manual
- **Não faz rollback automático** — se algo falhar, script para; owner
  corrige e re-roda. Pra rollback completo: `docker compose down && git checkout <previous>`

### Fluxo recomendado pra primeira vez (deploy real corp)

```bash
# 1. Na sua workstation, certifique-se que:
#    - SP Azure criado e tem "Get/List" no KV
#    - CSR enviado pra time PKI corp, recebeu tls.crt + ca.crt

# 2. SCP os certs e .env pra VM
scp tls.crt ca.crt opadmin@servidor:/tmp/
scp .env opadmin@servidor:/tmp/

# 3. Conecta na VM
ssh opadmin@servidor

# 4. Move certs e .env pros paths esperados (script consome de lá)
sudo mkdir -p /etc/api-gateway/certs
sudo cp /tmp/tls.crt /tmp/ca.crt /etc/api-gateway/certs/
sudo cp /tmp/.env /etc/api-gateway/.env
sudo chmod 640 /etc/api-gateway/.env
sudo chown root:gateway-ops /etc/api-gateway/.env
sudo rm /tmp/tls.crt /tmp/ca.crt /tmp/.env

# 5. Clone do repo onde o script vive
sudo git clone git@github.com:D4nRossi/ai-gateway.git /opt/api-gateway-bootstrap

# 6. Roda o script
cd /opt/api-gateway-bootstrap/infra/scripts
sudo ./deploy-oracle-linux.sh --release v2.5.0
```

### Fluxo recomendado pra simulação em VM dev

Mesmo das instruções acima, **mas** com 2 ajustes:

1. **Pré-popular `.env`** com valores reais do KV resolvidos manualmente
   (não há SP em dev geralmente):

   ```bash
   az login                                          # interativo
   DB_PASSWORD=$(az keyvault secret show --vault-name danieldev --name AzureAIGateway-DB-Password-hom --query value -o tsv)
   AES_KEY=$(az keyvault secret show --vault-name danieldev --name DB-ENCRYPTION-KEY --query value -o tsv)
   # Edita .env trocando placeholders pelos valores
   ```

2. **Cert self-signed** pra evitar fricção com CA corp pra VM dev:

   ```bash
   sudo openssl req -x509 -newkey rsa:2048 -nodes -days 365 \
       -keyout /etc/api-gateway/certs/tls.key \
       -out    /etc/api-gateway/certs/tls.crt \
       -subj   "/CN=api-gateway-dev.local"
   sudo cp /etc/api-gateway/certs/tls.crt /etc/api-gateway/certs/ca.crt
   ```

   Browser vai dar warning de TLS — clique "advanced → proceed". Curl precisa de `-k`.

3. **DNS local** apontando `api-gateway.tpb.corp` → IP da VM (ou rodar via IP direto + skip de smoke test):

   ```bash
   echo "127.0.0.1 api-gateway.tpb.corp" | sudo tee -a /etc/hosts
   ```

   Ou rode com `--skip smoke_test` se preferir validar manual.

### Troubleshooting comum

| Sintoma | Causa provável | Fix |
|---|---|---|
| `phase install_docker falhou` | Repo Docker CE inacessível (proxy corp) | Configurar proxy via `/etc/dnf/dnf.conf` ou rodar com pacotes pré-baixados |
| `phase check_certs falhou`, CSR gerado | Cert TLS ainda não emitido pela CA corp | Submeter CSR; receber tls.crt + ca.crt; copiar pra `/etc/api-gateway/certs/`; re-rodar `--only check_certs,build_and_start,wait_healthy,smoke_test` |
| `phase check_env falhou, placeholder não preenchido` | `.env` ainda tem `__FROM_KV__` literal | `az keyvault secret show ... -o tsv` + edit no `.env` |
| `phase wait_healthy timeout` | Algum container quebrou no boot | `docker compose --env-file /etc/api-gateway/.env ps` + `docker compose logs <svc>` |
| `phase smoke_test login falhou, response não tem token` | admin-api .NET crashou OU SQL/KV não acessível do container | `docker compose logs admin-api`; conferir env `Database__EncryptionKeyHex` e `ConnectionStrings__Gateway` |

### Roadmap futuro

- **Ansible playbook** quando precisar deploy em **N VMs**: o bash pode ser
  traduzido fácil (cada função vira task com módulo equivalente)
- **Healthcheck mais sensível** que valida não só Docker mas também alcance
  ao SQL e KV dentro do container (curl interno)
- **Smoke test mais profundo**: validar audit row, sessões expiradas
  (`gogateway.admin_sessions WHERE revoked_at IS NULL`), e que /admin/v1/auth
  realmente passa pelo container .NET (header de servidor)

## Licença

Mesmo do repo (interno corporativo TP).
