package middleware

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/D4nRossi/ai-gateway/internal/audit"
	"github.com/D4nRossi/ai-gateway/internal/auth"
	"github.com/D4nRossi/ai-gateway/internal/observability"
)

type ctxKeyPolicy struct{}

// Auth returns a middleware that authenticates Bearer tokens against the PolicyStore.
// On success it injects the matched AppPolicy into the request context.
// On failure it returns 401 with a JSON error body and emits an audit event.
//
// Token validation steps (SPEC §9.1 step 4):
//  1. Extract Bearer token from Authorization header.
//  2. Confirm prefix starts with "gwk_".
//  3. Derive key_prefix and look up AppPolicy.
//  4. SHA-256(token) vs. policy.KeyHash via constant-time comparison.
//
// References:
//   - SPEC.md §9.1 step 4
//   - CLAUDE.md §5.6 — constant-time comparison requirement
//   - CLAUDE.md §1.4 — never log the full token
func Auth(store auth.PolicyStore, auditWriter audit.Emitter, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rid, _ := r.Context().Value(observability.RequestIDKey).(string)

			headerVal := r.Header.Get("Authorization")
			if !strings.HasPrefix(headerVal, "Bearer ") {
				writeAuthError(w, r, rid, "missing bearer token", auditWriter, logger)
				return
			}

			tok := strings.TrimPrefix(headerVal, "Bearer ")
			if tok == "" || !strings.HasPrefix(tok, "gwk_") {
				writeAuthError(w, r, rid, "invalid token format", auditWriter, logger)
				return
			}

			prefix := auth.ExtractPrefix(tok)
			p, ok := store.Lookup(prefix)
			if !ok {
				writeAuthError(w, r, rid, "unknown key prefix", auditWriter, logger)
				return
			}

			// Compute SHA-256 of the full token.
			sum := sha256.Sum256([]byte(tok))

			// Decode the stored hex hash into raw bytes for constant-time comparison.
			want, err := hex.DecodeString(p.KeyHash)
			if err != nil {
				// Stored hash is malformed — treat as auth failure, not 500.
				logger.Error("malformed key_hash in policy config",
					"application_name", p.Name,
					"request_id", rid,
				)
				writeAuthError(w, r, rid, "invalid credential configuration", auditWriter, logger)
				return
			}

			// constant-time compare: sum[:] is always 32 bytes; want must also be 32 bytes.
			// subtle.ConstantTimeCompare returns 1 only when slices are identical.
			if subtle.ConstantTimeCompare(sum[:], want) != 1 {
				writeAuthError(w, r, rid, "token mismatch", auditWriter, logger)
				return
			}

			ctx := context.WithValue(r.Context(), ctxKeyPolicy{}, p)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// PolicyFrom extracts the AppPolicy injected by the Auth middleware from ctx.
// Returns false if no policy is present (request never passed through Auth).
func PolicyFrom(ctx context.Context) (auth.AppPolicy, bool) {
	p, ok := ctx.Value(ctxKeyPolicy{}).(auth.AppPolicy)
	return p, ok
}

// WithPolicy returns a copy of ctx with p injected under the same key used
// by the Auth middleware. Intended for use in tests that exercise handlers
// directly, bypassing the full auth middleware chain.
func WithPolicy(ctx context.Context, p auth.AppPolicy) context.Context {
	return context.WithValue(ctx, ctxKeyPolicy{}, p)
}

// writeAuthError writes a 401 JSON error response and emits an audit event.
func writeAuthError(w http.ResponseWriter, r *http.Request, rid, reason string, auditWriter audit.Emitter, logger *slog.Logger) {
	logger.Info("auth failed",
		"request_id", rid,
		"reason", reason,
		"event_type", audit.EventAuthFailed,
	)
	if auditWriter != nil {
		auditWriter.Emit(audit.AuditEvent{
			RequestID:       rid,
			ApplicationName: "unknown",
			EventType:       audit.EventAuthFailed,
			Severity:        "warn",
			Metadata:        map[string]any{"reason": reason},
			CreatedAt:       time.Now().UTC(),
		})
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{
			"message": "unauthorized",
			"type":    "auth_error",
		},
	})
}

