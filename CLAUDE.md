# CLAUDE.md

> Este arquivo é o **contrato de comportamento** do Claude Code para este repositório.
> O Claude Code **DEVE** ler este arquivo na íntegra antes de qualquer ação.
> O Claude Code **DEVE** ler `SPEC.md` na íntegra antes de qualquer ação.
> Em caso de conflito entre CLAUDE.md, SPEC.md e instruções inline do humano: **CLAUDE.md > SPEC.md > instrução inline**, exceto para correções pontuais explicitamente autorizadas pelo humano.

---

## 0. Identidade do projeto

- **Nome**: AI Gateway (Go)
- **Repo path**: `/home/daniel/projects/ai-gateway/ai-gateway`
- **Owner**: Danirek (Software Architect, Digital Innovation — Teleperformance Brasil)
- **Stack**: Go + PostgreSQL + Azure OpenAI + (opcional) Azure Content Safety
- **Objetivo de curto prazo**: entrega de demo executável. Não é POC descartável — é base para evoluir.
- **Idioma de comunicação com o humano**: Português do Brasil. Comentários de código e nomes de identificadores: inglês técnico.

---

## 1. Regras absolutas (NUNCA quebrar)

### 1.1 Zero invenção
- **NUNCA** inventar funções, métodos, tipos, pacotes, endpoints, headers, parâmetros, query params, schemas JSON, nomes de tabelas, nomes de colunas, ou qualquer outro símbolo.
- **NUNCA** "deduzir" comportamento de bibliotecas que você não conhece de fato.
- Se há dúvida sobre uma API: **consultar documentação oficial** (lista de URLs autorizadas na seção 12) com `WebFetch`, ou avisar o humano que precisa de validação.
- **NUNCA** preencher código com placeholder do tipo `// TODO: implementar lógica de X` e seguir adiante. Se não consegue implementar, **pare e pergunte**.

### 1.2 Zero `latest`
- **NUNCA** usar `latest` em imagens Docker.
- **NUNCA** usar `@latest` em `go get`.
- Toda dependência tem versão **pinada** no `go.mod` ou `Dockerfile`. Versões autorizadas estão na seção 4.

### 1.3 Zero erro silenciado
- **PROIBIDO** `_ = err`, `_, _ := func()`, ou qualquer padrão que descarte erro sem tratamento.
- Toda função que retorna `error` **deve** ter o erro tratado: propagado com `fmt.Errorf("contexto: %w", err)`, logado com severidade adequada, ou explicitamente comentado por quê o erro é seguro de ignorar (com justificativa de 1+ linha).

### 1.4 Zero log de dado sensível
- **NUNCA** logar:
  - Prompt bruto (`messages[].content` do request)
  - Resposta completa do modelo (`choices[].message.content`)
  - Bearer token completo (apenas `key_prefix` pode ser logado)
  - Conteúdo de variáveis de ambiente (`AZURE_OPENAI_API_KEY`, etc.)
  - SQL com parâmetros (apenas `pgx` parameter mode é permitido — `$1, $2`)
- Logs podem conter: `request_id`, `application_name`, `tier`, `model`, `latency_ms`, `status_code`, `event_type`, `severity`, contagens (ex.: `pii_categories: {"BR_CPF":2}`).

### 1.5 Zero mudança não autorizada de escopo
- A estrutura de pastas definida em SPEC.md **não pode ser alterada** sem ADR aprovado pelo humano.
- Adicionar uma dependência nova exige ADR aprovado.
- Mudar contrato de API público (`/v1/...`) exige ADR aprovado.
- Refatoração estética (renomear símbolos, mover funções de lugar) **só** com permissão explícita do humano.

### 1.6 Zero `git commit` automático
- **NUNCA** executar `git commit`, `git push`, `git reset --hard`, `git rebase`, `git merge` sem ordem explícita do humano nesta sessão.
- Pode executar: `git status`, `git diff`, `git log`, `git branch`.

---

## 2. Estado atual do repositório (em 2026-05-19)

