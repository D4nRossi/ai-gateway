package middleware

import (
	"context"
	"net/http"

	"github.com/D4nRossi/ai-gateway/internal/observability"
	"github.com/google/uuid"
)

// RequestID is a middleware that generates a UUID v7 per request, sets it as
// the X-Request-Id response header, and injects it into the request context
// under observability.RequestIDKey.
//
// UUID v7 provides monotonically increasing, time-ordered identifiers, which
// improve B-tree index locality when request_id is stored in audit/usage tables.
//
// References:
//   - SPEC.md §9.1 step 2
//   - CLAUDE.md §8.2 — request_id required in all request lifecycle logs; UUID v7 required
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rid, err := uuid.NewV7()
		if err != nil {
			// NewV7 reads from crypto/rand; failure is extremely rare but must not drop the request.
			rid = uuid.New()
		}
		ridStr := rid.String()
		w.Header().Set("X-Request-Id", ridStr)
		ctx := context.WithValue(r.Context(), observability.RequestIDKey, ridStr)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
