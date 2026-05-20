# ADR-0015: Restructure internal/ with domain / app / infra layers

- **Status**: accepted
- **Date**: 2026-05-20
- **Decision makers**: Danirek (Software Architect, Digital Innovation — Teleperformance Brasil)

## Context

Phase 1's `internal/` is a flat package layout optimized for a single concern: the AI gateway data
plane. With ~10 packages, this is readable and easy to navigate.

V2 introduces three significant new feature areas: Admin CRUD API, generic HTTP proxy engine, and
frontend serving. The total package count will grow to ~20+. Without a layering convention, the
dependency graph becomes opaque: a package may import DB drivers, domain types, and HTTP handlers
in arbitrary combinations, making it hard to test domain logic without standing up infrastructure.

The specific problems this ADR addresses:
1. **Testability**: domain rules (e.g., "a viewer-role admin cannot modify applications") should be
   testable without a database connection.
2. **Clarity of dependencies**: it should be immediately obvious which packages are "pure business
   logic" vs "infrastructure adapters".
3. **Extensibility**: adding a second storage backend (e.g., migrating from PostgreSQL to a
   different DB in the future) should only require adding a new implementation, not touching domain
   types.

## Decision

Add three new directories inside `internal/`:

```
internal/
├── domain/          # Pure Go types (entities + value objects + repository interfaces)
│   ├── application/ # Application, APIKey, TierLevel + ApplicationRepository interface
│   ├── endpoint/    # ProxyEndpoint, Target, LBStrategy, TargetAuth + EndpointRepository interface
│   └── admin/       # AdminUser, AdminSession, Role + AdminRepository interface
│
├── app/             # Application services (use cases) — depends on domain interfaces
│   ├── adminservice/ # CreateApp, RotateKey, ListUsage, ...
│   └── proxyservice/ # RouteRequest, SelectTarget, ApplyAuth
│
└── infra/           # Infrastructure implementations — depends on domain + external libs
    ├── postgres/    # pgx implementations of domain repository interfaces
    └── crypto/      # AES-256-GCM encrypter (ADR-0012)
```

**Import rule (enforced by convention, not tooling in V2):**
- `domain/*` packages import: only Go stdlib. No project-internal imports.
- `app/*` packages import: `domain/*`, Go stdlib, observability. No `infra/*`.
- `infra/*` packages import: `domain/*`, external libs (pgx, crypto), Go stdlib.
- `api/*` packages import: `app/*`, `domain/*`, `infra/*`, Go stdlib.

**Phase 1 packages are untouched:** `api/`, `auth/`, `audit/`, `budget/`, `config/`, `db/`,
`observability/`, `providers/`, `ratelimit/`, `security/`, `tiers/`, `usage/` remain as-is on
both `main` and `v2` branches until explicitly refactored (separate ADR required).

## Options considered

### Option 1: Keep flat internal/ layout
- Pros: no migration; consistent with Phase 1 style
- Cons: 20+ packages with no grouping convention; domain rules tangled with DB calls;
  testability requires mocking at the package level (fragile)

### Option 2: domain/app/infra layers (chosen)
- Pros: clear dependency direction; domain is testable in isolation; infra is swappable;
  standard Go pattern for projects of this scale
- Cons: more directories; slightly longer import paths
- Why: explicitly designed for the V2 feature set; repos and services are the primary new
  abstractions and they fit naturally into this model

### Option 3: Hexagonal / ports-and-adapters (strict)
- Pros: maximum separation; explicit port interfaces
- Cons: overkill for a project with a single team and single deployment model; too much
  boilerplate for the current size

## Consequences

### Positive
- Domain types (`Application`, `AdminUser`, `ProxyEndpoint`) are testable with zero mocking
- Repository interfaces in `domain/` allow swapping postgres for another backend without touching
  application service code
- Clear answer to "where does this code go?" for any new V2 feature

### Negative / Trade-offs
- Phase 1 packages (`auth/`, `audit/`, etc.) do not follow this convention yet — creates
  inconsistency within `internal/` that should be resolved in a future refactor (V3+)

### Mitigations
- Document the two zones in README: "Phase 1 packages (flat)" and "V2+ packages (layered)"
- Phase 1 packages will be migrated to domain/app/infra when they are next touched (no big-bang
  refactor)

## References

- docs/v2-alignment.md — response J (branch v2, evolutionary approach) and Bloco 1 structure
- https://go.dev/doc/effective_go — package guidelines
- "Layered Architecture" — Robert C. Martin, Clean Architecture §22