### 2.1 O que já foi feito (parcial)
- Estrutura de pastas criada (`internal/api`, `internal/auth`, `internal/audit`, etc.)
- `configs/gateway.yaml` provavelmente presente
- `go.mod` inicializado com módulo `github.com/D4nRossi/ai-gateway`
- Dependências baixadas via `go get` (mas `go.mod` reporta 9+ problemas — provável necessidade de `go mod tidy`)
- Início dos arquivos: `internal/api/middleware/auth.go`, `internal/api/middleware/requestid.go`, `internal/api/middleware/logging.go`, `internal/api/middleware/ratelimit.go`, `internal/api/router.go`, `internal/auth/policy.go`, `internal/auth/hash.go`, `internal/observability/logger.go`

### 2.2 Blocos completados na implementação (do plano)
- ✅ **Bloco 1** (Bootstrap): provavelmente OK
- ✅ **Bloco 2** (Config + logger + request_id): provavelmente OK
- 🟡 **Bloco 3** (Auth + policy): **TEM ERROS REPORTADOS PELO IDE** — 3 problemas, 12 warnings. `auth.go` e `policy.go` estão marcados com problema.
- ⏳ **Bloco 4** (Provider adapter + non-stream): iniciado, **não validado**
- ⏳ Blocos 5–11: pendentes

### 2.3 Primeira tarefa obrigatória do Claude Code nesta sessão
**ANTES** de implementar qualquer coisa nova, executar **auditoria do estado atual**:

```bash
cd /home/daniel/projects/ai-gateway/ai-gateway
go mod tidy
go vet ./...
go build ./...
```

Coletar a saída de cada comando e produzir um **relatório em Markdown** estruturado assim:

```markdown
## Auditoria do estado atual

### go mod tidy
- (saída)

### go vet ./...
- (saída)

### go build ./...
- (saída)

### Conformidade com SPEC.md
- Arquivos criados vs. esperados:
  - ✅ <path>
  - ❌ <path> faltando
  - ⚠️ <path> presente mas divergente de SPEC

### Problemas identificados
1. <descrição precisa do problema, com arquivo:linha>
2. ...

### Plano de correção proposto
1. <ordem mínima de correções com justificativa>
```

**Apresentar este relatório ao humano e AGUARDAR aprovação** antes de tocar em qualquer arquivo. Não corrija nada por iniciativa própria.

Após aprovação, corrigir **apenas** os itens aprovados, na ordem aprovada, **um arquivo de cada vez**, com diff claro antes de aplicar.

### 2.4 Suspeitas a investigar especificamente no Bloco 3
Sem afirmar que existem (deve verificar), os candidatos prováveis baseados na imagem do `auth.go`:
- `extractPrefix(tok)` referenciada mas possivelmente não definida.
- `want, _ := hex.DecodeString(p.KeyHash)` descarta erro — se o hash YAML estiver mal formado, comparação silenciosa retorna falso negativo.
- `gotBytes, _ := hex.DecodeString(got)` faz round-trip desnecessário; alternativa: `subtle.ConstantTimeCompare(sum[:], want)`.

Estas são **hipóteses**. Confirmar antes de propor correção.

---

## 3. Workflow obrigatório por tarefa

Para **toda** tarefa de implementação (novo bloco, correção, refatoração), seguir esta sequência:

1. **Reler SPEC.md** na seção relevante.
2. **Identificar** quais arquivos serão tocados.
3. **Anunciar plano ao humano** em formato:
   ```
   Vou implementar: <descrição curta>
   Arquivos a criar: <lista>
   Arquivos a modificar: <lista>
   Decisões que requerem ADR: <lista ou "nenhuma">
   Dependências novas: <lista ou "nenhuma">
   Documentação oficial a consultar: <URLs>
   ```
4. **Aguardar aprovação** do humano.
5. **Consultar documentação oficial** (`WebFetch` nas URLs autorizadas) **antes** de escrever código se há qualquer dúvida sobre API de lib.
6. **Implementar** arquivo por arquivo, com diff claro.
7. **Documentar** conforme seção 6 (toda função pública, toda decisão não óbvia).
8. **Validar** com `go vet ./...` e `go build ./...` após cada arquivo.
9. **Reportar** ao humano: o que foi feito, o que faltou, o que precisa de revisão.

---

## 4. Stack autorizada (versões pinadas)

### 4.1 Go
- **Versão**: Go 1.24.x. **Não usar** 1.25+ sem aprovação (recurso novos podem não estar disponíveis na imagem Alpine).
- `go.mod` deve declarar `go 1.24`.

