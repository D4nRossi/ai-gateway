# Handoff — retomada da sessão

> **Quando ler:** ao abrir o projeto. Esse documento é "como continuar
> sem perder contexto". Siga em ordem das seções.
>
> **Quando editar:** ao final de cada sessão de trabalho, sobrescreva com o
> novo estado. Não acumular versões — esse arquivo é de **uso atual**, não
> histórico (pra histórico use `roadmap.md` §6 e o `git log`).

---

## 0. Cross-machine — Win desktop ↔ Ubuntu notebook

Owner trabalha em duas máquinas. Os canais de persistência entre elas:

| Canal | O que carrega | Confiabilidade |
|---|---|---|
| **Repo git (este)** | Código + `docs/*` + ADRs + migrations + CLAUDE.md + SPEC.md | Alta — single source of truth |
| **Vault Obsidian (`KB/AI-Gateway/`)** | 55 notas-resumo cross-referenciando este repo. Sincronizadas via **Obsidian Sync oficial** | Alta — aparece automático nas duas máquinas |
| **Memória do Claude Code** | Cache de preferências do owner (user/feedback/project/reference) | **Local por máquina** — não sincroniza. Reconstruída a partir do `docs/handoff.md` no início de cada sessão |

**Regra de ouro:** se uma informação não pode ser perdida ao trocar de
máquina, ela precisa estar no repo (preferencialmente em `docs/handoff.md`
ou num ADR) ou no vault Obsidian. **Não confiar** na memória do Claude Code
para persistência entre máquinas.

## 1. Estado em que paramos

**Data:** fim do dia 2026-06-02 (sessão Ubuntu → trocando pra Windows).
Status: **admin .NET 100% implementada, infra deploy Linux+Docker pronta,
script de automação gerado. Deploy real ainda não executado** — owner vai
simular em VM Oracle Linux dev usando Claude Code lá.

> **Deploy Windows está SUSPENSO** desde 2026-06-02 (ADR-0030 pivot pra
> Linux+Docker). Material legado preservado em
> `infra/legacy-windows/` + `docs/deploy/windows.md` (referência histórica).

### O que está pronto pra deploy

| Componente | Status |
|---|---|
| Monorepo `apps/{gateway,admin-api,console}` + `infra/{docker,nginx,scripts}` | ✅ |
| Gateway Go (data plane + admin Go ainda no ar transicional) | ✅ |
| admin-api .NET 10 — 27/27 endpoints com paridade Go | ✅ |
| AES-256-GCM cipher (ADR-0012) paridade bit-a-bit | ✅ + UnitTests |
| Tests scaffold (xUnit + Testcontainers Auth piloto) | ✅ scaffold |
| Console standalone (sem `go:embed`) | ✅ |
| nginx terminator + routing 3-vias | ✅ |
| `/admin/v1/auth/{login,logout}` rolado pra .NET no nginx | ✅ |
| Demais `/admin/v1/*` continuam Go até Fase 5 | ⏳ |
| Compose multi-app com healthchecks + depends_on | ✅ |
| Guia `docs/deploy/oracle-linux.md` (~700 linhas) | ✅ |
| Script `infra/scripts/deploy-oracle-linux.sh` idempotente 14 fases | ✅ |
| Pre-deploy review (8 P1/P2 fixados + 3 micro-fixes) | ✅ |
| Vault Obsidian sincronizado (2 sessões 2026-06-02 + memória) | ✅ |

### O que está pendente

| Item | Bloqueador | Resolução |
|---|---|---|
| **Deploy real em VM Oracle Linux** | owner não rodou ainda | simular em VM dev → ajustar script → rodar em homolog/prod |
| Validação runtime do build .NET | NuGet timeout local (offline) | rodar `dotnet restore` em máquina com internet (Rider/VS no Windows) |
| Tests E2E pras 8 slices que não são Auth | padrão estabelecido em `AuthTests.cs` | replicar quando precisar |
| Rolar próximas slices no nginx routing | precisa primeiro validar Auth em homolog | um PR por slice quando deploy estabilizar |
| Fase 5 (desligar admin Go) | precisa todas 9 slices no .NET routing | meta longa |

### Tech debt pré-existente aceito (NÃO corrigido)

- README.md root + CLAUDE.md mencionam Postgres em ~6 lugares (pré-ADR-0022). Owner aceita
- `contracts/admin-api.openapi.yaml` não gerado (ADR-0030 prometeu) — owner gera via Rider depois
- Session expired cleanup no admin-api .NET (background service) — postergado
- ADRs 0011/0012 etc. não revisitadas após mudanças relacionadas

