# ADR-0014: Frontend stack — React + Vite + shadcn/ui embedded in Go binary

- **Status**: accepted
- **Date**: 2026-05-20
- **Decision makers**: Danirek (Software Architect, Digital Innovation — Teleperformance Brasil)

## Context

V2 adds an admin web UI for:
- CRUD of applications (with show-once API key modal)
- CRUD of proxy endpoints and targets
- Usage dashboard (request counts, cost, latency charts)
- Audit log viewer with filters by app, event type, and date range
- Budget tracker per application

The UI must be deliverable as part of the existing single-binary deployment (Docker Compose,
no separate web server to manage). The primary developer is a backend Go engineer (no dedicated
frontend team), so the frontend stack must have low friction for initial setup and maintenance.

## Decision

**React + TypeScript + Vite + TailwindCSS + shadcn/ui**, embedded into the Go binary via
`//go:embed static` at build time.

**Build artifact flow:**
1. Frontend source code lives in `web/` at the repo root.
2. `pnpm build` produces static files in `web/dist/`.
3. At `go build` time, the Go embed directive copies `web/dist/` into `cmd/gateway/static/`.
4. The Go server serves the SPA at `/admin/` and the Admin REST API at `/admin/v1/`.
5. **Deployment: single binary + postgres + docker compose. No extra process.**

**Key library choices:**
- `shadcn/ui` — DataTable (TanStack Table), Form (Zod validation), Chart (Recharts) built-in;
  no separate chart library needed
- `@tanstack/react-query` v5 — server state, caching, refetch on focus
- `react-router-dom` v7 — SPA routing
- `@fontsource/inter` — typography loaded locally (no CDN; works offline OnPrem)
- `date-fns` — date formatting in log viewers

**Design system:**
- Dark-first (zinc-950 background); light mode available via toggle
- Accent: violet-500 — distinct from generic blue/gray admin templates
- No external CSS frameworks beyond Tailwind — all components from shadcn/ui primitives

## Options considered

### Option 1: htmx + Templ (Go-native, no Node.js)
- Pros: zero JS toolchain; single Go binary; no client-side JS framework
- Cons: insufficient for rich admin dashboards (charts, real-time updates, complex DataTable
  with server-side pagination/filter); htmx is excellent for simple CRUD but not for metrics UI

### Option 2: Vue 3 + Nuxt
- Pros: simpler than React; good DX
- Cons: smaller admin UI component ecosystem; no pre-built DataTable+Chart combo like shadcn

### Option 3: Angular
- Pros: TypeScript-native; strong for large enterprise apps
- Cons: heavy initial setup; more boilerplate; slower for a single-developer context; Angular
  Material less customizable than shadcn/ui for bespoke designs

### Option 4: React + Vite + shadcn/ui (chosen)
- Pros: shadcn/ui has DataTable, Form, and Chart ready; Vite is fast (~300ms HMR); every
  frontend developer knows React; `go:embed` keeps single-binary deployment
- Cons: Node.js toolchain required at build time (not at runtime)
- Why: best balance of capability, ecosystem, and delivery speed for a single developer

## Consequences

### Positive
- Admin UI ships as part of the existing binary — no extra Dockerfile or deployment step
- shadcn/ui components can be customized at the source level (no locked-in black-box library)
- React ecosystem gives access to all necessary chart, table, and form primitives

### Negative / Trade-offs
- `pnpm build` must run before `go build` in CI pipelines
- Node.js LTS must be installed on the build machine (not needed at runtime)
- Initial setup requires installing Node + pnpm (one-time, documented in README)

### Mitigations
- Makefile target `make build` will run `pnpm build && go build` in sequence
- `web/dist/` is in `.gitignore`; generated at build time, not committed

## References

- docs/v2-alignment.md — Frontend decision (confirmed 2026-05-20)
- https://ui.shadcn.com/docs/components/data-table
- https://tanstack.com/query/v5
- https://vitejs.dev/guide/
- https://pkg.go.dev/embed