### 4.2 Bibliotecas Go (apenas estas — qualquer outra requer ADR)

| Pacote | Versão | Propósito | URL oficial |
|---|---|---|---|
| `github.com/go-chi/chi/v5` | v5.x | HTTP router | https://github.com/go-chi/chi |
| `github.com/jackc/pgx/v5` | v5.x | Postgres driver | https://github.com/jackc/pgx |
| `github.com/jackc/pgx/v5/pgxpool` | v5.x | Postgres connection pool | https://pkg.go.dev/github.com/jackc/pgx/v5/pgxpool |
| `github.com/golang-migrate/migrate/v4` | v4.x | DB migrations | https://github.com/golang-migrate/migrate |
| `gopkg.in/yaml.v3` | v3.x | YAML config parsing | https://pkg.go.dev/gopkg.in/yaml.v3 |
| `github.com/google/uuid` | v1.x | UUID generation | https://pkg.go.dev/github.com/google/uuid |
| `golang.org/x/time/rate` | latest stable | Token bucket rate limiter | https://pkg.go.dev/golang.org/x/time/rate |

### 4.3 Bibliotecas standard library obrigatórias
- `log/slog` (logger oficial, **não** usar `log` antigo nem `zap`)
- `context`
- `net/http`
- `encoding/json`
- `crypto/sha256`
- `crypto/subtle`
- `encoding/hex`
- `errors`
- `fmt`
- `time`
- `sync`

### 4.4 Imagens Docker
- `postgres:17-alpine` (não `latest`, não outra major)
- `golang:1.24-alpine` (build stage)
- `alpine:3.21` (runtime stage)

### 4.5 Bibliotecas explicitamente **proibidas**
- `github.com/sirupsen/logrus`, `go.uber.org/zap` (usar `slog`)
- `github.com/gorilla/mux` (usar `chi`)
- `github.com/jmoiron/sqlx`, `database/sql` (usar `pgx` direto)
- `github.com/joho/godotenv` (carregar `.env` via Docker Compose ou shell)
- Qualquer ORM (GORM, Ent, etc.) — querys SQL diretas com `pgx`
- Qualquer biblioteca não listada em 4.2 sem ADR aprovado

---

## 5. Convenções de código Go

### 5.1 Layout
- Pacote principal: `cmd/gateway/main.go`. Apenas composição de dependências e bootstrap. **Nenhuma lógica de negócio** aqui.
- Todo código de negócio em `internal/`.
- **Não criar** pasta `pkg/` — projeto não publica APIs externas.

### 5.2 Nomes
- Exportados: `PascalCase` (`AppPolicy`, `NewWriter`)
- Não exportados: `camelCase` (`extractPrefix`, `applyMasking`)
- Constantes: `PascalCase` se exportadas, `camelCase` se não. **Não usar `SCREAMING_SNAKE`** em Go.
- Arquivos: `snake_case.go` para múltiplas palavras (`prompt_shield.go`), `single.go` quando único.
- Pacotes: minúsculas, sem underscore, sem mix.

### 5.3 Imports
- Três blocos separados por linha em branco:
  1. Standard library
  2. Bibliotecas terceiras
  3. Pacotes internos do projeto

```go
import (
    "context"
    "fmt"

    "github.com/go-chi/chi/v5"
    "github.com/jackc/pgx/v5/pgxpool"

    "github.com/D4nRossi/ai-gateway/internal/auth"
)
```

### 5.4 Erros
- **Sempre** wrappar com contexto: `fmt.Errorf("loading config from %s: %w", path, err)`.
- **Verbo no gerúndio** ou no infinitivo. Não terminar com pontuação. Não usar maiúscula inicial (convenção Go).
- Erros sentinela (variáveis `var ErrXxx = errors.New(...)`) apenas para casos verificáveis com `errors.Is`.
- **Não usar `panic`** em produção. `panic` apenas em `main.go` quando bootstrap falha (e mesmo aí, prefira `log.Fatal`).

### 5.5 Concorrência
- Toda função que faz I/O: primeiro parâmetro é `ctx context.Context`.
- Toda goroutine: aceita `ctx` ou `<-chan` para sinalização de shutdown.
- `defer cancel()` sempre depois de `context.WithTimeout` / `context.WithCancel`.
- **Não usar** `time.Sleep` em produção (usar `time.After` em `select` com `ctx.Done()`).