### Decisões consolidadas pra deploy Linux+Docker (alvo atual ADR-0030)

| Item | Valor |
|---|---|
| Alvo OS | Oracle Linux 9 (RHEL 9 compatible) |
| Container runtime | Docker CE 27+ via repo CentOS |
| Reverse proxy/TLS | nginx 1.27-alpine (terminator + routing 3-vias) |
| Path raiz no servidor | `/opt/api-gateway` |
| Config path | `/etc/api-gateway/` (`.env`, `certs/`) |
| Log path | `/var/log/api-gateway/{nginx,gateway}` |
| User operacional | `gateway-ops` (membro grupo docker) |
| SELinux | enforcing; `container_file_t` em bind mounts |
| SQL Server | `BRSPVPDEV003.tpb.corp:1433` (corp, externo) |
| Database | `AzureAI_Gateway_hom` |
| SQL user | `usr_sist_AzureAI_Gateway_hom` (senha via KV) |
| Azure KV | `danieldev` (`AzureAIGateway-DB-Password-hom`, `DB-ENCRYPTION-KEY`, `AZURE-OPENAI-API-KEY`) |
| `DB_ENCRYPTION_KEY_HEX` (gerado) | `4b3d200ab31e8ede05af67a70632db4e02c01630eac9d1d2e5f51935117bbea1` |

⚠️ **A chave AES é viva.** Mesmo valor que o admin-api .NET lê de
`Database__EncryptionKeyHex`. Divergência → credentials cifradas por um lado
não decifram pelo outro. Tratar como dado sensível conforme classificação corp.

### O que foi entregue nesta megasessão 2026-06-02 (pushado em commits ordenados)

**Parte 1 (manhã)** — reorg + ADRs + esqueleto:

- ADRs 0027-0030 escritas (`docs/adrs/`)
- Reorg pra monorepo `apps/{gateway,admin-api,console}` + `infra/`
- Console standalone (`go:embed` removido — ADR-0028)
- `internal/` consolidado por domínio Variante C (`governance/`, `chat/`, `proxy/`)
- Esqueleto admin-api .NET 10 + Auth piloto
- Compose multi-app + nginx config inicial
- Guia `docs/deploy/oracle-linux.md` (~700 linhas)
- Deploy Windows formalmente suspenso

**Parte 2 (tarde)** — completar + ativar:

- Slices Users, Applications CRUD, RotateKey+Grants, Endpoints CRUD,
  Targets+MigrateToKV, Observability, Dashboard — **27/27 endpoints**
- AES-GCM cipher paridade ADR-0012 (`Infrastructure/Crypto/AesGcmCipher.cs`)
- `TargetAuthPlaintext` + custom converter (paridade Go json.Marshal)
- `IKeyVaultSecretWriter` + impl condicional (`DefaultAzureCredential`)
- Tests scaffold: `tests/APIGateway.Admin.UnitTests/` (FluentAssertions)
  + `tests/APIGateway.Admin.IntegrationTests/` (Testcontainers + 6 tests E2E Auth)
- Solution `APIGateway.Admin.sln` com 3 projetos
- **Fase 4d**: admin-api .NET **ativada no compose**; nginx rola
  `/admin/v1/auth/{login,logout}` pra `admin_api_upstream`
- Pre-deploy review: 5 problemas P1/P2 fixados (wget Dockerfile,
  compose headers stale, comentários migrations errados, README admin-api)
- 3 micro-fixes: connection string retry, escape de senha, nota explicativa
  `migrate up` primeira execução
- **Script `infra/scripts/deploy-oracle-linux.sh`** idempotente 14 fases
  (cobre 1:1 o guia oracle-linux.md; flags --dry-run/--yes/--only/--skip/--release)

**Vault Obsidian sincronizado**:
- `KB/AI-Gateway/Deploy/Sessao-Reorg-2026-06-02.md` (parte 1)
- `KB/AI-Gateway/Deploy/Sessao-AdminNET-2026-06-02.md` (parte 2 + micro-fixes)
- `KB/AI-Gateway/Melhorias-Arquitetura.md` (análise Richards/Ford que motivou tudo)
- `KB/AI-Gateway/00-Index.md` + `Deploy/_MOC.md` atualizados

### Como retomar (próxima sessão — provavelmente Windows)

Owner abre o Claude Code e diz **"vamos simular o deploy do API Gateway
em VM Oracle Linux dev"**. Sequência esperada:

**Passo 0 — Sync repo + Obsidian na nova máquina**

