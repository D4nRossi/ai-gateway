package middleware

import (
	"net/http"
	"strings"

	"github.com/D4nRossi/ai-gateway/internal/domain/admin"
)

// roleRank maps each Role to a numeric level for ≥ comparisons.
// viewer(0) < operator(1) < admin(2). Any role not in the map has rank -1
// (unknown roles are always denied).
var roleRank = map[admin.Role]int{
	admin.RoleViewer:   0,
	admin.RoleOperator: 1,
	admin.RoleAdmin:    2,
}

// RequireRole returns a middleware that passes the request only when the authenticated
// user's role is at least as privileged as minimum.
//
// SessionAuth must run before RequireRole so that UserFromCtx returns a valid user.
// If the user is missing from the context, the request is rejected with 403.
//
// Reasoning: a numeric rank lets one middleware express "at least operator" without
// enumerating every valid role at each call site, making it easy to add new roles
// without changing every RequireRole call.
//
// References:
//   - docs/v2-alignment.md — role hierarchy: viewer < operator < admin
func RequireRole(minimum admin.Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, ok := UserFromCtx(r.Context())
			if !ok {
				writeForbidden(w, "authentication required")
				return
			}

			if roleRank[user.Role] < roleRank[minimum] {
				writeForbidden(w, "insufficient permissions for this operation")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// writeForbidden writes a 403 JSON error without importing the handlers package.
func writeForbidden(w http.ResponseWriter, msg string) {
	safe := strings.ReplaceAll(strings.ReplaceAll(msg, `\`, `\\`), `"`, `\"`)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_, _ = w.Write([]byte(`{"error":{"code":"forbidden","message":"` + safe + `"}}`))
}
