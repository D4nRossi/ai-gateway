#!/usr/bin/env bash
#
# deploy-oracle-linux.sh — automação idempotente do deploy do API Gateway
# em Oracle Linux 9 (Docker CE + nginx + compose multi-app).
#
# Cobre as fases do docs/deploy/oracle-linux.md. Cada função é idempotente:
# rodar 2x não quebra; pular o que já foi feito.
#
# Uso:
#   sudo ./deploy-oracle-linux.sh               # roda tudo, perguntando confirmação
#   sudo ./deploy-oracle-linux.sh --yes         # roda tudo sem perguntar
#   sudo ./deploy-oracle-linux.sh --dry-run     # imprime o que faria, sem executar
#   sudo ./deploy-oracle-linux.sh --only check_prereqs,validate_outbound
#   sudo ./deploy-oracle-linux.sh --skip harden_host,install_docker
#   sudo ./deploy-oracle-linux.sh --release v2.5.0  # checkout tag/commit específico
#
# Não cobre (precisa intervenção humana):
#   - Criar Service Principal no Azure (az ad sp create-for-rbac)
#   - Submeter CSR pra CA corp (gera o CSR, mas o cert volta manualmente)
#   - Popular .env com segredos reais (KV secrets, SP credentials)
#
# Pre-requisitos:
#   - Oracle Linux 9.x atualizado
#   - Usuário com sudo NOPASSWD ou rodar com sudo
#   - Conectividade outbound pra: registry container, Azure, SQL corp
#
# References:
#   - docs/deploy/oracle-linux.md — guia operacional completo
#   - infra/docker/docker-compose.yml — orquestração alvo
#   - ADR-0030 — monorepo apps/
#
# Author: API Gateway team (gerado em 2026-06-02)

set -euo pipefail

# ── Constantes ──────────────────────────────────────────────────────────────
SCRIPT_NAME="deploy-oracle-linux.sh"
SCRIPT_VERSION="1.0.0"
REPO_DIR_DEFAULT="/opt/api-gateway"
CONFIG_DIR="/etc/api-gateway"
CERTS_DIR="${CONFIG_DIR}/certs"
LOG_DIR="/var/log/api-gateway"
OPS_USER="gateway-ops"
ENV_FILE_DEFAULT="${CONFIG_DIR}/.env"
HEALTHY_TIMEOUT_SECONDS=300
SQL_HOST_DEFAULT="BRSPVPDEV003.tpb.corp"
SQL_PORT="1433"
KV_HOST_DEFAULT="danieldev.vault.azure.net"
DEPLOY_TRACE_FILE="/var/log/api-gateway/deploy-$(date +%Y%m%d-%H%M%S).log"

# Lista canônica das funções/fases na ordem de execução.
PHASES=(
    check_prereqs
    harden_host
    install_docker
    create_ops_user
    provision_dirs
    check_certs
    clone_or_update
    azure_check
    validate_outbound
    check_env
    build_and_start
    wait_healthy
    run_migrations_dotnet
    smoke_test
)

# ── Flags ───────────────────────────────────────────────────────────────────
DRY_RUN=0
ASSUME_YES=0
ONLY_PHASES=""
SKIP_PHASES=""
REPO_DIR="${REPO_DIR_DEFAULT}"
ENV_FILE="${ENV_FILE_DEFAULT}"
RELEASE_REF=""
SQL_HOST="${SQL_HOST_DEFAULT}"
KV_HOST="${KV_HOST_DEFAULT}"

# ── ANSI colors (no-op se TERM não suporta) ─────────────────────────────────
if [[ -t 1 ]]; then
    C_RED='\033[0;31m'; C_GREEN='\033[0;32m'; C_YELLOW='\033[0;33m'
    C_BLUE='\033[0;34m'; C_BOLD='\033[1m'; C_RESET='\033[0m'
else
    C_RED=''; C_GREEN=''; C_YELLOW=''; C_BLUE=''; C_BOLD=''; C_RESET=''
fi