```powershell
# Pull repo
cd <path-onde-clonou>\ai-gateway
git fetch
git pull v2

# Validar último commit
git log --oneline -5
# Esperado topo: 1740984 chore(deploy): 3 micro-fixes pre-deploy
# (ou commit mais recente se o script já foi committed)
```

Obsidian Sync (oficial) já trouxe as 2 novas notas em
`KB/AI-Gateway/Deploy/Sessao-{Reorg,AdminNET}-2026-06-02.md`.

**Passo 1 — Setup Windows do dev local** (se for desenvolver/buildar nessa máquina)

Ver §2.B abaixo (novo).

**Passo 2 — Simulação do deploy em VM Oracle Linux**

Se a VM tem acesso outbound + VPN corp:

```bash
# 1. SCP do script + certs + .env pra VM
scp infra/scripts/deploy-oracle-linux.sh opadmin@vm-dev:/tmp/
# Resolver secrets do KV manualmente em algum momento:
#   az keyvault secret show --vault-name danieldev --name AzureAIGateway-DB-Password-hom -o tsv
#   az keyvault secret show --vault-name danieldev --name DB-ENCRYPTION-KEY -o tsv
# Popular .env localmente, scp pra /etc/api-gateway/.env

# 2. Na VM, instalar Claude Code + clonar repo
curl -fsSL https://claude.ai/install.sh | bash
sudo git clone git@github.com:D4nRossi/ai-gateway.git /opt/api-gateway-bootstrap

# 3. Rodar Claude Code na VM com a "primeira instrução" da seção §3
cd /opt/api-gateway-bootstrap
claude
```

**Passo 3 — Iteração no script + push de ajustes**

Quando Claude Code na VM encontrar surpresas (mirror dnf, SELinux, etc.),
o trace fica em `/tmp/deploy-trace-YYYYMMDD.md`. Depois de tudo verde,
ele atualiza `infra/scripts/deploy-oracle-linux.sh` com os patches reais
e abre PR.

### Frentes pendentes (sem ETA específica)

**Deploy + validação**:
- Simular deploy em VM Oracle Linux dev (Claude Code lá)
- Refinar `infra/scripts/deploy-oracle-linux.sh` com surpresas descobertas
- Deploy real em homolog corp
- Validar end-to-end: login admin pela `/admin/v1/auth/login` → audit row com `application_name="admin-api"` no DB

**Runtime .NET**:
- `dotnet restore` + build offline na máquina com internet (Rider/VS)
- Rodar tests UnitTests (FluentAssertions, sem container)
- Rodar tests IntegrationTests (precisa Docker + Testcontainers)
- Gerar `contracts/admin-api.openapi.yaml` via Swashbuckle (ADR-0030 prometeu)

**Rolagem de slices**:
- Rolar Users no nginx routing (slice mais simples, role admin)
- Rolar Applications CRUD
- ... uma por vez até todas as 9
- Fase 5: desligar admin Go

**Tech debt aceito**:
- Atualizar README.md root + CLAUDE.md removendo refs Postgres (~6 lugares)
- Session expired cleanup .NET background service
- Tests E2E pras 8 slices não-Auth

**Frentes mais antigas (do roadmap pré-2026-06-02)**:
- SSO Entra ID / OIDC (P1 Segurança)
- Anthropic/Gemini/Cohere adapters pra usage extractor
- Percentis p50/p95/p99 no dashboard
- Rotação de chaves vazadas POC AgentFlow
- Azure Language como CRUD no DB

---

## 2. Setup máquina nova

Dois perfis cobertos: **2.A Ubuntu notebook** (manutenção do mesmo fluxo
antigo) e **2.B Windows desktop** (nova máquina pós-2026-06-02 com .NET
no jogo).

### 2.A Ubuntu notebook

Pré-requisitos do ambiente, na ordem em que precisam estar prontos:

### 2.1 Toolchain

```bash
# Go 1.25+ (tarball oficial; evitar apt que vem desatualizado)
GOVER=1.25.0
wget -q https://go.dev/dl/go${GOVER}.linux-amd64.tar.gz
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf go${GOVER}.linux-amd64.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin:$HOME/go/bin' >> ~/.bashrc
source ~/.bashrc
go version  # esperado: go version go1.25.0 linux/amd64

# Node 20+ (NodeSource)
curl -fsSL https://deb.nodesource.com/setup_20.x | sudo -E bash -
sudo apt install -y nodejs
node -v && npm -v

# Git, build essentials, CGO deps (pro spike Voice Live)
sudo apt install -y git build-essential libasound2-dev
# libasound2-dev é dependência do malgo (captura/playback áudio do spike Voice Live)

# Azure CLI (repo MS oficial)
curl -sL https://aka.ms/InstallAzureCLIDeb | sudo bash
az version
```

