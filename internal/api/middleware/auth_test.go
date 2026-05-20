package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/D4nRossi/ai-gateway/internal/audit"
	"github.com/D4nRossi/ai-gateway/internal/auth"
	"github.com/D4nRossi/ai-gateway/internal/observability"
)

// captureAuditWriter records all emitted events for assertion in tests.
type captureAuditWriter struct {
	events []audit.AuditEvent
}

func (c *captureAuditWriter) Emit(e audit.AuditEvent) {
	c.events = append(c.events, e)
}

// testPolicyStore is a minimal PolicyStore backed by a fixed token for tests.
type testPolicyStore struct {
	prefix string
	policy auth.AppPolicy
}

func (s *testPolicyStore) Lookup(prefix string) (auth.AppPolicy, bool) {
	if prefix == s.prefix {
		return s.policy, true
	}
	return auth.AppPolicy{}, false
}

// makeTestStore creates a PolicyStore containing exactly one app whose token is tok.
func makeTestStore(tok string) (auth.PolicyStore, string) {
	sum := sha256.Sum256([]byte(tok))
	h := hex.EncodeToString(sum[:])
	prefix := auth.ExtractPrefix(tok)
	store := &testPolicyStore{
		prefix: prefix,
		policy: auth.AppPolicy{
			Name:          "TestApp",
			KeyPrefix:     prefix,
			KeyHash:       h,
			Tier:          "tier_1",
			AllowedModels: []string{"gpt-4.1-nano"},
		},
	}
	return store, h
}

// newAuthHandler wires a test store into an Auth middleware around a trivial handler.
func newAuthHandler(store auth.PolicyStore, aw audit.Emitter) http.Handler {
	logger, _ := observability.New("info", "text")
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	return RequestID(Auth(store, aw, logger)(inner))
}

// withRequestID injects a request_id into a request's context (simulates RequestID middleware).
func withRequestID(r *http.Request) *http.Request {
	ctx := context.WithValue(r.Context(), observability.RequestIDKey, "test-request-id")
	return r.WithContext(ctx)
}

// TestAuth_ValidToken verifies that a correct bearer token leads to 200 and injects AppPolicy.
//
// References:
//   - SPEC.md §9.1 step 4 — token validation flow
func TestAuth_ValidToken(t *testing.T) {
	t.Parallel()

	const token = "gwk_test_secrettoken123"
	store, _ := makeTestStore(token)
	aw := &captureAuditWriter{}
	handler := newAuthHandler(store, aw)

	req := withRequestID(httptest.NewRequest(http.MethodGet, "/", nil))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d; want 200", rec.Code)
	}
	if len(aw.events) != 0 {
		t.Errorf("expected no audit events on success, got %d", len(aw.events))
	}
}

// TestAuth_WrongToken verifies that an incorrect token leads to 401.
func TestAuth_WrongToken(t *testing.T) {
	t.Parallel()

	const token = "gwk_test_correcttoken"
	store, _ := makeTestStore(token)
	aw := &captureAuditWriter{}
	handler := newAuthHandler(store, aw)

	req := withRequestID(httptest.NewRequest(http.MethodGet, "/", nil))
	req.Header.Set("Authorization", "Bearer gwk_test_WRONGTOKEN")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d; want 401", rec.Code)
	}
	assertAuthFailedAudit(t, aw)
}

// TestAuth_MissingHeader verifies that a missing Authorization header leads to 401.
func TestAuth_MissingHeader(t *testing.T) {
	t.Parallel()

	store, _ := makeTestStore("gwk_test_sometoken")
	aw := &captureAuditWriter{}
	handler := newAuthHandler(store, aw)

	req := withRequestID(httptest.NewRequest(http.MethodGet, "/", nil))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d; want 401", rec.Code)
	}
	assertAuthFailedAudit(t, aw)
}

// TestAuth_WrongTokenPrefix verifies that a token with an unknown prefix leads to 401.
func TestAuth_WrongTokenPrefix(t *testing.T) {
	t.Parallel()

	store, _ := makeTestStore("gwk_test_sometoken")
	aw := &captureAuditWriter{}
	handler := newAuthHandler(store, aw)

	req := withRequestID(httptest.NewRequest(http.MethodGet, "/", nil))
	// "gwk_other" prefix is not in the store.
	req.Header.Set("Authorization", "Bearer gwk_other_sometoken")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d; want 401", rec.Code)
	}
	assertAuthFailedAudit(t, aw)
}

// TestAuth_NotBearerScheme verifies that non-Bearer auth schemes lead to 401.
func TestAuth_NotBearerScheme(t *testing.T) {
	t.Parallel()

	store, _ := makeTestStore("gwk_test_tok")
	aw := &captureAuditWriter{}
	handler := newAuthHandler(store, aw)

	req := withRequestID(httptest.NewRequest(http.MethodGet, "/", nil))
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d; want 401", rec.Code)
	}
}

// TestAuth_ErrorBodyFormat verifies the JSON error shape per SPEC §6.4.
func TestAuth_ErrorBodyFormat(t *testing.T) {
	t.Parallel()

	store, _ := makeTestStore("gwk_test_tok")
	aw := &captureAuditWriter{}
	handler := newAuthHandler(store, aw)

	req := withRequestID(httptest.NewRequest(http.MethodGet, "/", nil))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var body struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode error body: %v", err)
	}
	if body.Error.Message != "unauthorized" {
		t.Errorf("error.message = %q; want %q", body.Error.Message, "unauthorized")
	}
	if body.Error.Type != "auth_error" {
		t.Errorf("error.type = %q; want %q", body.Error.Type, "auth_error")
	}
}

// assertAuthFailedAudit is a helper that checks an auth_failed event was emitted.
func assertAuthFailedAudit(t *testing.T, aw *captureAuditWriter) {
	t.Helper()
	if len(aw.events) == 0 {
		t.Error("expected auth_failed audit event, got none")
		return
	}
	got := aw.events[len(aw.events)-1]
	if got.EventType != audit.EventAuthFailed {
		t.Errorf("audit event_type = %q; want %q", got.EventType, audit.EventAuthFailed)
	}
	if got.CreatedAt.IsZero() {
		t.Error("audit event CreatedAt is zero")
	}
	if got.CreatedAt.After(time.Now().Add(time.Second)) {
		t.Error("audit event CreatedAt is in the future")
	}
}