# ── Helpers de log ──────────────────────────────────────────────────────────
log_info()  { printf "%b[INFO]%b  %s\n" "${C_BLUE}"   "${C_RESET}" "$*"; }
log_ok()    { printf "%b[OK]%b    %s\n" "${C_GREEN}"  "${C_RESET}" "$*"; }
log_warn()  { printf "%b[WARN]%b  %s\n" "${C_YELLOW}" "${C_RESET}" "$*"; }
log_err()   { printf "%b[ERROR]%b %s\n" "${C_RED}"    "${C_RESET}" "$*" >&2; }
log_phase() { printf "\n%b━━━ %s ━━━%b\n" "${C_BOLD}" "$*" "${C_RESET}"; }

# Executa um comando, ou apenas imprime em dry-run.
run() {
    if (( DRY_RUN )); then
        printf "%b[DRY]%b   %s\n" "${C_YELLOW}" "${C_RESET}" "$*"
        return 0
    fi
    eval "$@"
}

# Confirma se ASSUME_YES não está setado. Retorna 0 = continuar, 1 = abortar.
confirm() {
    local prompt="$1"
    if (( ASSUME_YES )); then
        log_info "auto-confirmando: $prompt"
        return 0
    fi
    local reply
    read -r -p "$(printf "%b%s [y/N]:%b " "${C_BOLD}" "$prompt" "${C_RESET}") " reply
    [[ "$reply" =~ ^[Yy]([Ee][Ss])?$ ]]
}

# Decide se uma phase deve rodar dado --only/--skip.
phase_enabled() {
    local phase="$1"
    if [[ -n "$ONLY_PHASES" ]]; then
        [[ ",${ONLY_PHASES}," == *",${phase},"* ]] && return 0 || return 1
    fi
    if [[ -n "$SKIP_PHASES" ]]; then
        [[ ",${SKIP_PHASES}," == *",${phase},"* ]] && return 1 || return 0
    fi
    return 0
}

# Verifica que o caller é root (sudo). Várias phases precisam.
require_root() {
    if [[ "$(id -u)" -ne 0 ]]; then
        log_err "${1:-este passo} requer sudo/root"
        exit 1
    fi
}

# ── Argument parsing ────────────────────────────────────────────────────────
print_usage() {
    cat <<EOF
${SCRIPT_NAME} v${SCRIPT_VERSION} — automação deploy Oracle Linux 9

Usage:
  sudo $0 [options]

Options:
  --dry-run                Imprime cada comando sem executar
  --yes                    Auto-confirma operações destrutivas
  --only PHASES            Roda apenas as fases listadas (csv)
  --skip PHASES            Pula as fases listadas (csv)
  --release REF            Git tag/branch/commit pra checkout (default: main)
  --repo-dir PATH          Override do path do repo (default: ${REPO_DIR_DEFAULT})
  --env-file PATH          Override do path do .env (default: ${ENV_FILE_DEFAULT})
  --sql-host HOST          Override do SQL Server host (default: ${SQL_HOST_DEFAULT})
  --kv-host HOST           Override do Key Vault host (default: ${KV_HOST_DEFAULT})
  --list                   Lista as fases na ordem
  -h, --help               Mostra essa ajuda

Fases (na ordem): ${PHASES[@]}
EOF
}

parse_args() {
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --dry-run) DRY_RUN=1; shift ;;
            --yes|-y) ASSUME_YES=1; shift ;;
            --only) ONLY_PHASES="$2"; shift 2 ;;
            --skip) SKIP_PHASES="$2"; shift 2 ;;
            --release) RELEASE_REF="$2"; shift 2 ;;
            --repo-dir) REPO_DIR="$2"; shift 2 ;;
            --env-file) ENV_FILE="$2"; shift 2 ;;
            --sql-host) SQL_HOST="$2"; shift 2 ;;
            --kv-host) KV_HOST="$2"; shift 2 ;;
            --list) printf "%s\n" "${PHASES[@]}"; exit 0 ;;
            -h|--help) print_usage; exit 0 ;;
            *) log_err "argumento desconhecido: $1"; print_usage; exit 1 ;;
        esac
    done
}

# ╔═══════════════════════════════════════════════════════════════════════════╗
# ║                              FASES                                         ║
# ╚═══════════════════════════════════════════════════════════════════════════╝

