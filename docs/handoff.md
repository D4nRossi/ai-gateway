# Handoff — retomada da sessão

> **Quando ler:** ao abrir o projeto amanhã. Esse documento é "como continuar
> sem perder contexto". Siga em ordem das seções.
>
> **Quando editar:** ao final de cada sessão de trabalho, sobrescreva com o
> novo estado. Não acumular versões — esse arquivo é de **uso atual**, não
> histórico (pra histórico use `roadmap.md` §6 e o `git log`).

---

## 1. Estado em que paramos

**Data:** sessão fechada em 2026-05-27 (noite). Cinco frentes encadeadas:
Onda 6 → fix Bug 1 (token mismatch) → ADR-0022 proposed → atualização do
CLAUDE.md → troca PostgreSQL → SQL Server completa em código.

**Último commit aplicado:** `bee7c6f feat: troca PostgreSQL → SQL Server (ADR-0022, schema gogateway)`

### O que está commitado (e validado em build/test)

| Commit | Conteúdo |
|---|---|
| `d79d05d` | Onda 6 — latency breakdown observável (ADR-0021) |
| `f4b5e6e` | Fix Bug 1 — colisão de key_prefix em apps com nome similar (migration 009) |
| `dd21169` | ADR-0022 + edits do working tree pré-SQL ("pre sql") |
| `85e6b97` | docs(claude) — CLAUDE.md alinhado com ADR-0022 (T-SQL, deps) |
| `bee7c6f` | Troca PostgreSQL → SQL Server completa: driver, migrations T-SQL, repos `internal/infra/mssql`, writers, config, main.go, admin-create |

**Suite 100% verde:** `go vet ./...`, `go build ./...`, `go test -count=1 -race ./...` — 15 pacotes, sem race detector flags.

### O que NÃO foi validado ainda (próximo passo crítico)

- **Smoke test ao vivo contra o SQL Server real** (BRSPVPDEV003 / AzureAI_Gateway_hom). Tudo o que está commitado passa em build/test mas nunca rodou contra o banco corporativo.
- Validação ao vivo da Onda 6 (Caminho 1: header `X-Gateway-Latency-Breakdown` + 1 query SQL). Foi interrompida pela troca emergencial — retomar **depois** que o SQL Server estiver bootando limpo.
- Bug 2 — Acessos não persiste (logs instrumentados no commit `f4b5e6e`; aguarda repro com DevTools Network).

---

## 2. Próxima ação — smoke test SQL Server, em 5 passos

### Pré-requisitos do ambiente

| Item | Validar antes de prosseguir |
|---|---|
| `az login` no tenant `c050c98c-b463-4591-ac3b-deb782c0ba6e` | `az account show --query tenantId` |
| Secret no KV: `AzureAIGateway-DB-Password-hom` | `az keyvault secret show --vault-name danieldev --name AzureAIGateway-DB-Password-hom --query "{name:name,enabled:attributes.enabled}"` |
| Acesso de rede ao SQL Server `BRSPVPDEV003:1433` (VPN ou rede corp) | `Test-NetConnection BRSPVPDEV003 -Port 1433` |
| User `usr_sist_AzureAI_Gateway_hom` com permissão de **CREATE SCHEMA** OU schema `gogateway` já criado pelo DBA | conferir com o time de banco |
| User `usr_sist_AzureAI_Gateway_hom` com permissão de **CREATE TABLE** em `dbo` (pra `schema_migrations` do golang-migrate) E em `gogateway` (pras tabelas operacionais) | conferir com DBA |

Se algum desses falhar, **pare e resolva antes de bootar o gateway** — caso contrário a migration 001 vai falhar e o `schema_migrations` fica dirty.

### Passo 1 — Confirma estado git limpo

```pwsh
cd "E:/Teleperformance CRM SA/Arquitetura/Fontes/GoGateway/ai-gateway"
git status --short
git log --oneline -5
```

Esperado: working tree limpo (exceto `.idea/workspace.xml` ignorável), HEAD em `bee7c6f`.

