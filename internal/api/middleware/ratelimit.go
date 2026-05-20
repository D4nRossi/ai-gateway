package middleware

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/D4nRossi/ai-gateway/internal/audit"
	"github.com/D4nRossi/ai-gateway/internal/observability"
	"github.com/D4nRossi/ai-gateway/internal/ratelimit"
)

// RateLimit returns a middleware that enforces per-application RPM limits.
// On denial it returns 429 with a Retry-After header and emits an audit event.
//
// References:
//   - SPEC.md §9.1 step 5 — rate limit check
//   - SPEC.md §12.1 — rate limit specification
func RateLimit(mgr ratelimit.Limiter, auditWriter audit.Emitter, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			rid, _ := ctx.Value(observability.RequestIDKey).(string)

			policy, ok := PolicyFrom(ctx)
			if !ok {
				// Auth middleware must run before RateLimit.
				next.ServeHTTP(w, r)
				return
			}

			if !mgr.Allow(policy.Name) {
				reqLogger := observability.LoggerFrom(ctx, logger)
				reqLogger.Warn("rate limited",
					"application_name", policy.Name,
					"event_type", audit.EventRateLimited,
				)
				if auditWriter != nil {
					auditWriter.Emit(audit.AuditEvent{
						RequestID:       rid,
						ApplicationName: policy.Name,
						EventType:       audit.EventRateLimited,
						Severity:        "warn",
						CreatedAt:       time.Now().UTC(),
					})
				}
				w.Header().Set("Retry-After", "1")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error": map[string]string{
						"message": "rate_limited",
						"type":    "rate_limit_error",
					},
				})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