# §1 — Pré-requisitos: OS, sudo, conectividade básica.
check_prereqs() {
    log_phase "check_prereqs"

    # OS check
    if [[ ! -f /etc/os-release ]]; then
        log_err "/etc/os-release ausente — não consigo detectar a distro"
        return 1
    fi
    . /etc/os-release
    if [[ "${ID:-}" != "ol" && "${ID:-}" != "rhel" && "${ID_LIKE:-}" != *"rhel"* ]]; then
        log_warn "este script foi escrito pra Oracle Linux 9; detectado: ${ID:-desconhecido} ${VERSION_ID:-?}"
        confirm "continuar mesmo assim?" || return 1
    else
        log_ok "OS: ${PRETTY_NAME:-$ID $VERSION_ID}"
    fi

    # Internet outbound básica
    if ! curl -fsSL -m 5 -o /dev/null https://registry-1.docker.io/v2/; then
        log_warn "registry-1.docker.io inalcançável — verifique proxy/firewall corp"
    else
        log_ok "registry container acessível"
    fi

    # Required tools (já vem no OL9 base normalmente)
    local tool
    for tool in curl wget jq nc openssl tar; do
        if ! command -v "$tool" >/dev/null 2>&1; then
            log_warn "$tool não instalado — será instalado em harden_host"
        fi
    done

    log_ok "check_prereqs"
}

# §2 — Hardening: dnf upgrade, chrony, firewalld, SELinux, ulimits.
harden_host() {
    log_phase "harden_host"
    require_root "harden_host"

    log_info "atualizando pacotes (dnf upgrade -y)"
    confirm "rodar dnf upgrade -y agora?" || { log_warn "skip upgrade"; }
    if (( ASSUME_YES )) || [[ -z "${REPLY:-}" || "$REPLY" =~ ^[Yy] ]]; then
        run "sudo dnf upgrade -y"
    fi

    log_info "instalando utilitários de base"
    run "sudo dnf install -y vim curl wget git tar unzip jq lsof bind-utils policycoreutils-python-utils"

    log_info "chrony enable + start"
    run "sudo systemctl enable --now chronyd"

    log_info "firewalld enable + start"
    run "sudo systemctl enable --now firewalld"

    log_info "libera HTTPS + HTTP no firewall (idempotente)"
    run "sudo firewall-cmd --permanent --add-service=https || true"
    run "sudo firewall-cmd --permanent --add-service=http || true"
    run "sudo firewall-cmd --reload"

    # SELinux deve ficar enforcing
    local selinux_mode
    selinux_mode=$(getenforce 2>/dev/null || echo "Unknown")
    if [[ "$selinux_mode" != "Enforcing" ]]; then
        log_warn "SELinux em modo $selinux_mode — recomendado: Enforcing"
    else
        log_ok "SELinux: Enforcing"
    fi

    # nofile limits
    if [[ ! -f /etc/security/limits.d/99-api-gateway.conf ]]; then
        log_info "configurando nofile limits"
        run "sudo bash -c 'cat > /etc/security/limits.d/99-api-gateway.conf <<EOF
*  soft  nofile  65536
*  hard  nofile  65536
EOF'"
    else
        log_ok "nofile limits já configurados"
    fi

    log_ok "harden_host"
}

