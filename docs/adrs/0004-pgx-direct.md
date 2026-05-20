# ADR-0004: pgx Directly (vs. database/sql + driver, vs. ORM)

- **Status**: accepted
- **Date**: 2026-05-19
- **Decision makers**: Danirek

## Context

The gateway persists usage events, audit events, and budget counters to PostgreSQL. An approach for database access was needed.

## Decision

Use `github.com/jackc/pgx/v5` directly for all database access. No ORM, no `database/sql` adapter.

## Options considered

### Option 1: database/sql + lib/pq
- Pros: standard interface; easy migration to other drivers.
- Cons: `lib/pq` is maintenance-only; `database/sql` adds abstraction overhead; no native `JSONB`, `TIMESTAMPTZ`, or pgx-specific features.

### Option 2: GORM or Ent (ORM)
- Pros: less SQL to write; schema migrations via code.
- Cons: heavy dependency; hides SQL (harder to audit for correctness); performance overhead; not aligned with project philosophy.

### Option 3 (chosen): pgx v5 directly
- Pros: first-class PostgreSQL support; native `JSONB`, `TIMESTAMPTZ`; pgxpool for connection pooling; parameterized queries prevent SQL injection by default; smallest dependency surface.
- Cons: must write SQL manually; no query builder.
- Why: the project has simple, well-defined queries. SQL is preferable to ORM magic for auditability and correctness. pgx v5 is the de facto standard for production PostgreSQL in Go.

## Consequences

### Positive
- SQL queries are explicit and auditable.
- No risk of ORM generating unexpected queries.
- pgxpool handles connection management efficiently.

### Negative / Trade-offs
- SQL boilerplate for each query.

### Mitigations
- Query count is small (3 tables, ~4 queries total).

## References
- https://github.com/jackc/pgx
- https://pkg.go.dev/github.com/jackc/pgx/v5/pgxpool
- CLAUDE.md §4.5 — prohibited libraries
