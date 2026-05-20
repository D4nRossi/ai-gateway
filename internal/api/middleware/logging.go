package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/D4nRossi/ai-gateway/internal/observability"
)

// responseRecorder wraps http.ResponseWriter to capture the status code written
// by downstream handlers. It also propagates Flush() so SSE streaming works.
type responseRecorder struct {
	http.ResponseWriter
	status  int
	written bool
}

func (rr *responseRecorder) WriteHeader(status int) {
	if !rr.written {
		rr.status = status
		rr.written = true
		rr.ResponseWriter.WriteHeader(status)
	}
}

func (rr *responseRecorder) Write(b []byte) (int, error) {
	if !rr.written {
		rr.WriteHeader(http.StatusOK)
	}
	return rr.ResponseWriter.Write(b)
}

// Flush propagates to the underlying ResponseWriter when it supports flushing.
// Required so SSE streaming through the logging middleware continues to work.
func (rr *responseRecorder) Flush() {
	if f, ok := rr.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Logging returns a middleware that logs the start and end of each request.
// It injects a request-scoped logger (pre-loaded with request_id) into the
// context so downstream handlers can retrieve it via observability.LoggerFrom.
//
// References:
//   - SPEC.md §13.1 — logging fields and event types
//   - CLAUDE.md §8 — logging conventions
func Logging(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Build a request-scoped logger with request_id already attached.
			reqLogger := observability.WithRequestID(r.Context(), logger)

			reqLogger.Info("request started",
				"method", r.Method,
				"path", r.URL.Path,
				"remote_addr", r.RemoteAddr,
				"event_type", "request_started",
			)

			// Inject scoped logger into context for handler use.
			ctx := context.WithValue(r.Context(), observability.LoggerKey, reqLogger)

			rr := &responseRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rr, r.WithContext(ctx))

			reqLogger.Info("request completed",
				"status_code", rr.status,
				"latency_ms", time.Since(start).Milliseconds(),
				"event_type", "request_completed",
			)
		})
	}
}