# §3 — Docker CE via repo CentOS (oficial).
install_docker() {
    log_phase "install_docker"
    require_root "install_docker"

    if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
        local docker_ver
        docker_ver=$(docker --version 2>/dev/null | head -1)
        log_ok "Docker já instalado: $docker_ver"
        return 0
    fi

    log_info "adicionando repo Docker CE"
    run "sudo dnf -y install dnf-plugins-core"
    if [[ ! -f /etc/yum.repos.d/docker-ce.repo ]]; then
        run "sudo dnf config-manager --add-repo https://download.docker.com/linux/centos/docker-ce.repo"
    fi

    # Remove conflict com Podman se houver
    if rpm -q podman >/dev/null 2>&1; then
        log_warn "Podman detectado — pode conflitar com Docker. Pulando remove automático."
    fi

    log_info "instalando docker-ce + plugins"
    run "sudo dnf -y install docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin"

    log_info "enable + start docker daemon"
    run "sudo systemctl enable --now docker"

    # /etc/docker/daemon.json com log-driver + live-restore
    if [[ ! -f /etc/docker/daemon.json ]]; then
        log_info "criando /etc/docker/daemon.json (log rotation + live-restore)"
        run "sudo mkdir -p /etc/docker"
        run "sudo bash -c 'cat > /etc/docker/daemon.json <<EOF
{
  \"log-driver\": \"json-file\",
  \"log-opts\": { \"max-size\": \"100m\", \"max-file\": \"5\" },
  \"live-restore\": true,
  \"default-ulimits\": { \"nofile\": { \"Name\": \"nofile\", \"Hard\": 65536, \"Soft\": 65536 } }
}
EOF'"
        run "sudo systemctl restart docker"
    else
        log_ok "/etc/docker/daemon.json já existe"
    fi

    if run "sudo docker run --rm hello-world >/dev/null 2>&1"; then
        log_ok "Docker funcional (hello-world OK)"
    else
        log_err "Docker não consegue rodar hello-world — verifique daemon"
        return 1
    fi
}

# §5 — Usuário operacional + sudoers.
create_ops_user() {
    log_phase "create_ops_user"
    require_root "create_ops_user"

    if id "$OPS_USER" >/dev/null 2>&1; then
        log_ok "usuário $OPS_USER já existe"
    else
        log_info "criando usuário $OPS_USER"
        run "sudo useradd -m -s /bin/bash -c 'API Gateway operator' '$OPS_USER'"
    fi

    if id -nG "$OPS_USER" | grep -qw docker; then
        log_ok "$OPS_USER já está no grupo docker"
    else
        log_info "adicionando $OPS_USER ao grupo docker"
        run "sudo usermod -aG docker '$OPS_USER'"
    fi

    if [[ ! -f /etc/sudoers.d/${OPS_USER} ]]; then
        log_info "criando sudoers.d/${OPS_USER} pra systemctl docker"
        run "sudo bash -c 'cat > /etc/sudoers.d/${OPS_USER} <<EOF
${OPS_USER} ALL=(ALL) NOPASSWD: /usr/bin/systemctl restart docker
${OPS_USER} ALL=(ALL) NOPASSWD: /usr/bin/systemctl status docker
EOF'"
        run "sudo chmod 440 /etc/sudoers.d/${OPS_USER}"
    else
        log_ok "sudoers.d/${OPS_USER} já configurado"
    fi
}

# §6 — Diretórios + SELinux relabel.
provision_dirs() {
    log_phase "provision_dirs"
    require_root "provision_dirs"

    log_info "criando ${CONFIG_DIR}, ${CERTS_DIR}, ${LOG_DIR}"
    run "sudo mkdir -p '${CERTS_DIR}' '${LOG_DIR}/nginx' '${LOG_DIR}/gateway'"

    run "sudo chown -R root:${OPS_USER} '${CONFIG_DIR}'"
    run "sudo chmod 750 '${CONFIG_DIR}' '${CERTS_DIR}'"
    run "sudo chown -R '${OPS_USER}:${OPS_USER}' '${LOG_DIR}'"

    # SELinux relabel pra bind mounts
    log_info "SELinux fcontext + restorecon"
    run "sudo semanage fcontext -a -t container_file_t '${CONFIG_DIR}(/.*)?' 2>/dev/null || true"
    run "sudo semanage fcontext -a -t container_file_t '${LOG_DIR}(/.*)?' 2>/dev/null || true"
    run "sudo restorecon -Rv '${CONFIG_DIR}' '${LOG_DIR}' >/dev/null"

    log_ok "provision_dirs"
}

