// Package admin assembles the Admin API chi sub-router with its own middleware chain
// and route table.
//
// Mount point: /admin (configured in the top-level api.RouterDeps)
// All routes below /admin/v1 require a valid admin session token.
// Role requirements per route are documented inline.
//
// Route table:
//
//	POST   /admin/v1/auth/login                                  (public)
//	DELETE /admin/v1/auth/logout                                 (any role)
//
//	GET    /admin/v1/users                                       (admin)
//	POST   /admin/v1/users                                       (admin)
//	DELETE /admin/v1/users/{id}                                  (admin)
//
//	GET    /admin/v1/applications                                (operator+)
//	POST   /admin/v1/applications                                (operator+)
//	GET    /admin/v1/applications/{id}                           (operator+)
//	PUT    /admin/v1/applications/{id}                           (operator+)
//	DELETE /admin/v1/applications/{id}                           (operator+)
//	POST   /admin/v1/applications/{id}/rotate-key                (operator+)
//	POST   /admin/v1/applications/{id}/grants/{endpointID}       (operator+)
//	DELETE /admin/v1/applications/{id}/grants/{endpointID}       (operator+)
//
//	GET    /admin/v1/endpoints                                   (operator+)
//	POST   /admin/v1/endpoints                                   (operator+)
//	GET    /admin/v1/endpoints/{id}                              (operator+)
//	PUT    /admin/v1/endpoints/{id}                              (operator+)
//	DELETE /admin/v1/endpoints/{id}                              (operator+)
//	POST   /admin/v1/endpoints/{id}/targets                      (operator+)
//	PUT    /admin/v1/endpoints/{id}/targets/{targetID}           (operator+)
//	DELETE /admin/v1/endpoints/{id}/targets/{targetID}           (operator+)
//
//	GET    /admin/v1/usage                                       (viewer+)
//	GET    /admin/v1/audit                                       (viewer+)
//	GET    /admin/v1/budget                                      (viewer+)
//
// References:
//   - ADR-0009 — DB-backed admin plane
//   - ADR-0011 — opaque session token authentication
//   - docs/v2-alignment.md — role definitions
package admin

import (
	"database/sql"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	adminhandlers "github.com/D4nRossi/ai-gateway/internal/api/admin/handlers"
	adminmw "github.com/D4nRossi/ai-gateway/internal/api/admin/middleware"
	"github.com/D4nRossi/ai-gateway/internal/app/adminservice"
	"github.com/D4nRossi/ai-gateway/internal/domain/admin"
)

// Deps groups the dependencies needed to assemble the Admin API sub-router.
type Deps struct {
	Svc    *adminservice.Service
	DB     *sql.DB // ADR-0022 — substitui *pgxpool.Pool
	Logger *slog.Logger
}

// NewRouter builds and returns the Admin API chi sub-router.
// It is designed to be mounted at /admin in the top-level router.
//
// References:
//   - ADR-0009, ADR-0011 — route and auth design
func NewRouter(deps Deps) http.Handler {
	r := chi.NewRouter()

	// ── Public: login (rate-limited per IP) ──────────────────────────────────
	// Brute-force defense: 5 attempts per minute per IP (token bucket).
	// Failed AND successful attempts both consume tokens — leaking that
	// distinction would enable an enumeration oracle.
	loginLimiter := adminmw.NewLoginLimiter(0, 0, 0) // safe defaults
	r.With(loginLimiter.Middleware()).
		Post("/v1/auth/login", adminhandlers.Login(deps.Svc))

	// ── Protected: all remaining routes require a valid session ───────────────
	r.Group(func(r chi.Router) {
		r.Use(adminmw.SessionAuth(deps.Svc, deps.Logger))

		// Logout: any authenticated role may revoke their own session.
		r.Delete("/v1/auth/logout", adminhandlers.Logout(deps.Svc))

		// User management: admin only.
		r.Group(func(r chi.Router) {
			r.Use(adminmw.RequireRole(admin.RoleAdmin))
			r.Get("/v1/users", adminhandlers.ListUsers(deps.Svc))
			r.Post("/v1/users", adminhandlers.CreateUser(deps.Svc))
			r.Delete("/v1/users/{id}", adminhandlers.DeactivateUser(deps.Svc))
		})

		// Application management: operator or higher.
		r.Group(func(r chi.Router) {
			r.Use(adminmw.RequireRole(admin.RoleOperator))
			r.Get("/v1/applications", adminhandlers.ListApplications(deps.Svc))
			r.Post("/v1/applications", adminhandlers.CreateApplication(deps.Svc))
			r.Get("/v1/applications/{id}", adminhandlers.GetApplication(deps.Svc))
			r.Put("/v1/applications/{id}", adminhandlers.UpdateApplication(deps.Svc))
			r.Delete("/v1/applications/{id}", adminhandlers.DeleteApplication(deps.Svc))
			r.Post("/v1/applications/{id}/rotate-key", adminhandlers.RotateAPIKey(deps.Svc))
			r.Get("/v1/applications/{id}/grants", adminhandlers.ListGrants(deps.Svc))
			r.Post("/v1/applications/{id}/grants/{endpointID}", adminhandlers.GrantEndpointAccess(deps.Svc))
			r.Delete("/v1/applications/{id}/grants/{endpointID}", adminhandlers.RevokeEndpointAccess(deps.Svc))
		})

		// Endpoint management: operator or higher.
		r.Group(func(r chi.Router) {
			r.Use(adminmw.RequireRole(admin.RoleOperator))
			r.Get("/v1/endpoints", adminhandlers.ListEndpoints(deps.Svc))
			r.Post("/v1/endpoints", adminhandlers.CreateEndpoint(deps.Svc))
			r.Get("/v1/endpoints/{id}", adminhandlers.GetEndpoint(deps.Svc))
			r.Put("/v1/endpoints/{id}", adminhandlers.UpdateEndpoint(deps.Svc))
			r.Delete("/v1/endpoints/{id}", adminhandlers.DeleteEndpoint(deps.Svc))
			r.Post("/v1/endpoints/{id}/targets", adminhandlers.AddTarget(deps.Svc))
			r.Put("/v1/endpoints/{id}/targets/{targetID}", adminhandlers.UpdateTarget(deps.Svc))
			r.Post("/v1/endpoints/{id}/targets/{targetID}/migrate-to-kv", adminhandlers.MigrateTargetToKV(deps.Svc))
			r.Delete("/v1/endpoints/{id}/targets/{targetID}", adminhandlers.RemoveTarget(deps.Svc))
		})

		// Observability: read-only, viewer or higher.
		r.Group(func(r chi.Router) {
			r.Use(adminmw.RequireRole(admin.RoleViewer))
			r.Get("/v1/usage", adminhandlers.ListUsageEvents(deps.DB))
			r.Get("/v1/audit", adminhandlers.ListAuditEvents(deps.DB))
			r.Get("/v1/budget", adminhandlers.ListBudget(deps.DB))
		})
	})

	return r
}
