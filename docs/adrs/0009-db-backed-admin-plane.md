# ADR-0009: DB-backed admin plane — applications and keys migrated from YAML

- **Status**: accepted
- **Date**: 2026-05-20
- **Decision makers**: Danirek (Software Architect, Digital Innovation — Teleperformance Brasil)

## Context

In Phase 1, all applications, API key hashes, and tier policies are stored in `configs/gateway.yaml`
(SPEC.md §4, §2.2). This model is intentional for the demo: fast to bootstrap, zero DB dependency
for policy reads, easy to audit via Git.

However, it creates hard blockers for V2:

1. **No live CRUD** — onboarding a new application requires a config edit, a CI pipeline run, and a
   service restart. For an operational gateway used by multiple teams, this is a bottleneck.
2. **No key rotation without downtime** — the gateway holds the YAML in memory; changing a key
   requires restart.
3. **No history** — there is no audit trail of who created an app, changed a tier, or rotated a key.
   YAML in Git gives history for deliberate commits but not for day-to-day operations.
4. **Admin API prerequisite** — V2's Admin API (CRUD for apps, keys, endpoint grants) can't write
   to a static file from a running process in a container environment.

## Decision

Move the authoritative source of applications, tiers, key hashes, and per-app limits to PostgreSQL
in V2. The new tables are `applications` and `api_keys` (migration 003). A new DB-backed
`ApplicationRepository` (in `internal/infra/postgres/applicationrepo.go`) replaces the YAML-backed
`PolicyStore` for the admin plane.

The `auth` middleware will be updated in a subsequent block to load policies from the DB-backed store
rather than `configs/gateway.yaml`. The YAML config retains only structural settings (server, azure,
database, logging, models) — per-application configuration moves fully to the DB.

## Options considered

### Option 1: Keep YAML (Phase 1 approach)
- Pros: zero infra change; all history in Git; restart guarantee of consistency
- Cons: no live CRUD; restart required for every change; can't build Admin API on top of it

### Option 2: DB-backed apps (chosen)
- Pros: live CRUD; key rotation without restart; Admin API feasible; row-level audit trail
- Cons: DB is now on the critical path for policy lookup (mitigated: policies cached in memory with TTL, DB used only on cache miss or admin write)
- Why: PostgreSQL is already a hard dependency (usage/audit/budget); adding app storage is zero new infra

### Option 3: Key-value store (Redis, etcd, Consul)
- Pros: low-latency reads; native TTL and pub/sub for live reload
- Cons: adds a new infrastructure component; no transactional guarantees; out of scope for OnPrem V2

## Consequences

### Positive
- Admin API can perform live CRUD on applications and API keys
- Key rotation: generate new key → store → invalidate old → zero-downtime swap
- Full change history via `audit_events` (who created, who rotated, when)
- Role-based access: admin vs operator vs viewer can control who may modify apps

### Negative / Trade-offs
- Migration path needed: Phase 1 YAML apps must be seeded into the DB on first V2 boot
  (handled by bootstrap: if `applications` table is empty, seed from YAML)
- DB read latency on policy lookup (mitigated by in-memory cache with short TTL)

### Mitigations
- Cache policies in memory (with 60s TTL by default); invalidate on admin write
- Phase 1 `main` branch and YAML-backed `PolicyStore` remain untouched

## References

- SPEC.md §2.2 — control plane (Phase 1)
- SPEC.md §18 — out of scope for Phase 1: "Admin API (CRUD for apps, keys, policies)"
- docs/v2-alignment.md response A (admin auth) and Bloco 1 plan
- ADR-0002 — original decision to use YAML config for Phase 1