# §6.3 — Validar certs TLS (ou gera CSR pra CA corp).
check_certs() {
    log_phase "check_certs"

    local need_csr=0
    for f in tls.crt tls.key ca.crt; do
        if [[ ! -s "${CERTS_DIR}/$f" ]]; then
            log_warn "${CERTS_DIR}/$f ausente"
            need_csr=1
        fi
    done

    if (( need_csr )); then
        log_warn "Certificados TLS faltando. Gerando CSR para CA corp..."
        local csr_path="${CERTS_DIR}/api-gateway.csr"
        if [[ ! -s "$csr_path" ]]; then
            log_info "criando CSR em $csr_path"
            run "sudo openssl req -new -newkey rsa:4096 -nodes \
                -keyout '${CERTS_DIR}/tls.key' \
                -out '$csr_path' \
                -subj '/CN=api-gateway.tpb.corp/O=Teleperformance Brasil/C=BR' \
                -addext 'subjectAltName=DNS:api-gateway.tpb.corp'"
            run "sudo chmod 600 '${CERTS_DIR}/tls.key'"
        fi
        log_err "ACTION REQUIRED: envie $csr_path pra time de PKI corp, copie tls.crt + ca.crt pra ${CERTS_DIR}/ e re-rode --only check_certs"
        return 1
    fi

    # Cert válido?
    local enddate
    enddate=$(openssl x509 -in "${CERTS_DIR}/tls.crt" -noout -enddate 2>/dev/null | cut -d= -f2 || echo "")
    if [[ -z "$enddate" ]]; then
        log_err "tls.crt inválido ou ilegível"
        return 1
    fi
    log_ok "cert TLS válido até: $enddate"

    # Permissões
    run "sudo chmod 644 '${CERTS_DIR}/tls.crt' '${CERTS_DIR}/ca.crt'"
    run "sudo chmod 600 '${CERTS_DIR}/tls.key'"
    run "sudo restorecon -Rv '${CERTS_DIR}' >/dev/null"
    log_ok "check_certs"
}

# §7 — Clone ou git fetch do repo (idempotente).
clone_or_update() {
    log_phase "clone_or_update"
    require_root "clone_or_update"

    if [[ ! -d "${REPO_DIR}/.git" ]]; then
        log_info "clonando repo em ${REPO_DIR}"
        run "sudo mkdir -p '$(dirname ${REPO_DIR})'"
        run "sudo chown ${OPS_USER}:${OPS_USER} '$(dirname ${REPO_DIR})'"
        # NOTA: requer SSH key ou HTTPS PAT configurado pro user que roda.
        # Se git remote for SSH-only e não houver chave, falhará — operador resolve.
        run "sudo -u '${OPS_USER}' git clone git@github.com:D4nRossi/ai-gateway.git '${REPO_DIR}'"
    else
        log_ok "repo já presente em ${REPO_DIR}, fazendo fetch"
        run "sudo -u '${OPS_USER}' git -C '${REPO_DIR}' fetch --tags --prune"
    fi

    if [[ -n "$RELEASE_REF" ]]; then
        log_info "checkout ${RELEASE_REF}"
        run "sudo -u '${OPS_USER}' git -C '${REPO_DIR}' checkout '${RELEASE_REF}'"
    else
        log_info "mantendo branch atual (use --release pra fixar tag)"
    fi

    # Validar layout monorepo
    for d in apps/gateway apps/admin-api apps/console infra/docker infra/nginx; do
        if [[ ! -d "${REPO_DIR}/$d" ]]; then
            log_err "layout esperado faltando: ${REPO_DIR}/$d (rev atual está pré-monorepo?)"
            return 1
        fi
    done
    log_ok "clone_or_update — layout monorepo OK"
}

# §8 — Validar acesso Azure (KV listável).
azure_check() {
    log_phase "azure_check"

    if ! command -v az >/dev/null 2>&1; then
        log_info "instalando azure-cli"
        require_root "azure_check"
        run "sudo rpm --import https://packages.microsoft.com/keys/microsoft.asc"
        run "sudo dnf install -y https://packages.microsoft.com/config/rhel/9/packages-microsoft-prod.rpm 2>/dev/null || true"
        run "sudo dnf install -y azure-cli"
    fi

    # Tenta listar secrets do vault — se SP env vars setadas, usa
    if [[ -n "${AZURE_CLIENT_ID:-}" && -n "${AZURE_CLIENT_SECRET:-}" && -n "${AZURE_TENANT_ID:-}" ]]; then
        log_info "az login via Service Principal env vars"
        run "az login --service-principal -u \"\$AZURE_CLIENT_ID\" -p \"\$AZURE_CLIENT_SECRET\" --tenant \"\$AZURE_TENANT_ID\" >/dev/null"
    fi

    # Tenta listar (não falha o script se vault não acessível — só warn)
    local vault_name="${KV_HOST%%.*}"
    if az keyvault secret list --vault-name "$vault_name" --query "[].name" -o tsv >/dev/null 2>&1; then
        log_ok "KV $vault_name acessível"
    else
        log_warn "KV $vault_name não acessível com credenciais atuais — owner valida manualmente"
    fi
}