### 2.2 Acesso corporativo

| Item | Como obter / validar |
|---|---|
| VPN corporativa Teleperformance (Linux) | Cliente fornecido pela TI corporativa (provavelmente OpenConnect/Cisco AnyConnect Linux). Sem isso, `BRSPVPDEV003:1433` é inalcançável |
| `az login` no tenant `c050c98c-b463-4591-ac3b-deb782c0ba6e` | `az login --tenant c050c98c-b463-4591-ac3b-deb782c0ba6e` (abre browser pra MFA) |
| Acesso ao KV `danieldev` | `az keyvault secret list --vault-name danieldev --query "[].name" -o tsv` deve listar 5 secrets |
| Acesso ao SQL Server `BRSPVPDEV003:1433` | `nc -vz BRSPVPDEV003 1433` (precisa VPN) |

### 2.3 IDEs JetBrains

- **GoLand** — instalar via JetBrains Toolbox (recomendado, gerencia updates)
- **WebStorm** — idem. Pode rodar lado a lado com GoLand
- Toolbox: https://www.jetbrains.com/toolbox-app/ (tem .tar.gz direto)
- Run Configurations do GoLand foram criadas no Windows; ao abrir o projeto no Ubuntu, GoLand re-detecta a estrutura mas pode ser preciso re-apontar paths absolutos (working directory). Detalhes em `docs/local-development.md §3`

### 2.4 Cliente SQL (substituto do SSMS que não existe no Linux)

| Opção | Notas |
|---|---|
| **GoLand Database tool** (recomendado) | Já vem embutido. Add datasource → Microsoft SQL Server. Conexão idêntica à do Windows (`docs/local-development.md §9`) |
| **Azure Data Studio** | Cross-platform Microsoft. `sudo snap install azuredatastudio` ou via .deb da Microsoft |
| **DBeaver Community** | Universal. `sudo snap install dbeaver-ce` |
| `sqlcmd` CLI | `curl https://packages.microsoft.com/keys/microsoft.asc \| sudo apt-key add - && curl https://packages.microsoft.com/config/ubuntu/$(lsb_release -rs)/prod.list \| sudo tee /etc/apt/sources.list.d/mssql-release.list && sudo apt update && sudo ACCEPT_EULA=Y apt install -y mssql-tools18 unixodbc-dev && echo 'export PATH=$PATH:/opt/mssql-tools18/bin' >> ~/.bashrc` |

### 2.5 Clonar e validar o repo

```bash
mkdir -p ~/projects && cd ~/projects
git clone <url-do-repo-ai-gateway> ai-gateway
cd ai-gateway

# Verifica que está na branch v2 e sincronizado com remote
git fetch
git checkout v2
git pull
git log --oneline -5

# Resolver deps + validar
go mod download
go vet ./...
go build ./...
go test -count=1 -race ./...   # 15 pacotes verdes esperados

# Frontend
cd web && npm install && cd ..
```

### 2.6 .env e KV

`.env` está em `.gitignore` — **precisa recriar** no Ubuntu:

```bash
cp .env.example .env
nano .env   # preencher KEYVAULT_URI e endpoints Azure conforme exemplos abaixo
```

Conteúdo mínimo do `.env` (segredos vêm do KV via `${kv:...}` no `configs/gateway.yaml`):

```env
KEYVAULT_URI=https://danieldev.vault.azure.net/
AZURE_OPENAI_ENDPOINT=https://danie-mc4ryviy-westeurope.cognitiveservices.azure.com
AZURE_LANGUAGE_ENDPOINT=https://tp-language-pii.cognitiveservices.azure.com
LOG_LEVEL=info
```

Carregar no shell:

```bash
set -a && source .env && set +a
```

### 2.7 Boot e validação rápida

```bash
go run ./cmd/gateway
```

Esperar a sequência de logs documentada em `docs/local-development.md §5`.

Acessar `http://localhost:8080/ui` → login com `root` / `Adm!nGogateway2026` (migration 010) → se já trocou a senha na sessão anterior, usar a nova credencial pessoal.

> Note: depois do desembed (ADR-0028), o gateway Go **não serve mais o UI**.
> Pra dev local da UI, rodar `npm run dev` em `apps/console/` (porta 5173)
> em paralelo ao gateway. Detalhes em `docs/local-development.md §3.1`.

---

### 2.B Windows desktop

Setup pra desenvolver Go + .NET 10 + React no Windows. Owner já tem
`amorim.286-adm1` na máquina corporativa.

#### 2.B.1 Toolchain