### 5.6 Comparação segura de bytes
- Hashes, tokens, secrets: **sempre** `subtle.ConstantTimeCompare`. Nunca `bytes.Equal` ou `==`.

### 5.7 JSON
- Toda struct exposta em endpoint público: tag `json:"snake_case"` explícita em todo campo.
- Campos opcionais: ponteiro + `omitempty` (`*float64` com tag `json:"temperature,omitempty"`).

### 5.8 Testes
- **Quando o humano pedir** testes: estilo *table-driven*. Não testar implementação interna; testar comportamento.
- Arquivo: `xxx_test.go` no mesmo pacote.
- Para HTTP: `httptest.NewRecorder` + `httptest.NewRequest`.
- **Não** adicionar `testify` ou outro framework de teste sem ADR.

---

## 6. Documentação obrigatória

> Documentação é entregável de primeira classe neste projeto. Igual ao código.

### 6.1 Comentário de função/método (Go doc comment)

Toda função/método **exportado** (PascalCase) **deve** ter comentário no formato:

```go
// NomeDaFuncao does X. It returns Y when Z. Returns an error if W.
//
// Reasoning: <por que esta função existe e por que está implementada assim;
//             se há trade-off relevante, explicar; referenciar ADR se aplicável>.
//
// References:
//   - <URL doc oficial relevante>
//   - <referência interna: SPEC.md seção X, ADR-NNN>
func NomeDaFuncao(ctx context.Context, ...) (..., error) {
```

Funções não exportadas **devem** ter comentário quando: contém regra de negócio, faz I/O, ou tem trade-off não óbvio.

Comentários inline (`// ...` no meio do código): use quando o **porquê** não é óbvio. Não explicar o **o quê** se o código já é claro.

### 6.2 Comentário de struct

```go
// AppPolicy represents the runtime authorization policy of a consumer application.
// It is loaded from configs/gateway.yaml at boot and held in memory through PolicyStore.
//
// Reasoning: keys + policies live in config (not DB) on Phase 1 of the demo because
// Admin API is out of scope; migrating to DB is planned (see ADR-002).
type AppPolicy struct {
    // Name is the unique application identifier used in logs and audit events.
    Name string

    // KeyHash is the SHA-256 hex digest of the full bearer token.
    // Comparison is performed in constant time (see auth.Auth middleware).
    KeyHash string

    // ... (todos os campos comentados)
}
```

### 6.3 Comentário de pacote

Todo pacote tem um arquivo `doc.go` (ou comentário no topo do primeiro arquivo) com:

```go
// Package auth implements bearer-token authentication and per-application
// policy lookup for the AI Gateway.
//
// This package is responsible for:
//   - Loading AppPolicy entries from config at boot
//   - Validating incoming bearer tokens against stored SHA-256 hashes
//   - Injecting the matched AppPolicy into request context
//
// It is intentionally backend-agnostic: storage of policies is abstracted
// behind PolicyStore. The current implementation is in-memory; a future
// implementation may read from PostgreSQL (see ADR-002).
//
// References:
//   - SPEC.md, section "Authentication Layer"
//   - https://pkg.go.dev/crypto/subtle (constant-time comparison)
package auth
```

### 6.4 Comentário de decisão localizada

Quando uma escolha não óbvia é feita inline (ex.: tamanho de buffer de canal, ordem de middleware, valor de timeout), comentar com **justificativa**:

```go
// Buffer size 10000: each request emits 1 usage event; assuming peak of 1000 req/s
// and worker capable of >100 inserts/s, 10s of burst before backpressure kicks in.
// See ADR-005.
events := make(chan UsageEvent, 10000)
```

### 6.5 README.md no root
Manter atualizado com:
- Pré-requisitos (Go 1.24, Docker, PostgreSQL)
- Como subir o ambiente (`docker compose up -d postgres && go run ./cmd/gateway`)
- Como rodar testes
- Como rodar migrations manualmente
- Variáveis de ambiente necessárias
- Onde estão as docs (SPEC.md, CLAUDE.md, docs/adrs/)

---

## 7. Sistema de ADRs (Architecture Decision Records)

