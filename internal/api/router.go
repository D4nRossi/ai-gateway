// Package api assembles the chi router with the full middleware chain and handlers.
//
// Middleware chain (outermost → innermost, per SPEC §16 step 15):
//
//	Recover → RequestID → Logging → Auth → RateLimit → Handlers
//
// The Admin API is mounted at /admin and uses its own session-based authentication
// chain (ADR-0011). It does not share middleware with the main API.
//
// References:
//   - SPEC.md §6.1 — endpoint list
//   - SPEC.md §16 step 15 — router assembly
//   - ADR-0009 — DB-backed admin plane
package api

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/D4nRossi/ai-gateway/internal/api/handlers"
	"github.com/D4nRossi/ai-gateway/internal/api/middleware"
	"github.com/D4nRossi/ai-gateway/internal/audit"
	"github.com/D4nRossi/ai-gateway/internal/auth"
	"github.com/D4nRossi/ai-gateway/internal/config"
	"github.com/D4nRossi/ai-gateway/internal/ratelimit"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RouterDeps groups dependencies needed to assemble the router.
// Rate limiter and audit writer are expressed as interfaces to allow
// unit testing without live infrastructure (CLAUDE.md §14).
type RouterDeps struct {
	Config      *config.Config
	PolicyStore auth.PolicyStore
	RateLimiter ratelimit.Limiter
	AuditWriter audit.Emitter
	Pool        *pgxpool.Pool
	ChatDeps    handlers.ChatDeps
	Logger      *slog.Logger
	// AdminHandler is the fully assembled Admin API sub-router, mounted at /admin.
	// It is constructed in main.go and injected here to keep router.go free of
	// admin-specific dependencies (ADR-0015).
	AdminHandler http.Handler
	// ProxyAuth is the DB-backed Bearer-token middleware for the generic proxy
	// plane. Constructed in main.go from the proxy package.
	ProxyAuth func(http.Handler) http.Handler
	// ProxyHandler is the generic-proxy http.Handler mounted under /v1/proxy.
	// Constructed in main.go from the proxy package.
	ProxyHandler http.Handler
	// WebHandler serves the embedded admin SPA. Mounted at /ui.
	// Constructed in main.go from the web package (ADR-0014).
	WebHandler http.Handler
}

// NewRouter builds and returns the fully assembled chi router.
//
// References:
//   - SPEC.md §6.1 — endpoint list
//   - SPEC.md §16 step 15 — middleware chain order
func NewRouter(deps RouterDeps) *chi.Mux {
	r := chi.NewRouter()

	// Outermost layer: panic recovery.
	r.Use(middleware.Recover(deps.Logger))

	r.Use(middleware.RequestID)
	r.Use(middleware.Logging(deps.Logger))

	// ── Public endpoints (no auth required) ──────────────────────────────────
	r.Get("/healthz", handlers.Health())
	r.Get("/readyz", handlers.Ready(deps.Pool, deps.Config.AzureOpenAI.Endpoint))

	// ── Authenticated endpoints ───────────────────────────────────────────────
	r.Group(func(r chi.Router) {
		r.Use(middleware.Auth(deps.PolicyStore, deps.AuditWriter, deps.Logger))
		r.Use(middleware.RateLimit(deps.RateLimiter, deps.AuditWriter, deps.Logger))

		r.Get("/v1/models", handlers.Models(deps.Config))
		r.Post("/v1/chat/completions", handlers.Chat(deps.ChatDeps))
	})

	// ── Admin API ─────────────────────────────────────────────────────────────
	// The admin sub-router owns its own session-auth middleware chain (ADR-0011).
	// chi.Mount strips the /admin prefix before passing the request to the sub-router,
	// so the admin router registers routes under /v1/... not /admin/v1/....
	if deps.AdminHandler != nil {
		r.Mount("/admin", deps.AdminHandler)
	}

	// ── Generic HTTP proxy plane ──────────────────────────────────────────────
	// Mounted at /v1/proxy/{slug}, /v1/proxy/{slug}/* — accepts every HTTP method
	// so any upstream API can be proxied transparently. Uses the proxy package's
	// own DB-backed Bearer-token auth (ADR-0009, ADR-0010, ADR-0013).
	if deps.ProxyHandler != nil && deps.ProxyAuth != nil {
		r.Group(func(r chi.Router) {
			r.Use(deps.ProxyAuth)
			r.Handle("/v1/proxy/{slug}", deps.ProxyHandler)
			r.Handle("/v1/proxy/{slug}/*", deps.ProxyHandler)
		})
	}

	// ── Admin SPA ─────────────────────────────────────────────────────────────
	// chi.Mount strips the /ui prefix before invoking WebHandler, which serves
	// the embedded Vite build. Visiting / redirects to /ui/ so the landing page
	// is the admin console (ADR-0014).
	if deps.WebHandler != nil {
		r.Mount("/ui", deps.WebHandler)
		r.Get("/", func(w http.ResponseWriter, req *http.Request) {
			http.Redirect(w, req, "/ui/", http.StatusFound)
		})
	}

	return r
}