| Item | Como instalar | Validação |
|---|---|---|
| Go 1.25+ | https://go.dev/dl/ MSI (`go1.25.x.windows-amd64.msi`) | `go version` |
| .NET SDK 10.0 | https://dotnet.microsoft.com/download/dotnet/10.0 (SDK x64) | `dotnet --list-sdks` mostra `10.0.x` |
| Node 20+ | https://nodejs.org/ MSI LTS | `node -v && npm -v` |
| Git | https://git-scm.com/download/win | `git --version` |
| Docker Desktop | https://docs.docker.com/desktop/install/windows-install/ | `docker version && docker compose version` |
| Azure CLI | `winget install Microsoft.AzureCLI` ou MSI | `az version` |
| WSL2 (recomendado) | `wsl --install` (PowerShell admin) — Docker Desktop pede | `wsl --status` |

Reboot depois do Docker Desktop pra Hyper-V/WSL2 entrar.

#### 2.B.2 Claude Code no Windows

Opção A — **dentro do WSL2** (recomendado pra paridade com Ubuntu):

```bash
# Abrir terminal Ubuntu WSL
curl -fsSL https://claude.ai/install.sh | bash
mkdir -p ~/projects
cd ~/projects
git clone git@github.com:D4nRossi/ai-gateway.git
cd ai-gateway
claude
```

Opção B — **direto no Windows** (PowerShell):

```powershell
# Conforme docs.claude.com/en/docs/claude-code/setup
# (Windows native, sem WSL — pode ter incompatibilidade de paths em
#  alguns scripts bash)
```

⚠️ **Memory do Claude Code é local por máquina** — quando subir aqui no
Windows, o histórico de preferências/memory começa zerado. O **handoff.md
(este arquivo) + vault Obsidian** carregam todo o contexto. Claude Code
vai ler o handoff automaticamente.

#### 2.B.3 IDEs

| Stack | IDE recomendada | Setup |
|---|---|---|
| Go (gateway) | **GoLand** via JetBrains Toolbox | `Open` → `apps/gateway/`; usa `go.mod` ali |
| .NET 10 (admin-api) | **Rider** ou **Visual Studio 2022 17.13+** | Open `apps/admin-api/APIGateway.Admin.sln` |
| React (console) | **WebStorm** ou VS Code | Open `apps/console/`; `npm install` |

Rider pode rodar IntegrationTests via Testcontainers se Docker Desktop ativo.

#### 2.B.4 Clonar + validar

```powershell
mkdir C:\dev
cd C:\dev
git clone git@github.com:D4nRossi/ai-gateway.git
cd ai-gateway

# Branch e estado
git checkout v2
git pull

# Build Go
cd apps\gateway
go mod download
go vet ./...
go build ./...
cd ..\..

# Restore + build .NET (precisa internet pra NuGet)
cd apps\admin-api
dotnet restore
dotnet build APIGateway.Admin.sln
cd ..\..

# Tests UnitTests (rápidos, sem container)
dotnet test apps\admin-api\tests\APIGateway.Admin.UnitTests\
```

Se `dotnet restore` der NU1301 timeout, configurar proxy corp em
`%APPDATA%\NuGet\NuGet.Config` ou rodar com `--source nuget.org`.

#### 2.B.5 .env e secrets

Pra dev local Windows, NÃO há `.env` no repo (ignored). Pra rodar gateway+admin-api locais contra SQL Server corp:

```powershell
# 1. Login Azure no shell pra resolver KV
az login --tenant c050c98c-b463-4591-ac3b-deb782c0ba6e

# 2. Resolver secrets manualmente
$env:DATABASE_PASSWORD = $(az keyvault secret show --vault-name danieldev --name AzureAIGateway-DB-Password-hom --query value -o tsv)
$env:DB_ENCRYPTION_KEY = $(az keyvault secret show --vault-name danieldev --name DB-ENCRYPTION-KEY --query value -o tsv)
$env:AZURE_OPENAI_API_KEY = $(az keyvault secret show --vault-name danieldev --name AZURE-OPENAI-API-KEY --query value -o tsv)

# 3. Outros endpoints
$env:KEYVAULT_URI = "https://danieldev.vault.azure.net/"
$env:AZURE_OPENAI_ENDPOINT = "https://danie-mc4ryviy-westeurope.cognitiveservices.azure.com"

# 4. Rodar gateway Go
cd apps\gateway
go run .\cmd\gateway

# 5. Em outro terminal: rodar admin-api .NET
cd apps\admin-api
$env:ConnectionStrings__Gateway = "Server=BRSPVPDEV003.tpb.corp;Database=AzureAI_Gateway_hom;User Id=usr_sist_AzureAI_Gateway_hom;Password=$env:DATABASE_PASSWORD;Encrypt=true;TrustServerCertificate=false"
$env:Database__EncryptionKeyHex = $env:DB_ENCRYPTION_KEY
dotnet run --project src\APIGateway.Admin.Api -- --environment Development

# 6. Em outro terminal: console
cd apps\console
npm install
npm run dev
```

