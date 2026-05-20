package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/D4nRossi/ai-gateway/internal/auth"
	"github.com/D4nRossi/ai-gateway/internal/observability"
)

// authHandlerOnly builds Auth middleware wrapping a 200 OK handler (no audit).
func authHandlerOnly(store auth.PolicyStore) http.Handler {
	logger, _ := observability.New("error", "text")
	return Auth(store, nil, logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
}

// ── SQL/injection in token values ─────────────────────────────────────────────

// TestAuth_SQLInjectionInToken verifies that tokens crafted as SQL injection
// strings are simply rejected with 401 without causing a panic or 500.
func TestAuth_SQLInjectionInToken(t *testing.T) {
	t.Parallel()
	store, _ := makeTestStore("gwk_app_validtoken123")
	handler := authHandlerOnly(store)

	sqlTokens := []string{
		"gwk_app_' OR '1'='1",
		"gwk_app_; DROP TABLE applications;--",
		"gwk_app_\" OR \"\"=\"",
		"gwk_app_\\'; EXEC xp_cmdshell('dir');--",
	}
	for _, tok := range sqlTokens {
		tok := tok
		t.Run(tok, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Authorization", "Bearer "+tok)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code == http.StatusInternalServerError {
				t.Errorf("SQL injection token triggered 500: %q", tok)
			}
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("token %q: status=%d; want 401", tok, rec.Code)
			}
		})
	}
}

// TestAuth_TimingConsistency verifies that different failure modes (wrong prefix,
// right prefix/wrong secret, partial token) all return 401 with no status oracle.
func TestAuth_TimingConsistency(t *testing.T) {
	t.Parallel()
	store, _ := makeTestStore("gwk_app_validtoken123")
	handler := authHandlerOnly(store)

	cases := []struct {
		name  string
		token string
	}{
		{"valid prefix wrong secret", "gwk_app_wrongsecret"},
		{"unknown prefix", "gwk_unknown_sometoken"},
		{"empty secret", "gwk_app_"},
		{"prefix only", "gwk_app"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Authorization", "Bearer "+tc.token)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("%s: status=%d; want 401", tc.name, rec.Code)
			}
		})
	}
}

// TestAuth_TokenPrefixVariants verifies only the exact valid token passes.
func TestAuth_TokenPrefixVariants(t *testing.T) {
	t.Parallel()
	const token = "gwk_myapp_secretpart9x"
	store, _ := makeTestStore(token)
	handler := authHandlerOnly(store)

	cases := []struct {
		name   string
		header string
		want   int
	}{
		{"valid token", "Bearer " + token, http.StatusOK},
		{"prefix only", "Bearer gwk_myapp", http.StatusUnauthorized},
		{"partial token", "Bearer gwk_myapp_secretpar", http.StatusUnauthorized},
		{"extra char", "Bearer " + token + "X", http.StatusUnauthorized},
		{"uppercase BEARER", "BEARER " + token, http.StatusUnauthorized},
		{"no bearer scheme", token, http.StatusUnauthorized},
		{"basic scheme", "Basic " + token, http.StatusUnauthorized},
		{"double bearer", "Bearer Bearer " + token, http.StatusUnauthorized},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Authorization", tc.header)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Errorf("%s: status=%d; want %d", tc.name, rec.Code, tc.want)
			}
		})
	}
}

// TestAuth_MalformedAuthHeaders covers headers that could confuse parsers.
// None should cause a 500 — only 401.
func TestAuth_MalformedAuthHeaders(t *testing.T) {
	t.Parallel()
	store, _ := makeTestStore("gwk_app_validtoken123")
	handler := authHandlerOnly(store)

	cases := []struct {
		name   string
		header string
	}{
		{"empty header", ""},
		{"whitespace only", "   "},
		{"bearer with no token", "Bearer"},
		{"bearer with spaces", "Bearer   "},
		{"null bytes in token", "Bearer gwk_app_\x00null"},
		{"very long token", "Bearer gwk_app_" + strings.Repeat("x", 10000)},
		{"unicode token", "Bearer gwk_app_こんにちは"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Authorization", tc.header)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code == http.StatusInternalServerError {
				t.Errorf("malformed header %q triggered 500 (must never happen)", tc.name)
			}
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("%s: status=%d; want 401", tc.name, rec.Code)
			}
		})
	}
}

// TestAuth_PolicyInjectedOnSuccess verifies the AppPolicy is in context after
// successful authentication, so downstream middleware can read it.
func TestAuth_PolicyInjectedOnSuccess(t *testing.T) {
	t.Parallel()
	const token = "gwk_check_contextpolicy9"
	store, _ := makeTestStore(token)

	var gotName string
	logger, _ := observability.New("error", "text")
	captureHandler := Auth(store, nil, logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p, ok := PolicyFrom(r.Context()); ok {
			gotName = p.Name
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	captureHandler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d; want 200", rec.Code)
	}
	if gotName == "" {
		t.Error("policy.Name in context is empty; policy was not injected")
	}
}

// TestAuth_ConcurrentRequests verifies Auth middleware is goroutine-safe.
func TestAuth_ConcurrentRequests(t *testing.T) {
	t.Parallel()
	const token = "gwk_conc_goroutinesafe99"
	store, _ := makeTestStore(token)
	handler := authHandlerOnly(store)

	const goroutines = 50
	codes := make(chan int, goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			codes <- rec.Code
		}()
	}

	for i := 0; i < goroutines; i++ {
		code := <-codes
		if code != http.StatusOK {
			t.Errorf("concurrent request: status=%d; want 200", code)
		}
	}
}
