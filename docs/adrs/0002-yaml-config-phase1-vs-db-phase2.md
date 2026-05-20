# ADR-0002: YAML Config for Apps/Keys/Policies (Phase 1) vs. DB-backed (Phase 2)

- **Status**: accepted
- **Date**: 2026-05-19
- **Decision makers**: Danirek

## Context

The gateway needs to store application identities, bearer token hashes, allowed models, tier assignments, and rate/budget limits. Two storage approaches were considered for Phase 1.

## Decision

Store applications, keys, and policies in `configs/gateway.yaml` for Phase 1. Migrate to PostgreSQL-backed storage in Phase 2 when an Admin API is implemented.

## Options considered

### Option 1: DB-backed from day 1
- Pros: supports runtime changes without restart; enables Admin API.
- Cons: requires Admin API for safe mutation (out of scope for Phase 1 demo); adds DB dependency to the auth hot-path.

### Option 2 (chosen): YAML config (Phase 1) → DB (Phase 2)
- Pros: zero extra DB queries on the auth hot-path; simple to reason about during demo; file is auditable via git history.
- Cons: requires restart to add/remove applications; no runtime mutation.
- Why: the Phase 1 goal is a functional demo, not a production-grade admin system. The `PolicyStore` interface ensures no call sites need to change when DB storage is added.

## Consequences

### Positive
- Auth hot-path is allocation-free (immutable in-memory map after startup).
- No DB dependency for auth; DB failure does not break authentication.

### Negative / Trade-offs
- Adding an application requires editing YAML and restarting the gateway.
- Key rotation requires restart.

### Mitigations
- `PolicyStore` interface abstracts the storage backend; Phase 2 implementation swaps the concrete type.

## References
- SPEC.md §2.2 — control plane (Phase 1)
- SPEC.md §5.2 — PolicyStore interface
- ADR-0001
