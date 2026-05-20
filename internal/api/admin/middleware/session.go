// Package middleware provides HTTP middleware for the Admin API.
//
// SessionAuth authenticates requests using opaque Bearer session tokens (ADR-0011).
// RequireRole enforces role-based access control using the role hierarchy defined
// in docs/v2-alignment.md: viewer(0) < operator(1) < admin(2).
//
// References:
//   - ADR-0011 — opaque session token authentication
//   - docs/v2-alignment.md — role hierarchy
package middleware

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/D4nRossi/ai-gateway/internal/app/adminservice"
	"github.com/D4nRossi/ai-gateway/internal/domain/admin"
)

// contextKey is an unexported type for admin-middleware context keys.
// Using a typed key prevents collisions with keys from other packages.
type contextKey int

const (
	sessionKey contextKey = iota
	userKey
)

// SessionFromCtx retrieves the AdminSession injected by SessionAuth.
// Returns the zero value and false when called outside a SessionAuth-protected handler.
func SessionFromCtx(ctx context.Context) (admin.AdminSession, bool) {
	s, ok := ctx.Value(sessionKey).(admin.AdminSession)
	return s, ok
}

// UserFromCtx retrieves the AdminUser injected by SessionAuth.
// Returns the zero value and false when called outside a SessionAuth-protected handler.
func UserFromCtx(ctx context.Context) (admin.AdminUser, bool) {
	u, ok := ctx.Value(userKey).(admin.AdminUser)
	return u, ok
}

// SessionAuth validates the Bearer token in the Authorization header against active
// admin sessions. On success it injects the matched session and user into the context.
//
// Returns 401 when the header is absent, malformed, or the token is invalid/expired.
//
// Reasoning: admin tokens are 32-byte random values; the middleware hashes the raw
// token (SHA-256) before the DB lookup so the raw secret never touches persistence
// layers (ADR-0011).
//
// References:
//   - ADR-0011 — opaque session token, SHA-256 hash stored
func SessionAuth(svc *adminservice.Service, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := extractBearer(r)
			if raw == "" {
				writeUnauthorized(w, "missing or invalid Authorization header")
				return
			}

			session, user, err := svc.ValidateSession(r.Context(), raw)
			if err != nil {
				if errors.Is(err, admin.ErrNotFound) {
					writeUnauthorized(w, "invalid or expired session token")
					return
				}
				logger.Error("admin session validation error", "err", err)
				writeUnauthorized(w, "session validation error")
				return
			}

			ctx := context.WithValue(r.Context(), sessionKey, session)
			ctx = context.WithValue(ctx, userKey, user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// extractBearer returns the token value from an "Authorization: Bearer <token>" header,
// or an empty string when the header is absent or has a different scheme.
func extractBearer(r *http.Request) string {
	hdr := r.Header.Get("Authorization")
	after, found := strings.CutPrefix(hdr, "Bearer ")
	if !found || after == "" {
		return ""
	}
	return after
}

// writeUnauthorized writes a 401 JSON error without importing the handlers package
// (which would create a circular dependency).
func writeUnauthorized(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":{"code":"unauthorized","message":"` + jsonSafe(msg) + `"}}`))
}

// jsonSafe escapes the handful of characters that would break the inline JSON literal.
// msg is always a package-level constant so this is defensive, not strictly necessary.
func jsonSafe(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}