### Passo 2 — Restart no GoLand

Stop → Run na Run Config `gateway`. Olhe o log de boot procurando, **em ordem**:

```
{"level":"INFO","msg":"ai gateway starting","config_path":"configs/gateway.yaml"}
{"level":"INFO","msg":"sqlserver connected","host":"BRSPVPDEV003","database":"AzureAI_Gateway_hom","schema":"gogateway"}
{"level":"INFO","msg":"applying migration","version":1}
{"level":"INFO","msg":"applying migration","version":2}
... (até version 9)
NOTICE:  migration 009: no duplicate api_keys rows found, schema was already clean
{"level":"INFO","msg":"migrations applied"}
{"level":"INFO","msg":"admin api configured"}
{"level":"INFO","msg":"generic proxy plane configured"}
{"level":"INFO","msg":"server listening","addr":":8080"}
```

A linha `NOTICE: migration 009 ...` vem do `RAISERROR(...) WITH NOWAIT` em PL/T-SQL. No primeiro boot ela aparece com `no duplicate api_keys rows found, schema was already clean` (banco virgem).

### Passo 3 — Cenários de falha esperáveis (e como tratar)

| Sintoma no log | Causa | Ação |
|---|---|---|
| `pinging sqlserver at BRSPVPDEV003:1433: dial tcp: ... no route to host` | VPN/firewall | Habilita VPN; testa `Test-NetConnection` |
| `pinging sqlserver: login failed for user 'usr_sist_AzureAI_Gateway_hom'` | Senha errada no KV ou user sem permissão | Conferir secret no KV; conferir login com DBA |
| `pinging sqlserver: TLS Handshake failed` | Cert do SQL Server inválido | Verificar `database.encrypt: true` + `database.trust_server_certificate: true` no YAML (homolog usa cert self-signed) |
| `running migrations: ... CREATE SCHEMA permission denied in database 'AzureAI_Gateway_hom'` | User não tem CREATE SCHEMA | Pedir DBA pra criar schema gogateway manualmente |
| `running migrations: ... cannot create object 'dbo.schema_migrations' permission denied` | User não tem CREATE TABLE em dbo | Pedir DBA pra dar grant; alternativa: GRANT CREATE TABLE TO usr_sist_AzureAI_Gateway_hom |
| `Dirty database version 1. Fix and force version.` | Migration 001 falhou parcial | Conectar no SQL, `UPDATE dbo.schema_migrations SET dirty=0` (mais detalhes em `roadmap.md` §6) |

Em qualquer falha, **não tentar mexer no código** — anotar o sintoma + me avisar.

### Passo 4 — Logar no Console com o admin "root" provisionado pela migration 010

A partir da troca SQL Server, **não precisa mais rodar `cmd/admin-create` em
ambiente virgem** — a migration 010 já provisiona o user `root` com senha
temporária bootstrap. Acessa `http://localhost:8080/ui`:

```
Username: root
Senha:    Adm!nGogateway2026
```

**Imediatamente após login:**
1. Cria seu próprio admin pessoal pela UI (role=admin) — atribui seu nome
2. Sai e re-loga com seu admin pessoal
3. Trocar a senha do `root` pela UI **OU** desativar o `root` (Active=false)

Quando o SSO Entra ID/SAML for implementado (sem ETA — ver `roadmap.md` §3.3),
o `root` local será removido permanentemente via migration de cleanup.

O `cmd/admin-create` continua disponível pra criar admins adicionais via CLI,
mas o caminho normal agora é pela UI.

### Passo 5 — Criar 1 aplicação + 1 endpoint + smoke do proxy

Pela UI:
1. Criar app **AppPro** (Tier 2, allowed_models `[gpt-4.1-mini, gpt-4.1-nano]`, streaming permitido). Anotar token gerado.
2. Criar endpoint **azure-foundry** (slug `danielv2`, provider_kind `azure_openai`, `provider_config: {"api_version": "2025-01-01-preview", "model_to_deployment": {"gpt-4.1-mini":"gpt-4.1-mini"}}`).
3. Adicionar 1 target ao endpoint (URL do Azure OpenAI; auth_type=`api_key_header` com a Azure key).
4. Na aba Acessos da app, conceder grant ao endpoint.

