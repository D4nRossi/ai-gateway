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

**Data:** sessão fechada em 2026-05-27 (noite, máquina Windows). Pacote da
sessão: Onda 6 (latency breakdown) → fix Bug 1 (token mismatch) → ADR-0022 →
atualização do CLAUDE.md → troca PostgreSQL → SQL Server completa →
smoke test passou → reorganização do roadmap → spike Voice Live →
ADR-0023 redigido (proposed) → **vault Obsidian populado com 55 notas em
`KB/AI-Gateway/`**.

**Onda 7 (troca PG → SQL Server) está oficialmente entregue** — ADR-0022
`accepted`. Smoke test rodou contra `BRSPVPDEV003`/`AzureAI_Gateway_hom`:
migrations 001-010 aplicadas, 3 apps criadas via UI, 1 endpoint Azure,
request `/v1/proxy/{slug}/chat/completions` respondeu 200 OK com header
`X-Gateway-Latency-Breakdown` populado.

### O que está commitado (e validado em build/test)

| Commit | Conteúdo |
|---|---|
| `d79d05d` | Onda 6 — latency breakdown observável (ADR-0021) |
| `f4b5e6e` | Fix Bug 1 — colisão de key_prefix em apps com nome similar (migration 009) |
| `dd21169` | ADR-0022 + edits do working tree pré-SQL ("pre sql") |
| `85e6b97` | docs(claude) — CLAUDE.md alinhado com ADR-0022 (T-SQL, deps) |
| `bee7c6f` | Troca PostgreSQL → SQL Server completa: driver, migrations T-SQL, repos `internal/infra/mssql`, writers, config, main.go, admin-create |

**Suite 100% verde:** `go vet ./...`, `go build ./...`, `go test -count=1 -race ./...` — 15 pacotes.

### Trabalho não-commitado (working tree na máquina Windows)

⚠️ Quando trocar de máquina amanhã, **fazer commit ou stash** desses antes:

```
M  .idea/workspace.xml                                  (ignorável)
M  docs/how-it-works.md                                 (alinhamento pós-SQL Server)
M  docs/roadmap.md                                      (reorg + Onda 8)
M  internal/api/handlers/chat.go                        (Onda 6 — header X-Gateway-Latency-Breakdown)
M  internal/usage/event.go                              (Onda 6 — 5 colunas latency)
M  internal/usage/writer.go                             (Onda 6)
?? docs/adrs/0021-latency-breakdown-observavel.md       (Onda 6)
?? docs/adrs/0023-streaming-audio-bidirecional.md       (Onda 8 — proposed, ainda não lido pelo owner)
?? docs/handoff.md                                      (este arquivo)
?? internal/observability/trace.go                      (Onda 6)
?? internal/observability/trace_test.go                 (Onda 6)
?? migrations/008_usage_events_latency_breakdown.down.sql
?? migrations/008_usage_events_latency_breakdown.up.sql
?? _voicelive-spike/                                    (spike Voice Live — Onda 8)
```

**Sugestão de sequência de commits** antes de fechar a máquina Windows hoje:

1. `feat(observability): latency breakdown (Onda 6, ADR-0021)` — trace.go, trace_test.go, chat.go, event.go, writer.go, migrations 008
2. `docs(adr): adicionar ADR-0023 streaming áudio bidirecional (proposed)` — apenas o ADR-0023
3. `spike(voice-live): cliente isolado para baseline de latência` — `_voicelive-spike/` inteiro
4. `docs: atualizar roadmap (Onda 8), how-it-works (pós-SQL Server) e handoff` — docs/* atualizados

### Próxima ação (Ubuntu notebook)

**Onda 4.5 (Target credentials no Key Vault) foi entregue** em 2026-05-28
— ADR-0020 `accepted`. Próxima onda: a decidir entre as frentes pendentes
abaixo (ou o owner sugere outra). Sugestão de ordem:

1. **Cache de lookup** (§3.1 Desempenho, P1). Tira 2 DB hits por request com
   LRU+TTL em memória. ~5-10 ms de ganho. Baixo risco, sem ADR pesado.
2. **SSO Entra ID / OIDC** (§3.3 Segurança, P1) — depende de App Registration
   no Entra corporativo (passo externo).
3. **Modelos como CRUD + Page Models** (§3.4, P2) — unifica YAML/DB pra modelos.

### Frentes pendentes (sem ETA específica)

- **Validação ao vivo da Onda 6** (Caminho 1: header + 1 query SQL).
- **Bug 2 — Acessos não persiste** — instrumentação adicionada, aguarda repro com DevTools Network.
- **SSO Entra ID / OIDC** (P1 Segurança — ADR sem número ainda). Quando rolar, migration remove o `root` da mig 010.
- **Rotação de chaves vazadas** da POC AgentFlow (Voice Live, ElevenLabs, Cartesia, MS Graph, Zenvia, etc.) — owner postergou explicitamente em 2026-05-27.

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