### 7.1 Quando criar ADR
**Obrigatório** criar ADR quando:
- Escolher entre bibliotecas competidoras
- Decidir trade-off de design não óbvio (sincronia vs assincronia, in-memory vs DB, single vs multi-instance)
- Adotar pattern arquitetural (CQRS, mediator, etc. — embora improvável neste projeto)
- Definir contratos públicos (API, schema DB)
- Configurar parâmetros operacionais com impacto (timeouts, buffer sizes, max conns)
- Decidir desvio da SPEC.md

**Não** criar ADR para:
- Rename de variável
- Mudança de layout interno de função
- Correção de bug óbvio

### 7.2 Localização
- Pasta: `docs/adrs/`
- Arquivo: `NNNN-titulo-curto-em-kebab-case.md` (ex.: `0001-bearer-token-vs-mtls.md`)
- Numeração: sequencial, três dígitos, começando em `0001`.

### 7.3 Template obrigatório

```markdown
# ADR-NNNN: <título>

- **Status**: proposed | accepted | rejected | superseded by ADR-XXXX | deprecated
- **Date**: YYYY-MM-DD
- **Decision makers**: <quem aprovou>
- **Consulted**: <quem foi consultado, opcional>

## Context

<2-5 parágrafos. O que motivou a decisão? Qual o problema? Quais restrições?>

## Decision

<O que foi decidido, em uma frase clara, seguido de detalhamento.>

## Options considered

### Option 1: <nome>
- Pros: ...
- Cons: ...

### Option 2: <nome>
- Pros: ...
- Cons: ...

### Option 3 (chosen): <nome>
- Pros: ...
- Cons: ...
- Why: ...

## Consequences

### Positive
- ...

### Negative / Trade-offs
- ...

### Mitigations
- ...

## References

- <URL doc oficial>
- <link interno: SPEC.md#seção>
- <PRs ou commits relacionados, se aplicável>
```

### 7.4 ADRs já implícitas no SPEC.md (criar agora se ainda não existem)

O Claude Code **deve** materializar estes ADRs como `docs/adrs/0001-*.md` ... `docs/adrs/0008-*.md`, **na primeira oportunidade**, baseados no que está implícito em SPEC.md e nas decisões já tomadas:

1. **ADR-0001**: Por que Go (e não LiteLLM como núcleo).
2. **ADR-0002**: Apps/keys/policies em YAML config (Fase 1) vs. tabelas Postgres (Fase 2+).
3. **ADR-0003**: `slog` como logger oficial (vs. `zap`, `logrus`).
4. **ADR-0004**: `pgx` direto (vs. `database/sql` + driver, vs. ORM).
5. **ADR-0005**: Usage/audit assíncrono via channel (buffer 10000) vs. síncrono no caminho crítico.
6. **ADR-0006**: Rate limit in-memory (Fase 1) vs. Redis (Fase 2+).
7. **ADR-0007**: `fail-closed` em Tier 3 vs. `fail-open` em Tier 1/2 para guardrails externos.
8. **ADR-0008**: `WriteTimeout: 0` no `http.Server` para suportar streaming SSE longo.

Cada ADR consolidando o que SPEC.md decide, com seção "Options considered" e "Consequences" preenchidas. Não inventar pros/cons — basear no que está documentado em SPEC.md ou em documentação oficial. Se faltar informação, marcar `<a confirmar com humano>` e perguntar.

---

## 8. Convenções de logging

### 8.1 Logger
- `slog.Logger` injetado via parâmetro de função/método. **Não usar logger global** (com a exceção do bootstrap em `main.go`).
- Sempre log estruturado, formato JSON em produção. Texto em desenvolvimento (controlado por env).

### 8.2 Campos obrigatórios em todo log de request lifecycle
- `request_id` (UUID v7, injetado pelo middleware)
- `application_name` (após auth)
- `tier` (após auth)
- `model` (após handler parsear body)
- `latency_ms` (no log de finalização)
- `status_code` (no log de finalização)
- `event_type` (semântico: `request_started`, `request_completed`, `auth_failed`, `model_blocked`, `pii_masked`, `prompt_shield_blocked`, `rate_limited`, `budget_exceeded`, `provider_error`)