Faz request:
```pwsh
curl -i -X POST "http://localhost:8080/v1/proxy/danielv2/chat/completions" `
  -H "Authorization: Bearer gwk_apppro_<rest>" `
  -H "Content-Type: application/json" `
  -d '{\"model\":\"gpt-4.1-mini\",\"messages\":[{\"role\":\"user\",\"content\":\"oi\"}]}'
```

Esperado: `200 OK` + header `X-Gateway-Latency-Breakdown` + body OpenAI-compatible.

Se passar, **smoke test fechado**. ADR-0022 status passa de `proposed` → `accepted`.

---

## 3. Cenários de resultado e próxima frente

| Smoke test resultado | Próxima onda |
|---|---|
| Tudo verde, request retorna 200 | **ADR-0022 → accepted**, retomar **Caminho 1 da Onda 6** (10–20 requests pra validar header `X-Gateway-Latency-Breakdown`), depois rodar a query SQL única de fechamento da Onda 6 |
| Migration falha por permissão | Coordenar com DBA antes de qualquer outra coisa — sem isso o resto não roda |
| Migration aplica mas request 500 | Anotar erro do log + me chamar — provavelmente bug residual no port de algum repo |
| Bug 2 (Acessos não persiste) ainda aparece | Reproduzir com DevTools Network aberto; manda log do gateway (deve aparecer `event_type=grant_created` ou `grant_revoked` com o `application_id`/`endpoint_id`) |

---

## 4. Decisões em aberto

Anotadas pra não esquecer; **não bloqueiam** o smoke test.

### 4.1 Compilado de docs gerais no Obsidian (sugerido pelo owner)
**Pedido (2026-05-27):** "tem uma skill MCP pro meu obsidian, ia ser interessante documentar nele também — compilado de documentações gerais, stacks, infra, comandos, how to use, etc."

**Escopo proposto** (a fechar):
- Estrutura: 1 cluster por eixo (Stack, Infra, Comandos, How-to-Use, ADRs, Decisões)
- Fonte da verdade: docs/* deste repo (preservar fidelidade — não inventar nem deduzir)
- Quando: depois do smoke test passar; usar a skill `obsidian-knowledge-vault` (já disponível) pra gerar/manter as notas

**Sem urgência.** Quando virar prioridade, abrir tarefa dedicada (não vira ADR — é doc operacional externa).

### 4.2 Bug 2 — Acessos não persiste
Diagnóstico no commit `f4b5e6e` ficou inconclusivo no código. Instrumentação adicionada em `internal/app/adminservice/service.go` (logs `event_type=grant_created`/`grant_revoked`). Aguarda reprodução com DevTools Network aberto pra ver o status do POST e do GET subsequente.

### 4.3 Caminho 2 — Latency trace no log estruturado
Registrado em `roadmap.md` §3.1 (P2). Propagar `*LatencyTrace` via `r.Context()` pra que o middleware `Logging` enriqueça `request_completed` com os 5 buckets. Diff esperado: ~30 LOC. Sem urgência — Caminho 1 (header) ainda funciona pra validação.

### 4.4 Desacoplamento do frontend (roadmap.md §4.1)
5 sub-decisões pendentes. Sem urgência — anotado.

### 4.5 Cache de prompts (semantic) — roadmap.md §4.3
P3, sem urgência.

### 4.6 Modo híbrido para migrations (auto-apply vs manual)
**Discutido em 2026-05-27.** Hoje o gateway roda `migrate.Up()` no boot
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

**Por que isso e não "deixar tudo dinâmico":** auto-recuperação só vale
quando o problema é trivial e não mascara bug real (exemplos válidos: 007
descobrir nome de constraint, 009 quarentenar duplicatas, 005 deferred
name resolution via EXEC). Pra controle de schema em prod, previsibilidade
> mágica — o DBA precisa **ver** a migration antes de rodar.

**Quando virar prioridade:** quando for empacotar o gateway para deploy
corporativo real (não só homolog dev). Hoje fica como P2 / Eixo Segurança
no roadmap, mas sem urgência.

---

## 5. Dívidas conhecidas

### 5.1 Migrations PG em `migrations/postgres-legacy/`
Movidas pra subdir como referência histórica. `golang-migrate` ignora subdirs, então não rodam. **Não apagar** — são úteis pra entender a evolução do schema antes da troca.

### 5.2 ADR-0022 status = proposed
Vira `accepted` após smoke test (Passo 5 acima). Atualizar manualmente o status na seção do ADR depois.

### 5.3 SPEC.md desatualizada
O contrato (`SPEC.md`) ainda menciona PostgreSQL/pgx em várias seções. Foi parcialmente atualizada nesta sessão; o resto fica como tech debt (não bloqueia operação). Itens já anotados em `roadmap.md` §6.

### 5.4 Onda 4.5 — Target credentials no KV (ainda não iniciada)
Resolve o problema "rotacionar `DB_ENCRYPTION_KEY` quebra targets". Próxima onda sugerida **depois** que o ambiente SQL Server estiver estável. Escopo completo em `roadmap.md` §3.3.

### 5.5 Latência ainda dominada pelo Azure
Premissa permanente: o gateway adiciona ~150-310ms ao total (~85-95% é Azure puro). Mesmo otimizando, latência mínima fica em ~1.5s pra `gpt-4.1`. Pra "latência perceived menor", a frente real é **streaming**, não otimizar pipeline.

---

## 6. Comandos úteis pra retomar

```pwsh
# Confirmar estado
cd "E:/Teleperformance CRM SA/Arquitetura/Fontes/GoGateway/ai-gateway"
git status --short
git log --oneline -5

