// Package api assembles the chi router with the full middleware chain and handlers.
//
// Middleware chain (outermost → innermost, per SPEC §16 step 15):
//
//	Recover → RequestID → Logging → Auth → RateLimit → Handlers
//
// References:
//   - SPEC.md §6.1 — endpoint list
//   - SPEC.md §16 step 15 — router assembly
package api

import (
	"log/slog"

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
type RouterDeps struct {
	Config       *config.Config
	PolicyStore  auth.PolicyStore
	RateLimiter  *ratelimit.Manager
	AuditWriter  *audit.Writer
	Pool         *pgxpool.Pool
	ChatDeps     handlers.ChatDeps
	Logger       *slog.Logger
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

	return r
}
