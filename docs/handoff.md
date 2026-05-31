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

**Data:** pausa em 2026-05-29 (durante deploy Windows). Status:
**deploy Windows do gateway está em progresso, travado na fase de ACLs**
(`E:\AIGateway` ficou com Owner sem permissão de Read após `icacls /deny`).
Owner pediu pausa pra retomar depois.

**Detalhe operacional importante:** o MCP do Obsidian caiu durante a sessão
e não voltou. Vault não foi atualizado com o material novo. Quando o MCP
voltar, espelhar o conteúdo das seções abaixo + `docs/adrs/0024-0026` no
vault.

### Onde estamos no deploy Windows

| Etapa | Status |
|---|---|
| Build do pacote `.zip` na Ubuntu workstation | ✅ feito (`ai-gateway-deploy-69ed91f.zip`) |
| Transporte pro servidor + extração em `E:\AIGateway\` | ✅ feito (`bin/`, `configs/`, `migrations/`, `SHA256SUMS`) |
| Pasta `logs/` no servidor | ❌ falta criar |
| ACL inicial — script `/deny "Users:(W)"` quebrou | 🔥 **travado** — Administrator perdeu Read na pasta |
| Cifrar `gateway.env` via DPAPI | ⏳ pendente (PowerShell pronto, falta executar) |
| Migrations manuais (`migrate up`) | ⏳ pendente |
| Instalar WinSW como serviço | ⏳ pendente |
| Configurar IIS (URL Rewrite + ARR + cert TLS) | ⏳ pendente |
| Smoke test (`/healthz`, login admin, chat) | ⏳ pendente |

### Decisões consolidadas pra esse deploy

| Item | Valor |
|---|---|
| Path raiz no servidor | `E:\AIGateway\` (disco E:) |
| Service account | `sv_autoccms` (não-gMSA — senha vai no WinSW XML, opção B) |
| Senha no XML | aceitar com ACL apertada; preferível gMSA no futuro |
| `SECRET_PROVIDER` | `db` (sem Azure KV) |
| `DPAPI_ENV_FILE` | `E:\AIGateway\configs\gateway.env.dpapi` |
| `MIGRATIONS_AUTO_APPLY` | `false` (DBA roda manual) |
| Azure Language no yaml V1 | comentado (Tier 2/3 PII só por regex local) |
| Azure OpenAI no yaml V1 | removido (endpoints cadastrados via Console / proxy plane) |
| SQL Server inicial | `BRSPVPDEV003.tpb.corp` (mesmo de homolog) |
| Database | `AzureAI_Gateway_hom` |
| SQL user | `usr_sist_AzureAI_Gateway_hom` |
| `DB_ENCRYPTION_KEY_HEX` (gerado) | `4b3d200ab31e8ede05af67a70632db4e02c01630eac9d1d2e5f51935117bbea1` |

⚠️ **A chave AES é viva.** Quem tiver acesso a esse arquivo + ao
`gateway.env.dpapi` consegue derivar todos os secrets. Tratar como dado
sensível conforme classificação corp.

### O que foi entregue nessa sessão (e pushado)

Commits no `main` / `v2` (push confirmado pelo owner):

- **Onda 4.5** — Target credentials no Key Vault (ADR-0020 `accepted`)
- **ADR-0024** — Usage tracking no proxy plane (Playground agora aparece em
  `usage_events` + dashboards)
- **ADR-0025** — `MIGRATIONS_AUTO_APPLY` toggle (default `true`; `false` em
  prod pra DBA controlar janela)
- **ADR-0026 V1** — Secrets Windows sem KV (DPAPI cobre boot; `gogateway.secrets`
  + Always Encrypted cobre runtime). Componentes:
  - `internal/infra/dpapi/` (Windows-only via build tags; stub em Linux/macOS)
  - `internal/infra/secretsdb/` (drop-in pra `keyvault.SecretGetter` + `SecretSetter`)
  - `migrations/012_gogateway_secrets.up.sql` (tabela base; AE manual via PowerShell)
  - `cmd/secrets/` (CLI 5 subcomandos)
  - `cmd/gateway/main.go`: env `SECRET_PROVIDER=kv|db` decide backend
- **Fase A polish** Dashboard + Observability (bugs, skeletons, validações,
  tooltips, refresh, ordenação top spenders)
- **Fase B** Dashboard com 5 charts via recharts (timeseries requests +
  4xx/5xx, latência avg+max, custo BRL área, top apps barra, tier pie)
- **Manuais de deploy**: `docs/deploy/linux.md` + `docs/deploy/windows.md`
- **Postman collection** em `docs/postman/`
- **Script de build Windows** na raiz (`build-windows-deploy.sh`)
- **Notas vault Obsidian** em `KB/AI-Gateway/Deploy/` (3 notas:
  `_MOC`, `Linux-NGINX-Docker`, `Windows-IIS-WinSW`). 3 ADRs novas
  (0024/0025/0026) ainda **não foram pra vault** porque o MCP caiu.

### Como retomar (próxima sessão)

Owner abre o Claude Code e diz "vamos continuar o deploy Windows":

**Passo 0 — Reset ACL no servidor pra desbloquear**

Fechar a janela do File Explorer. PowerShell como Administrator:

```powershell
takeown /F E:\AIGateway /R /D Y
icacls E:\AIGateway /reset /T
Get-ChildItem E:\AIGateway   # confirmar acesso restaurado
```

**Passo 1 — Aplicar ACL correta (sem o `deny` problemático)**

```powershell
$account = "DOMAIN\sv_autoccms"   # ← substituir DOMAIN pelo AD real