# §9 — Validar outbound critical.
validate_outbound() {
    log_phase "validate_outbound"

    if nc -vz "$SQL_HOST" "$SQL_PORT" 2>&1 | grep -q "succeeded"; then
        log_ok "SQL Server $SQL_HOST:$SQL_PORT alcançável"
    else
        log_err "SQL Server $SQL_HOST:$SQL_PORT inalcançável — verifique VPN + firewall corp"
        return 1
    fi

    if curl -fsSL -m 5 -I "https://${KV_HOST}" >/dev/null 2>&1; then
        log_ok "Key Vault $KV_HOST alcançável (HTTPS)"
    else
        log_warn "$KV_HOST não respondeu — pode ser bloqueio corp"
    fi

    if curl -fsSL -m 5 -I "https://login.microsoftonline.com" >/dev/null 2>&1; then
        log_ok "login.microsoftonline.com alcançável"
    else
        log_warn "login.microsoftonline.com não respondeu"
    fi
}

# §10 — Valida que .env existe e tem as keys obrigatórias.
check_env() {
    log_phase "check_env"

    if [[ ! -s "$ENV_FILE" ]]; then
        log_err ".env não encontrado em $ENV_FILE — copie de infra/docker/.env.example e preencha"
        log_info "Sugestão: sudo cp ${REPO_DIR}/infra/docker/.env.example $ENV_FILE && sudo chmod 640 $ENV_FILE && sudo chown root:${OPS_USER} $ENV_FILE && sudo vim $ENV_FILE"
        return 1
    fi

    # Permissões
    local perms
    perms=$(stat -c '%a' "$ENV_FILE")
    if [[ "$perms" != "640" && "$perms" != "600" ]]; then
        log_warn ".env com perms $perms — recomendado 640 (root:${OPS_USER})"
    fi

    # Keys obrigatórias (mínimo viável)
    local required=(
        KEYVAULT_URI
        AZURE_OPENAI_ENDPOINT
        SQL_CONNECTION_STRING
        DATABASE_ENCRYPTION_KEY_HEX
        CERTS_DIR
    )
    local missing=()
    local key
    for key in "${required[@]}"; do
        # Aceita "key=value" não-vazio (ignora linhas com placeholder __FROM_KV__ ou vazio)
        if ! grep -qE "^${key}=[^[:space:]]+$" "$ENV_FILE"; then
            missing+=("$key")
        elif grep -qE "^${key}=__FROM_KV.*__$" "$ENV_FILE"; then
            missing+=("$key (placeholder não preenchido)")
        fi
    done

    if (( ${#missing[@]} )); then
        log_err ".env faltando ou com placeholder: ${missing[*]}"
        return 1
    fi
    log_ok "check_env: ${#required[@]} keys obrigatórias presentes"
}

# §11 — Build + up.
build_and_start() {
    log_phase "build_and_start"

    log_info "compose build (primeira vez demora ~5-10min)"
    run "cd '${REPO_DIR}/infra/docker' && docker compose --env-file '$ENV_FILE' build --pull"

    log_info "compose up -d"
    run "cd '${REPO_DIR}/infra/docker' && docker compose --env-file '$ENV_FILE' up -d"

    log_ok "build_and_start — containers iniciados"
}

# Polling até todos containers ficarem (healthy).
wait_healthy() {
    log_phase "wait_healthy"

    (( DRY_RUN )) && { log_info "dry-run, pulando wait"; return 0; }

    local elapsed=0 interval=5
    local services=(nginx gateway console admin-api)
    while (( elapsed < HEALTHY_TIMEOUT_SECONDS )); do
        local all_healthy=1
        local status
        for svc in "${services[@]}"; do
            status=$(cd "${REPO_DIR}/infra/docker" && \
                docker compose --env-file "$ENV_FILE" ps --format "json" "$svc" 2>/dev/null | \
                jq -r '.Health // empty' 2>/dev/null | head -1)
            if [[ "$status" != "healthy" ]]; then
                all_healthy=0
            fi
        done
        if (( all_healthy )); then
            log_ok "todos os 4 serviços (healthy)"
            return 0
        fi
        printf "."
        sleep "$interval"
        elapsed=$((elapsed + interval))
    done
    printf "\n"
    log_err "timeout esperando todos os serviços healthy (${HEALTHY_TIMEOUT_SECONDS}s)"
    cd "${REPO_DIR}/infra/docker" && docker compose ps
    return 1
}

# Aplica DbUp migrations via admin-api CLI (opcional — gateway Go já aplica também).
run_migrations_dotnet() {
    log_phase "run_migrations_dotnet"

    log_info "rodando admin-api migrate up (idempotente; .NET tem bookkeeping separado)"
    run "cd '${REPO_DIR}/infra/docker' && docker compose --env-file '$ENV_FILE' run --rm admin-api migrate up"

    log_ok "run_migrations_dotnet — .NET bookkeeping sincronizado"
}

# Smoke test end-to-end.
smoke_test() {
    log_phase "smoke_test"

    local base="https://api-gateway.tpb.corp"
    log_info "testando ${base}/__nginx_healthz"
    if curl -ksSf -m 10 "${base}/__nginx_healthz" >/dev/null 2>&1; then
        log_ok "nginx healthz: ok"
    else
        log_warn "nginx healthz falhou — talvez DNS interno não aponta pra esta VM ainda"
    fi

    log_info "testando ${base}/healthz (gateway Go)"
    if curl -ksSf -m 10 "${base}/healthz" >/dev/null 2>&1; then
        log_ok "gateway healthz: ok"
    else
        log_warn "gateway healthz falhou — investigar docker compose logs gateway"
    fi

    log_info "testando ${base}/readyz (gateway Go — testa SQL)"
    if curl -ksSf -m 10 "${base}/readyz" >/dev/null 2>&1; then
        log_ok "gateway readyz: ok (SQL alcançável)"
    else
        log_err "gateway readyz falhou — SQL Server inalcançável do container; investigue"
        return 1
    fi

    log_info "testando login admin via admin-api .NET"
    local resp
    resp=$(curl -ksS -m 10 -X POST "${base}/admin/v1/auth/login" \
        -H "Content-Type: application/json" \
        -d '{"username":"root","password":"Adm!nGogateway2026"}' 2>&1 || true)
    if echo "$resp" | jq -e '.token' >/dev/null 2>&1; then
        log_ok "login admin retornou token — admin-api .NET funcionando"
        log_warn "TROQUE A SENHA DO ROOT IMEDIATAMENTE NO UI"
    else
        log_err "login admin falhou — resposta: $resp"
        return 1
    fi

    log_ok "smoke_test completo"
}

# ╔═══════════════════════════════════════════════════════════════════════════╗
# ║                                MAIN                                        ║
# ╚═══════════════════════════════════════════════════════════════════════════╝

main() {
    parse_args "$@"

    printf "\n%b%s v%s%b\n" "${C_BOLD}" "$SCRIPT_NAME" "$SCRIPT_VERSION" "${C_RESET}"
    printf "dry-run=%s assume-yes=%s repo=%s env=%s\n\n" "$DRY_RUN" "$ASSUME_YES" "$REPO_DIR" "$ENV_FILE"

    local phase failed=0
    for phase in "${PHASES[@]}"; do
        if ! phase_enabled "$phase"; then
            log_info "skip $phase"
            continue
        fi
        if ! "$phase"; then
            log_err "phase $phase falhou"
            failed=1
            break
        fi
    done

    if (( failed )); then
        printf "\n%bDEPLOY FALHOU%b — corrija e re-rode (idempotente; pode usar --only pra retomar)\n" "${C_RED}" "${C_RESET}"
        exit 1
    fi
    printf "\n%bDEPLOY OK%b\n" "${C_GREEN}" "${C_RESET}"
}

main "$@"
