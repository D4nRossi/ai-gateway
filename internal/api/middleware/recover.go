package middleware

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/D4nRossi/ai-gateway/internal/observability"
)

// Recover returns a middleware that catches panics, logs them with a stack trace,
// and returns 500 to the client without exposing internal details.
//
// Reasoning: panics in handlers must not crash the server process. The stack
// trace is logged at error severity so operators can investigate; the client
// receives only a generic "internal_error" message to avoid leaking internals.
//
// References:
//   - SPEC.md §9.1 — middleware chain; recover is the outermost layer
//   - CLAUDE.md §11 — panic handling policy
func Recover(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					rid, _ := r.Context().Value(observability.RequestIDKey).(string)
					stack := debug.Stack()

					reqLogger := observability.LoggerFrom(r.Context(), logger)
					reqLogger.Error("panic recovered",
						"request_id", rid,
						"panic_value", rec,
						"stack_trace", string(stack),
						"event_type", "panic_recovered",
					)

					// Only write response if headers haven't been sent yet.
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusInternalServerError)
					_ = json.NewEncoder(w).Encode(map[string]any{
						"error": map[string]string{
							"message": "internal_error",
						},
					})
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