# Administrators full + propagar
icacls E:\AIGateway /grant "Administrators:(OI)(CI)F" /T

# Criar logs/ se ainda não existe
New-Item -ItemType Directory -Force -Path E:\AIGateway\logs | Out-Null

# Permissões por pasta
icacls E:\AIGateway\logs        /grant "${account}:(OI)(CI)M"  /T
icacls E:\AIGateway\bin         /grant "${account}:(OI)(CI)RX" /T
icacls E:\AIGateway\configs     /grant "${account}:(OI)(CI)R"  /T
icacls E:\AIGateway\migrations  /grant "${account}:(OI)(CI)R"  /T

# Trava o XML do serviço (senha dentro)
icacls E:\AIGateway\bin\gateway-service.xml /inheritance:r
icacls E:\AIGateway\bin\gateway-service.xml /grant "SYSTEM:(F)" "Administrators:(F)" "${account}:(R)"
```

⚠️ Atenção: **NÃO usar `/deny "Users:(W)"`** — bloqueia o próprio admin
porque a conta de admin pertence ao grupo Users localmente e `deny` tem
precedência sobre `allow`. Foi essa linha que travou na sessão de 2026-05-29.

**Passo 2 — Cifrar `gateway.env` com DPAPI**

```powershell
$envContent = @"
SQL_SERVER_HOST=BRSPVPDEV003.tpb.corp
SQL_DATABASE_NAME=AzureAI_Gateway_hom
SQL_USER=usr_sist_AzureAI_Gateway_hom
DATABASE_PASSWORD=<SENHA_DO_USR_SIST_AZUREAI_GATEWAY_HOM>
DB_ENCRYPTION_KEY_HEX=4b3d200ab31e8ede05af67a70632db4e02c01630eac9d1d2e5f51935117bbea1
"@

$bytes  = [System.Text.Encoding]::UTF8.GetBytes($envContent)
$cipher = [System.Security.Cryptography.ProtectedData]::Protect(
    $bytes, $null, [System.Security.Cryptography.DataProtectionScope]::LocalMachine)
[System.IO.File]::WriteAllBytes('E:\AIGateway\configs\gateway.env.dpapi', $cipher)

icacls E:\AIGateway\configs\gateway.env.dpapi /inheritance:r
icacls E:\AIGateway\configs\gateway.env.dpapi /grant "SYSTEM:(F)" "Administrators:(F)" "${account}:(R)"

# Sanity check
$enc = [System.IO.File]::ReadAllBytes('E:\AIGateway\configs\gateway.env.dpapi')
$dec = [System.Security.Cryptography.ProtectedData]::Unprotect(
    $enc, $null, [System.Security.Cryptography.DataProtectionScope]::LocalMachine)
