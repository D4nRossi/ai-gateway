# ADR-0025: MIGRATIONS_AUTO_APPLY — auto-apply em dev/homolog vs assertion em prod

- **Status**: accepted
- **Date**: 2026-05-28
- **Implementation date**: 2026-05-28
- **Decision makers**: Daniel (owner)
- **Consulted**: Claude Opus 4.7
- **Supersedes**: nenhum

## Context

O gateway aplica migrations no boot via `migrate.Up()` (`internal/db/migrate.go`).
Funciona perfeitamente em dev, homolog e ambientes single-operator, mas conflita
com a operação corporativa típica:

- DBAs querem **janelas controladas** pra DDL (revisão, backup pré-mudança,
  rollback rehearsado).
- O gateway hoje **não consegue bootar** se a migration aplicada parcialmente
  travou no meio — `schema_migrations.dirty = 1` requer cleanup manual antes
  do próximo boot.
- Em deploys com múltiplas réplicas (futuro), N processos rodando `Up()` em
  paralelo causaria contenção e prováveis falhas de bootstrap.

Owner declarou em 2026-05-28: "em prod eu rodo local a migration". Significa
que pra produção:
1. Owner aplica `migrate up` numa janela controlada via cliente externo
2. Depois faz deploy do binário novo
3. Binário não deve tentar aplicar migrations de novo

## Decision

Adicionar a env var **`MIGRATIONS_AUTO_APPLY`** que controla o que o boot do
gateway faz com migrations:

| Valor | Comportamento |
|---|---|
| unset (default), `true`, `1`, `yes` | `migrate.Up()` — comportamento histórico |
| `false`, `0`, `no` | `AssertSchemaUpToDate()` — só verifica versão, NÃO altera schema |

`AssertSchemaUpToDate` compara `schema_migrations.version` (no banco) com a
maior versão presente em `migrations/*.up.sql` (no FS do binário) e retorna:

- `nil` — versões iguais, `dirty = 0`. Boot continua.
- `ErrSchemaOutOfDate` — DB < binário (operator esqueceu de aplicar migration nova)
- `ErrSchemaAhead` — DB > binário (rollback do binário sem alinhamento)
- `ErrSchemaDirty` — `dirty = 1` (migration anterior travou)

Em qualquer caso de erro, gateway aborta o boot com mensagem clara
indicando o comando exato pra corrigir.

## Options considered

### Option 1: Status quo (sempre `Up()`)

- **Pros:** zero código novo.
- **Cons:** incompatível com governance corporativa.

### Option 2: Sempre `AssertSchemaUpToDate`

- **Pros:** comportamento consistente.
- **Cons:** quebra dev — desenvolvedores teriam que rodar `migrate up` manualmente
  toda vez que pulam uma migration nova.

### Option 3: Toggle por env var (CHOSEN)

- **Pros:** mantém ergonomia de dev; habilita governance em prod sem fork.
- **Cons:** mais um knob — operador precisa lembrar de setar. Mitigação: o
  manual de deploy lista a env var como obrigatória.

## Consequences

### Positive

- Compatível com modelo de governance DBA-driven em prod
- Detecta automaticamente schema drift (DB versão N, binário versão M ≠ N)
- Detecta automaticamente estado dirty antes de tentar processar requests
- Zero impacto em dev (default mantém comportamento anterior)

### Negative / Trade-offs

- Mais uma env var no manual de deploy
- `LatestExpectedVersion` scan do FS no boot — custo ínfimo (~1ms pra 50 arquivos)

### Mitigations

- Mensagens de erro do `AssertSchemaUpToDate` incluem o comando exato:
  `migrate -database "..." -path migrations up`
- Manual de deploy (`docs/deploy/linux.md` + `docs/deploy/windows.md`)
  documenta a env var como **obrigatória=false** pra prod
- Log de boot diferencia os 2 modos: `migrations applied` vs
  `schema check passed (manual migration mode)` — operador identifica o modo
  ativo em rolling restarts

## Implementation sketch

```go
// internal/db/migrate.go
func LatestExpectedVersion(migrationsPath string) (uint, error) { /* scan FS */ }
func AssertSchemaUpToDate(connStr, migrationsPath string) error { /* compare */ }

var (
    ErrSchemaOutOfDate = errors.New("schema is out of date")
    ErrSchemaAhead     = errors.New("schema is ahead of the running binary")
    ErrSchemaDirty     = errors.New("schema_migrations.dirty = 1; previous migration failed")
)
```

```go
// cmd/gateway/main.go (§5 Migrations)
autoApply := os.Getenv("MIGRATIONS_AUTO_APPLY")
manualMode := strings.EqualFold(autoApply, "false") /* + "0", "no" */
if manualMode {
    db.AssertSchemaUpToDate(connStr, "migrations")
} else {
    db.RunMigrations(connStr, "migrations")
}
```

## References

- ADR-0022 — golang-migrate driver sqlserver
- `internal/db/migrate.go` — implementação
- `cmd/gateway/main.go` §5 — wiring
- `docs/deploy/linux.md` + `docs/deploy/windows.md` — operação em prod
- `docs/handoff.md §4.6` — discussão original (2026-05-27)
- https://github.com/golang-migrate/migrate/blob/master/MIGRATIONS.md