### 8.3 Severidade
- `debug`: apenas em dev. Não logar em prod.
- `info`: ciclo de vida normal (start, completed, configured).
- `warn`: situação anômala mas não falha (rate limit hit, channel quase cheio, fail-open).
- `error`: falha verdadeira (provider error, DB error, panic recovered).

### 8.4 Anti-padrões de log
- ❌ `logger.Info("user prompt: %s", prompt)` — vaza prompt
- ❌ `logger.Error(err.Error())` — perde stack/wrapping
- ✅ `logger.Error("provider call failed", "err", err, "request_id", rid, "model", model)`

---

## 9. Convenções de Postgres / SQL

### 9.1 Queries
- **Sempre** parameterizadas (`$1, $2, ...`). Nunca `fmt.Sprintf` em SQL.
- Multi-linha com backticks Go, indentação clara.
- Comentário acima de cada query não trivial explicando intenção.

### 9.2 Migrations
- `golang-migrate` é o oficial.
- Arquivos: `NNN_descricao.up.sql` + `NNN_descricao.down.sql`. Numeração sequencial.
- Toda migration `up` tem migration `down` correspondente que reverte exatamente. Não criar `up` sem `down`.
- Migrations rodam **no boot** da aplicação (idempotente via `ErrNoChange`).

### 9.3 Naming
- Tabelas: `snake_case`, plural (`usage_events`, `audit_events`).
- Colunas: `snake_case` (`application_name`, `created_at`).
- Índices: `idx_<tabela>_<colunas>` (`idx_usage_app_created`).
- PKs: `id BIGSERIAL`.
- Timestamps: `TIMESTAMPTZ` (com fuso), default `NOW()`.

---

## 10. Configuração e segredos

### 10.1 Carregamento
- Configurações **estruturais** (apps, models, tiers, timeouts): `configs/gateway.yaml`.
- **Segredos** (API keys, DB passwords): variáveis de ambiente, **referenciadas** no YAML como `${VAR_NAME}`.
- Expansão de variáveis: `os.ExpandEnv` ou regex equivalente, no momento do load.

### 10.2 Validação
- Toda config tem `Validate() error` que falha rápido no boot:
  - Endpoint URL não vazio
  - API key não vazia (depois da expansão)
  - Pelo menos 1 app
  - Cada app tem `key_prefix`, `key_hash`, `tier`, `allowed_models`
  - Cada tier ∈ {`tier_1`, `tier_2`, `tier_3`}

### 10.3 `.env`
- Há `.env.example` no repo. **Nunca** commitar `.env` real.
- `.env` está em `.gitignore`.

---

## 11. Tratamento de panic

- `main.go` instala recoverer global no router (middleware do `chi`).
- Recover: loga `event_type=panic_recovered`, `severity=error`, com stack trace (`debug.Stack()`), retorna `500 Internal Server Error` ao cliente sem expor detalhes.

---

## 12. Documentação oficial autorizada para consulta via WebFetch

Estas URLs são fontes de verdade. Em caso de dúvida sobre API, consultar **antes** de implementar.

### Go
- https://pkg.go.dev/std (standard library)
- https://go.dev/doc/effective_go
- https://go.dev/ref/spec

### Libs Go
- https://github.com/go-chi/chi (router)
- https://pkg.go.dev/github.com/go-chi/chi/v5
- https://pkg.go.dev/github.com/jackc/pgx/v5
- https://github.com/jackc/pgx/wiki
- https://github.com/golang-migrate/migrate/blob/master/MIGRATIONS.md
- https://pkg.go.dev/golang.org/x/time/rate
- https://pkg.go.dev/gopkg.in/yaml.v3
- https://pkg.go.dev/log/slog

### Azure OpenAI
- https://learn.microsoft.com/en-us/azure/ai-services/openai/reference
- https://learn.microsoft.com/en-us/azure/ai-services/openai/how-to/chatgpt
- https://learn.microsoft.com/en-us/azure/ai-services/openai/api-version-deprecation

### Azure Content Safety (se aplicável)
- https://learn.microsoft.com/en-us/azure/ai-services/content-safety/quickstart-prompt-shield
- https://learn.microsoft.com/en-us/azure/ai-services/content-safety/quickstart-text
- https://learn.microsoft.com/en-us/rest/api/contentsafety/

### OpenAI (referência de contratos compatíveis)
- https://platform.openai.com/docs/api-reference/chat
- https://platform.openai.com/docs/api-reference/streaming

