package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/D4nRossi/ai-gateway/internal/observability"
)

func recoverMiddleware() func(http.Handler) http.Handler {
	logger, _ := observability.New("error", "text")
	return Recover(logger)
}

// TestRecover_PanicsReturn500 verifies that a panicking handler returns 500
// instead of crashing the server.
func TestRecover_PanicsReturn500(t *testing.T) {
	t.Parallel()
	panicHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("simulated handler panic")
	})

	handler := recoverMiddleware()(panicHandler)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d; want 500", rec.Code)
	}
}

// TestRecover_ErrorBodyFormat verifies the 500 response body structure.
func TestRecover_ErrorBodyFormat(t *testing.T) {
	t.Parallel()
	panicHandler := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		panic("test panic")
	})

	handler := recoverMiddleware()(panicHandler)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var body struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Error.Message != "internal_error" {
		t.Errorf("error.message = %q; want %q", body.Error.Message, "internal_error")
	}
}

// TestRecover_PanicDoesNotLeakInternals verifies that internal panic details
// are not exposed to the client (stack traces, panic value, etc.).
func TestRecover_PanicDoesNotLeakInternals(t *testing.T) {
	t.Parallel()
	secret := "super-secret-internal-detail"
	panicHandler := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		panic(secret)
	})

	handler := recoverMiddleware()(panicHandler)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body := rec.Body.String()
	if contains(body, secret) {
		t.Errorf("response body leaks internal panic value %q", secret)
	}
}

// TestRecover_ContentType verifies the 500 response has JSON content type.
func TestRecover_ContentType(t *testing.T) {
	t.Parallel()
	panicHandler := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		panic("boom")
	})

	handler := recoverMiddleware()(panicHandler)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q; want %q", ct, "application/json")
	}
}

// TestRecover_NormalHandlerPassthrough verifies that non-panicking handlers
// are not affected by the recover middleware.
func TestRecover_NormalHandlerPassthrough(t *testing.T) {
	t.Parallel()
	normalHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	handler := recoverMiddleware()(normalHandler)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d; want 200", rec.Code)
	}
}

// TestRecover_PanicWithNilValue verifies nil panics are handled gracefully.
func TestRecover_PanicWithNilValue(t *testing.T) {
	t.Parallel()
	panicHandler := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		panic(nil)
	})

	handler := recoverMiddleware()(panicHandler)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	// Must not crash the test runner.
	handler.ServeHTTP(rec, req)
	// nil panic: recover() returns nil in Go 1.21+, so the handler may run normally.
	// Acceptable outcomes: 200 (nil panic not caught) or 500 (caught). Neither should crash.
	if rec.Code != http.StatusOK && rec.Code != http.StatusInternalServerError {
		t.Errorf("unexpected status %d for nil panic", rec.Code)
	}
}

// TestRecover_PanicWithError verifies that panics with error values are handled.
func TestRecover_PanicWithError(t *testing.T) {
	t.Parallel()
	panicHandler := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		panic(http.ErrAbortHandler)
	})

	handler := recoverMiddleware()(panicHandler)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d; want 500 for error panic", rec.Code)
	}
}

// contains is a simple substring check helper.
func contains(s, sub string) bool {
	return len(sub) > 0 && len(s) >= len(sub) &&
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}()
}