# Validar que tudo ainda buila
go vet ./...
go build ./...
go test -count=1 -race ./...

# Pegar a senha do SQL Server pro admin-create (sem expor no shell history)
$env:DATABASE_URL = "sqlserver://usr_sist_AzureAI_Gateway_hom:$(az keyvault secret show --vault-name danieldev --name AzureAIGateway-DB-Password-hom --query value -o tsv)@BRSPVPDEV003:1433?database=AzureAI_Gateway_hom&encrypt=true&trustServerCertificate=true"

# Criar admin
go run ./cmd/admin-create -username admin -role admin

# Limpar dirty state do schema_migrations (se uma migration falhar parcial)
# Conecta via tab Database do GoLand ou sqlcmd e roda:
#   UPDATE dbo.schema_migrations SET dirty = 0;
#   (se quiser regredir, use também: UPDATE dbo.schema_migrations SET version = N;)

# Frontend (se quiser rodar separado em dev)
cd web
npm run dev   # Vite em http://localhost:5173

# Frontend embedado (build pro Go go:embed)
cd web
npm run build
```

---

## 7. Run Configurations do GoLand

Pra rodar localmente:
- **gateway** — Run Config principal, `cmd/gateway`, `.env` carregado pelo plugin EnvFile, working dir = raiz do projeto
- **admin-create** — CLI, só pra provisionar admin novo, raramente usada

Variáveis críticas no `.env`:
- `KEYVAULT_URI=https://danieldev.vault.azure.net/` — pra resolução de `${kv:...}`
- `AZURE_OPENAI_ENDPOINT=https://danie-mc4ryviy-westeurope.cognitiveservices.azure.com`
- `AZURE_LANGUAGE_ENDPOINT=https://tp-language-pii.cognitiveservices.azure.com`
- **NÃO precisa mais `DATABASE_URL` pro gateway** (config estruturado em `configs/gateway.yaml`); só `admin-create` ainda usa.

Auth Azure: `az login --tenant c050c98c-b463-4591-ac3b-deb782c0ba6e` (tem MFA — fluxo interativo pelo browser).

Detalhes completos em `docs/local-development.md` e `docs/keyvault-setup.md`.
