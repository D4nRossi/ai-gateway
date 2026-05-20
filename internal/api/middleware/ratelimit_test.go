package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/D4nRossi/ai-gateway/internal/auth"
	"github.com/D4nRossi/ai-gateway/internal/observability"
)

// ── Limiter stubs ─────────────────────────────────────────────────────────────

type allowLimiter struct{}

func (allowLimiter) Allow(_ string) bool { return true }

type denyLimiter struct{}

func (denyLimiter) Allow(_ string) bool { return false }

// ── Helpers ───────────────────────────────────────────────────────────────────

func rateLimitHandler(lim interface{ Allow(string) bool }) http.Handler {
	logger, _ := observability.New("error", "text")
	return RateLimit(lim, nil, logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
}

func injectPolicyForRL(policy auth.AppPolicy, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := WithPolicy(r.Context(), policy)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func rlPolicy() auth.AppPolicy {
	return auth.AppPolicy{Name: "TestApp", KeyPrefix: "gwk_test"}
}

// ── Tests ─────────────────────────────────────────────────────────────────────

// TestRateLimit_Allows verifies that an allowed request passes through to the handler.
func TestRateLimit_Allows(t *testing.T) {
	t.Parallel()
	handler := injectPolicyForRL(rlPolicy(), rateLimitHandler(allowLimiter{}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d; want 200", rec.Code)
	}
}

// TestRateLimit_Denies verifies that a denied request returns 429.
func TestRateLimit_Denies(t *testing.T) {
	t.Parallel()
	handler := injectPolicyForRL(rlPolicy(), rateLimitHandler(denyLimiter{}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d; want 429", rec.Code)
	}
}

// TestRateLimit_ErrorBody verifies the 429 body format matches the contract.
func TestRateLimit_ErrorBody(t *testing.T) {
	t.Parallel()
	handler := injectPolicyForRL(rlPolicy(), rateLimitHandler(denyLimiter{}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var body struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Error.Message != "rate_limited" {
		t.Errorf("error.message = %q; want %q", body.Error.Message, "rate_limited")
	}
	if body.Error.Type != "rate_limit_error" {
		t.Errorf("error.type = %q; want %q", body.Error.Type, "rate_limit_error")
	}
}

// TestRateLimit_RetryAfterHeader verifies the Retry-After header is set.
func TestRateLimit_RetryAfterHeader(t *testing.T) {
	t.Parallel()
	handler := injectPolicyForRL(rlPolicy(), rateLimitHandler(denyLimiter{}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if ra := rec.Header().Get("Retry-After"); ra == "" {
		t.Error("Retry-After header missing on 429 response")
	}
}

// TestRateLimit_NoPolicyPassthrough verifies that when no policy is in context
// (before Auth middleware runs), the request passes through unchanged.
func TestRateLimit_NoPolicyPassthrough(t *testing.T) {
	t.Parallel()
	// No injectPolicy — context has no AppPolicy.
	handler := rateLimitHandler(denyLimiter{})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Should pass through (200) because no policy means RateLimit skips.
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d; want 200 (no policy in context should pass through)", rec.Code)
	}
}

// TestRateLimit_ContentTypeOnDeny verifies JSON content-type on 429.
func TestRateLimit_ContentTypeOnDeny(t *testing.T) {
	t.Parallel()
	handler := injectPolicyForRL(rlPolicy(), rateLimitHandler(denyLimiter{}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q; want %q", ct, "application/json")
	}
}