Browser: `http://localhost:5173/ui/`. Vite proxy roteia `/admin/v1/*` pra
admin-api .NET (porta default 5070) e `/v1/*` pro gateway Go (8080).
Pode precisar editar `vite.config.ts` se as portas locais diferirem.

#### 2.B.6 Validação final

```powershell
# Healthchecks
curl http://localhost:8080/healthz   # gateway Go
curl http://localhost:5070/healthz   # admin-api .NET

# Login (vai pelo admin-api .NET via Vite proxy)
curl -X POST http://localhost:5173/admin/v1/auth/login `
  -H "Content-Type: application/json" `
  -d '{\"username\":\"root\",\"password\":\"Adm!nGogateway2026\"}'
```

---

## 3. Sequência amanhã

Owner abre Claude Code (Windows ou WSL) e diz **"vamos simular o deploy
em VM Oracle Linux dev"**. Sequência esperada:

1. `git fetch && git pull v2` no clone Windows
2. Validar que tem o commit do script de deploy (`infra/scripts/deploy-oracle-linux.sh`)
3. SSH na VM Oracle Linux dev (ou montar nova)
4. Na VM: instalar Claude Code (`curl -fsSL https://claude.ai/install.sh | bash`)
5. Na VM: clonar repo em `/opt/api-gateway-bootstrap`
6. Na VM: resolver secrets KV manualmente, criar `/etc/api-gateway/.env`
7. Na VM: rodar Claude Code com primeira instrução (ver §3.1)
8. Iterar `deploy-oracle-linux.sh` até passar 100% sem ajuste manual
9. Quando ok: PR de update do script

### 3.1 Primeira instrução pra Claude Code na VM Oracle Linux

```
Vamos simular o deploy seguindo `infra/scripts/deploy-oracle-linux.sh`
em modo full simulation (VPN ativa, SQL corp, cert real).

Antes de executar, valide manualmente:
1. VPN ativa? `nc -vz BRSPVPDEV003.tpb.corp 1433` deve retornar succeeded
2. Service Principal criado e tem permissões Get/List no KV danieldev
3. /etc/api-gateway/.env existe com SQL_CONNECTION_STRING e
   DATABASE_ENCRYPTION_KEY_HEX preenchidos (sem placeholders)
4. tls.crt, tls.key, ca.crt em /etc/api-gateway/certs/

Crie /tmp/deploy-trace-$(date +%Y%m%d).md onde vai anotando: comando
proposto, comando executado, output truncado a 20 linhas, e quaisquer
ajustes necessários ao script. Vamos fase por fase com confirmação:

  sudo ./infra/scripts/deploy-oracle-linux.sh --only check_prereqs --dry-run

Depois executa real com --yes. Sigamos sequencial até smoke_test.
Reportar qualquer surpresa ANTES de aplicar.
```

### 3.2 Quando tudo passar

```bash
# Compare trace com script atual
diff -u /opt/api-gateway-bootstrap/infra/scripts/deploy-oracle-linux.sh <(...)

# Claude Code aplica patches que tiveram que ser feitos manualmente,
# git commit + push, e o script vira oficial pra prod
```

Toda etapa de implementação segue o **workflow obrigatório do CLAUDE.md §3** (anunciar plano → aguardar aprovação → consultar doc oficial → implementar → validar → reportar).

---

## 4. Decisões em aberto

Anotadas pra não esquecer; **não bloqueiam** retomada amanhã.

### 4.1 Vault Obsidian populado

55 notas em `KB/AI-Gateway/` cobrindo arquitetura, ADRs, ondas, stack, infra,
comandos, how-to-use. Resumo navegável com source fidelity ao repo. **Sincroniza
via Obsidian Sync oficial** — disponível automático no notebook.

Notas-âncora pra retomada:
- `KB/AI-Gateway/00-Index.md` — MOC global
- `KB/AI-Gateway/Decisoes-em-aberto.md` — backlog consolidado (espelha esta seção)
- `KB/AI-Gateway/Ondas/Onda-8-Streaming-Audio.md` — contexto da próxima frente
- `KB/AI-Gateway/ADRs/ADR-0023-Streaming-Audio.md` — resumo do ADR completo