### PostgreSQL
- https://www.postgresql.org/docs/17/

### Docker
- https://docs.docker.com/engine/reference/builder/

---

## 13. Anti-padrões explícitos (NUNCA fazer)

| ❌ Antipadrão | ✅ Alternativa correta |
|---|---|
| `_ = err` ou `_, _ := func()` | Tratar erro: wrappar e propagar |
| Logger global (`log.Println`) | Injetar `*slog.Logger` |
| `panic("..." )` em runtime | Retornar erro |
| String concat em SQL | Parâmetros `$1, $2` |
| `time.Sleep` em produção | `select` com `ctx.Done()` + `time.After` |
| `bytes.Equal` em hashes | `subtle.ConstantTimeCompare` |
| `latest` em Docker | Versão pinada |
| `init()` com lógica de negócio | Composição explícita em `main.go` |
| Goroutine sem `ctx` ou shutdown signal | Goroutine recebe `ctx` ou `<-chan struct{}` |
| TODO sem issue/ADR | Implementar agora ou criar ADR |
| Comentário sem "porquê" | Comentário explica reasoning + referência |
| `os.Getenv` espalhado pelo código | Centralizar no pacote `config`, validar no boot |
| Inventar campo JSON sem confirmar com doc oficial | `WebFetch` doc oficial antes |
| Modificar SPEC.md sem permissão | Propor mudança via ADR |

---

## 14. Política de testes (Fase 1 — demo)

- **Não obrigatório** escrever testes na fase de demo.
- **Obrigatório** que código seja **testável**: dependências injetadas, interfaces para externos (provider, DB, content safety client).
- Quando o humano pedir testes: estilo table-driven, `testing` standard, sem framework externo.

---

## 15. Política de pull requests / commits (quando o humano pedir)

- Mensagens em inglês, formato Conventional Commits:
  - `feat(auth): add bearer token middleware`
  - `fix(streaming): correct SSE chunk parsing`
  - `docs(adr): add ADR-0003 for slog choice`
  - `chore(deps): pin pgx to v5.7.0`
- Um commit por escopo lógico.
- Mensagem de commit descreve **o quê** e **por quê**, não o como.

---

## 16. Comandos úteis do projeto

```bash
# Sobe Postgres
docker compose up -d postgres

# Roda app local
go run ./cmd/gateway

# Tidy deps
go mod tidy

# Static check
go vet ./...

# Build
go build ./cmd/gateway

# Build binário
CGO_ENABLED=0 GOOS=linux go build -o bin/ai-gateway ./cmd/gateway

# Build container
docker build -t ai-gateway:dev .

# Migrations manuais (se necessário)
migrate -database "postgres://gateway:gateway@localhost:5432/gateway?sslmode=disable" -path migrations up
migrate -database "..." -path migrations down 1

# Geração de hash de bearer token (para preencher key_hash no YAML)
echo -n "gwk_xxxx_realkey_yyyy" | sha256sum | cut -d' ' -f1
```

---

## 17. Quando o Claude Code DEVE parar e perguntar

- A spec é ambígua ou contraditória.
- Uma decisão exige criar ADR e não está claro qual opção escolher.
- O humano pediu algo que conflita com regra desta CLAUDE.md.
- Encontrou um erro/bug que sugere problema maior do que o pedido aparente.
- Vai precisar de mais de 3 arquivos novos para a tarefa.
- Vai precisar de dependência externa nova.
- O comportamento de uma lib não está claro mesmo após consultar doc oficial.

Não invente. Não deduza. Pergunte.

---

## 18. Resumo executivo de comportamento esperado

1. **Leia** SPEC.md e CLAUDE.md antes de tudo.
2. **Audite** o estado atual (Seção 2.3) e reporte antes de mexer.
3. **Documente** cada função, decisão e ADR. Documentação é entregável.
4. **Confirme** com doc oficial antes de usar API que você não tem certeza.
5. **Pergunte** quando há dúvida. Nunca invente.
6. **Pinne** todas as versões.
7. **Trate** todos os erros.
8. **Logue** sem vazar dado sensível.
9. **Crie** ADR para toda decisão não trivial.
10. **Reporte** ao humano após cada bloco, com diff e justificativa.

Fim do CLAUDE.md.
