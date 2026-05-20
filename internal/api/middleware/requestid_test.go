package middleware

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/D4nRossi/ai-gateway/internal/observability"
)

// uuidV7Re matches the UUID v7 string format (time-ordered, version=7).
// UUID v7 has version nibble = 7 and variant bits = 10 (binary).
// Format: xxxxxxxx-xxxx-7xxx-[89ab]xxx-xxxxxxxxxxxx
var uuidV7Re = regexp.MustCompile(
	`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`,
)

// TestRequestID_GeneratesUUIDv7 verifies that the middleware injects a UUID v7
// into the X-Request-Id header and the request context.
//
// References:
//   - CLAUDE.md §8.2 — UUID v7 required for request_id
func TestRequestID_GeneratesUUIDv7(t *testing.T) {
	t.Parallel()

	var capturedCtxID string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedCtxID, _ = r.Context().Value(observability.RequestIDKey).(string)
		w.WriteHeader(http.StatusOK)
	})

	handler := RequestID(inner)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	rid := rec.Header().Get("X-Request-Id")

	if rid == "" {
		t.Fatal("X-Request-Id header is empty")
	}
	if !uuidV7Re.MatchString(rid) {
		t.Errorf("X-Request-Id %q does not match UUID v7 pattern", rid)
	}
	if capturedCtxID != rid {
		t.Errorf("ctx request_id %q != header %q", capturedCtxID, rid)
	}
}

// TestRequestID_UniquePerRequest confirms that each invocation produces a distinct ID.
func TestRequestID_UniquePerRequest(t *testing.T) {
	t.Parallel()

	seen := make(map[string]bool)
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := RequestID(inner)

	const n = 20
	for range n {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		rid := rec.Header().Get("X-Request-Id")
		if seen[rid] {
			t.Errorf("duplicate request ID: %q", rid)
		}
		seen[rid] = true
	}
}