### 4.2 Bug 2 — Acessos não persiste

Diagnóstico no commit `f4b5e6e` ficou inconclusivo no código. Instrumentação adicionada em `internal/app/adminservice/service.go` (logs `event_type=grant_created`/`grant_revoked`). Aguarda reprodução com DevTools Network aberto pra ver o status do POST e do GET subsequente.

### 4.3 Caminho 2 — Latency trace no log estruturado

Registrado em `roadmap.md` §3.1 (P2). Propagar `*LatencyTrace` via `r.Context()` pra que o middleware `Logging` enriqueça `request_completed` com os 5 buckets. Diff esperado: ~30 LOC. Sem urgência — Caminho 1 (header) ainda funciona pra validação.

### 4.4 Desacoplamento do frontend (roadmap.md §3.8)

5 sub-decisões pendentes. Sem urgência — anotado.

### 4.5 Cache de prompts (semantic) — roadmap.md §4.3

P3, sem urgência.

### 4.6 Modo híbrido para migrations (auto-apply vs manual)

Discutido em 2026-05-27. Hoje o gateway roda `migrate.Up()` no boot
(modo auto-apply). Bom pra dev/homolog, mas em prod corporativo o DBA quer
controlar quando DDL roda (janela de mudança, peer review).

**Proposta:** flag `migrations_auto_apply` (config + env `MIGRATIONS_AUTO_APPLY`).
- `true` (default): comportamento atual (`m.Up()` no boot).
- `false`: gateway só verifica que `schema_migrations.version` é igual à
  `latestKnownVersion` hardcoded no binário. Se diferente, falha o boot com
  mensagem clara ("schema version is N, expected M — DBA must apply
  migrations"). DBA aplica via `migrate -database ... -path migrations up` em
  janela controlada.

**Tempo estimado:** 30-60 min. ~20 LOC em `internal/db/migrate.go` + 1 campo
em `internal/config/config.go` + atualização do `production-deploy.md` e do
`maintenance.md`.

**Quando virar prioridade:** quando for empacotar o gateway para deploy
corporativo real (não só homolog dev). Hoje fica como P2 / Eixo Segurança
no roadmap, mas sem urgência.

---

## 5. Dívidas conhecidas

### 5.1 Migrations PG em `migrations/postgres-legacy/`

Movidas pra subdir como referência histórica. `golang-migrate` ignora subdirs, então não rodam. **Não apagar** — são úteis pra entender a evolução do schema antes da troca.

### 5.2 SPEC.md desatualizada

O contrato (`SPEC.md`) ainda menciona PostgreSQL/pgx em várias seções. Foi parcialmente atualizada nesta sessão; o resto fica como tech debt (não bloqueia operação). Itens já anotados em `roadmap.md` §6.

### 5.3 Onda 4.5 — Target credentials no KV (entregue 2026-05-28)

✅ Entregue. ADR-0020 `accepted`. Schema novo `proxy_targets.credential_storage_mode {aes|kv|both}` + `kv_secret_name`; resolver com timeout 200 ms + fallback AES em modo `both`; CLI `cmd/migrate-targets-to-kv`; endpoint admin `POST /admin/v1/endpoints/{id}/targets/{tid}/migrate-to-kv`; botão UI "Migrar para Key Vault". Targets existentes ficaram em `aes` (zero migração compulsória). Onda futura abre ADR pra descontinuar AES quando KV provar SLA.

### 5.4 Latência ainda dominada pelo Azure

Premissa permanente: o gateway adiciona ~150-310ms ao total (~85-95% é Azure puro). Mesmo otimizando, latência mínima fica em ~1.5s pra `gpt-4.1`. Pra "latência perceived menor", a frente real é **streaming**, não otimizar pipeline. Onda 8 (áudio bidirecional) é justamente essa frente — Voice Live entrega sub-segundo (404ms média, 571ms p95) no spike.

### 5.5 Rotação de chaves vazadas da POC AgentFlow

Owner declarou explicitamente em 2026-05-27: "Sei que é errado mas vamos manter
essas chaves por enquanto, o pessoal deixou sem criptografia, depois eu
rotaciono, vamos focar na funcionaliade primeiro". Lista das chaves expostas
documentada em `roadmap.md §3.3 P1`. Item P1 mas sem ETA.

---

## 6. Comandos úteis pra retomar

Bloco **bash** assume Ubuntu/Linux/macOS/WSL/Git Bash. Bloco **pwsh** assume Windows PowerShell.

```bash
# Confirmar estado
cd ~/projects/ai-gateway   # ou caminho equivalente
git status --short
git log --oneline -5

# Validar build
go mod tidy && go vet ./... && go build ./... && go test -count=1 -race ./...

# Senha do SQL pro admin-create (sem expor no history)
export DATABASE_URL="sqlserver://usr_sist_AzureAI_Gateway_hom:$(az keyvault secret show --vault-name danieldev --name AzureAIGateway-DB-Password-hom --query value -o tsv)@BRSPVPDEV003:1433?database=AzureAI_Gateway_hom&encrypt=true&trustServerCertificate=true"

# Limpar dirty state (se migration falhar parcial)
# Conecta via GoLand Database tab ou sqlcmd e roda:
#   UPDATE dbo.schema_migrations SET dirty = 0;
#   (se quiser regredir, use também: UPDATE dbo.schema_migrations SET version = N;)

# Frontend dev
cd web && npm run dev   # Vite em http://localhost:5173

# Frontend bundle pro go:embed
cd web && npm run build
```

```pwsh
# Equivalente Windows PowerShell
cd "E:/Teleperformance CRM SA/Arquitetura/Fontes/GoGateway/ai-gateway"
git status --short
git log --oneline -5

go mod tidy ; if ($?) { go vet ./... } ; if ($?) { go build ./... } ; if ($?) { go test -count=1 -race ./... }

$env:DATABASE_URL = "sqlserver://usr_sist_AzureAI_Gateway_hom:$(az keyvault secret show --vault-name danieldev --name AzureAIGateway-DB-Password-hom --query value -o tsv)@BRSPVPDEV003:1433?database=AzureAI_Gateway_hom&encrypt=true&trustServerCertificate=true"
```

---

## 7. Run Configurations dos IDEs

### GoLand — gateway

- Run Config principal: `cmd/gateway`
- `.env` carregado pelo plugin **EnvFile** ou campo nativo "Paths to '.env' files"
- Working dir: raiz do projeto (precisa conter `configs/` e `migrations/`)

Variáveis críticas no `.env`:

- `KEYVAULT_URI=https://danieldev.vault.azure.net/` — pra resolução de `${kv:...}`
- `AZURE_OPENAI_ENDPOINT=https://danie-mc4ryviy-westeurope.cognitiveservices.azure.com`
- `AZURE_LANGUAGE_ENDPOINT=https://tp-language-pii.cognitiveservices.azure.com`
- **NÃO precisa mais `DATABASE_URL` pro gateway** (config estruturado em `configs/gateway.yaml`); só `admin-create` ainda usa.

Auth Azure: `az login --tenant c050c98c-b463-4591-ac3b-deb782c0ba6e` (MFA interativo via browser).

### WebStorm — frontend

Abrir `web/` numa janela paralela. Scripts npm aparecem no painel **npm** lateral. Daily dev:

- Gateway no GoLand (porta 8080)
- `npm run dev` no WebStorm (porta 5173, com proxy de `/admin`, `/v1`, `/healthz`, `/readyz` pra :8080)
- Acessa `http://localhost:5173/ui`

Detalhes em `docs/local-development.md §3`.

### Re-criando Run Configs no Ubuntu

GoLand armazena Run Configs em `.idea/runConfigurations/*.xml` — esses arquivos
**não vão pro git** (`.idea/` está parcialmente em `.gitignore`). Você vai precisar
recriar no notebook seguindo `docs/local-development.md §3.1` (5 minutos).

---

## 8. Onde está cada coisa

| Pergunta | Resposta |
|---|---|
| "O que o gateway faz?" | `SPEC.md` + `docs/how-it-works.md` + vault `KB/AI-Gateway/Visao-geral.md` |
| "Por que essa decisão arquitetural?" | `docs/adrs/NNNN-*.md` ou vault `KB/AI-Gateway/ADRs/ADR-NNNN-*.md` (resumo) |
| "Como rodar localmente?" | `docs/local-development.md` |
| "Como deploy?" | `docs/production-deploy.md`, `docs/deployment.md` |
| "Como configurar KV?" | `docs/keyvault-setup.md` |
| "O que está na lista pra fazer?" | `docs/roadmap.md` |
| "Como o gateway está estruturado por dentro?" | `docs/how-it-works.md` + vault `KB/AI-Gateway/Stack/_MOC.md` |
| "Por onde retomar a sessão?" | **Este arquivo** (`docs/handoff.md`) |
| "Quais bugs/dúvidas ainda em aberto?" | §4 + §5 deste arquivo + `KB/AI-Gateway/Decisoes-em-aberto.md` |
| "Como o owner gosta de trabalhar?" | `CLAUDE.md` (contrato de comportamento) |