[System.Text.Encoding]::UTF8.GetString($dec)
# saída esperada: as 5 linhas KEY=VALUE
```

**Passo 3 — Aplicar migrations (do laptop/bastion com acesso ao SQL)**

```bash
export DATABASE_URL='sqlserver://usr_sist_AzureAI_Gateway_hom:<SENHA>@BRSPVPDEV003.tpb.corp:1433?database=AzureAI_Gateway_hom&encrypt=true&trustServerCertificate=false'

migrate -database "$DATABASE_URL" -path migrations up
migrate -database "$DATABASE_URL" -path migrations version
# esperado: 12 (após adr-0026)
```

Se aparecer `dirty=1`, ver `docs/deploy/windows.md §6.3` ou seguir o
padrão de cleanup que owner já fez na sessão da migration 011.

**Passo 4 — Instalar e iniciar o WinSW**

```powershell
cd E:\AIGateway\bin
.\gateway-service.exe install
Start-Service AIGateway
Get-Service AIGateway   # esperado: Status=Running
Get-Content E:\AIGateway\logs\AIGateway.out.log -Tail 30
```

Se o serviço falhar, ver `AIGateway.err.log` e
`AIGateway.wrapper.log`. Causas comuns:

- `sv_autoccms` sem `Logon as a service` right → `secpol.msc`
- DPAPI env file inacessível → conferir ACL do passo 1
- Senha do user SQL errada → editar e re-cifrar `.env.dpapi`

**Passo 5 — IIS** (URL Rewrite + ARR + cert TLS) — seguir
`docs/deploy/windows.md §8`. Pré-req: módulos URL Rewrite 2.1 + ARR 3.0
instalados (MSIs separados, baixar na workstation com internet).

**Passo 6 — Smoke test** — seguir
`docs/deploy/windows.md §9`.

### Frentes pendentes (sem ETA específica)

- **Re-sincronizar vault Obsidian** quando o MCP voltar:
  - 3 ADRs novas (0024/0025/0026) precisam ser espelhadas em `KB/AI-Gateway/ADRs/`
  - Atualizar `00-Index.md` adicionando os 3 ADRs e link pra `Deploy/_MOC`
- **Validação ao vivo da Onda 6** (header de latency breakdown + 1 query SQL).
- **Bug 2 — Acessos não persiste** — instrumentação adicionada, aguarda repro com DevTools Network.
- **SSO Entra ID / OIDC** (P1 Segurança — ADR sem número ainda). Quando rolar, migration remove o `root` da mig 010.
- **Anthropic / Gemini / Cohere adapters** pra usage extractor (ADR-0024 só cobre azure_openai + openai). Quando outro provider virar P1.
- **Percentis p50/p95/p99** no timeseries do dashboard (hoje só avg+max). `PERCENTILE_CONT` no SQL Server precisa subquery dedicada.
- **Rotação de chaves vazadas** da POC AgentFlow — owner postergou explicitamente em 2026-05-27.
- **Azure Language como CRUD no DB** — owner pediu isso pra ter feature CRUD. Hoje removida do yaml V1 do prod, regex local cobre Tier 2/3 PII até a feature ficar pronta.
- **Após deploy Windows estabilizar**, decidir entre:
  - Cache de lookup (§3.1 Desempenho, P1)
  - SSO Entra ID
  - Modelos como CRUD + Page Models
  - Streaming SSE no proxy

---

## 2. Setup máquina nova (Ubuntu notebook)

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

---

## 3. Sequência amanhã

1. `git pull` no notebook
2. Ler `docs/adrs/0023-streaming-audio-bidirecional.md` (Onda 8) — owner ainda não leu
3. Decisão: aprovar o ADR `proposed` ou editar escopo
4. Se aprovar:
   - Atualizar status do ADR-0023 pra `accepted`
   - Iniciar **Sub-onda 8.1** (proxy Pure Voice Live no gateway)
   - Esboçar plano com Claude Code seguindo o template do CLAUDE.md §3
5. Se ajustar:
   - Editar ADR-0023 antes de aceitar

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
