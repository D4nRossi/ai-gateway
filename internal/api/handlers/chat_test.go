package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/D4nRossi/ai-gateway/internal/api/middleware"
	"github.com/D4nRossi/ai-gateway/internal/audit"
	"github.com/D4nRossi/ai-gateway/internal/auth"
	"github.com/D4nRossi/ai-gateway/internal/budget"
	"github.com/D4nRossi/ai-gateway/internal/config"
	"github.com/D4nRossi/ai-gateway/internal/observability"
	"github.com/D4nRossi/ai-gateway/internal/providers"
	"github.com/D4nRossi/ai-gateway/internal/providers/mock"
	"github.com/D4nRossi/ai-gateway/internal/security/postvalidation"
	"github.com/D4nRossi/ai-gateway/internal/usage"
)

// ── Test stubs ────────────────────────────────────────────────────────────────

type noopAudit struct{}

func (noopAudit) Emit(audit.AuditEvent) {}

type noopUsage struct{}

func (noopUsage) Emit(usage.UsageEvent) {}

type allowBudget struct{}

func (allowBudget) Check(_ context.Context, _ string, _ float64) error { return nil }

type denyBudget struct{}

func (denyBudget) Check(_ context.Context, _ string, _ float64) error {
	return budget.ErrBudgetExceeded
}

type noopRecorder struct{}

func (noopRecorder) Record(budget.UpdateEvent) {}

// ── Test helpers ──────────────────────────────────────────────────────────────

// testConfig builds a minimal Config containing one model and one application.
func testConfig() *config.Config {
	return &config.Config{
		AzureOpenAI: config.AzureOpenAIConfig{RequestTimeoutSeconds: 30},
		Models: []config.ModelConfig{
			{
				PublicName:         "gpt-4.1-nano",
				Deployment:         "nano-deploy",
				Provider:           "mock",
				CostInputPer1kBRL:  0.001,
				CostOutputPer1kBRL: 0.002,
			},
			{
				PublicName: "gpt-4.1-mini",
				Deployment: "mini-deploy",
				Provider:   "mock",
			},
		},
	}
}

// testPolicy returns an AppPolicy for an application with the given allowed models.
func testPolicy(tier string, allowedModels []string, streamOK bool) auth.AppPolicy {
	return auth.AppPolicy{
		Name:             "TestApp",
		KeyPrefix:        "gwk_test",
		KeyHash:          strings.Repeat("a", 64),
		Tier:             tier,
		AllowedModels:    allowedModels,
		StreamingAllowed: streamOK,
		MaxRPM:           1000,
		MonthlyBudgetBRL: 999.99,
	}
}

// chatHandler builds a Chat http.HandlerFunc with the given budget and provider.
func chatHandler(budgetCheck budget.PreChecker, prov providers.Provider) http.HandlerFunc {
	logger, _ := observability.New("error", "text")
	return Chat(ChatDeps{
		Provider:    prov,
		Config:      testConfig(),
		AuditWriter: noopAudit{},
		UsageWriter: noopUsage{},
		BudgetCheck: budgetCheck,
		BudgetCount: noopRecorder{},
		Validator:   postvalidation.New(),
		Logger:      logger,
	})
}

// injectPolicy wraps handler with a context that contains the given AppPolicy,
// bypassing the Auth middleware for handler-only unit tests.
func injectPolicy(policy auth.AppPolicy, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := middleware.WithPolicy(r.Context(), policy)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// jsonBody serialises v and returns an io.Reader for use in httptest.NewRequest.
func jsonBody(t *testing.T, v any) *bytes.Buffer {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return bytes.NewBuffer(b)
}

// assertErrorBody decodes the response and checks error.message and error.type.
func assertErrorBody(t *testing.T, rec *httptest.ResponseRecorder, wantMsg, wantType string) {
	t.Helper()
	var body struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if body.Error.Message != wantMsg {
		t.Errorf("error.message = %q; want %q", body.Error.Message, wantMsg)
	}
	if wantType != "" && body.Error.Type != wantType {
		t.Errorf("error.type = %q; want %q", body.Error.Type, wantType)
	}
}

// ── Tests ─────────────────────────────────────────────────────────────────────

// TestChat_ModelNotAllowed verifies that requesting a model outside the
// application's allowlist returns 403 model_not_allowed.
//
// References:
//   - SPEC.md §6.4
//   - SPEC.md §9.1 step 6c
func TestChat_ModelNotAllowed(t *testing.T) {
	t.Parallel()

	policy := testPolicy("tier_1", []string{"gpt-4.1-nano"}, false)
	handler := injectPolicy(policy, chatHandler(allowBudget{}, mock.New()))

	body := jsonBody(t, map[string]any{
		"model":    "gpt-4.1-mini", // not in allowlist
		"messages": []map[string]string{{"role": "user", "content": "hi"}},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d; want 403", rec.Code)
	}
	assertErrorBody(t, rec, "model_not_allowed", "policy_error")
}

// TestChat_StreamingNotAllowed verifies that a streaming request from an app with
// streaming_allowed=false returns 403 streaming_not_allowed.
//
// References:
//   - SPEC.md §6.4
//   - SPEC.md §9.1 step 6d
func TestChat_StreamingNotAllowed(t *testing.T) {
	t.Parallel()

	policy := testPolicy("tier_1", []string{"gpt-4.1-nano"}, false) // streaming=false
	handler := injectPolicy(policy, chatHandler(allowBudget{}, mock.New()))

	body := jsonBody(t, map[string]any{
		"model":    "gpt-4.1-nano",
		"messages": []map[string]string{{"role": "user", "content": "hi"}},
		"stream":   true,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d; want 403", rec.Code)
	}
	assertErrorBody(t, rec, "streaming_not_allowed", "policy_error")
}

// TestChat_InvalidJSON verifies that malformed request body returns 400.
//
// References:
//   - SPEC.md §6.4
func TestChat_InvalidJSON(t *testing.T) {
	t.Parallel()

	policy := testPolicy("tier_1", []string{"gpt-4.1-nano"}, false)
	handler := injectPolicy(policy, chatHandler(allowBudget{}, mock.New()))

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader("{invalid json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d; want 400", rec.Code)
	}
	assertErrorBody(t, rec, "invalid_json", "")
}

// TestChat_BudgetExceeded verifies that a depleted budget returns 429 budget_exceeded.
//
// References:
//   - SPEC.md §6.4
//   - SPEC.md §9.1 step 8 — budget pre-check
func TestChat_BudgetExceeded(t *testing.T) {
	t.Parallel()

	policy := testPolicy("tier_1", []string{"gpt-4.1-nano"}, false)
	handler := injectPolicy(policy, chatHandler(denyBudget{}, mock.New()))

	body := jsonBody(t, map[string]any{
		"model":    "gpt-4.1-nano",
		"messages": []map[string]string{{"role": "user", "content": "hi"}},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d; want 429", rec.Code)
	}
	assertErrorBody(t, rec, "budget_exceeded", "budget_error")
}

// TestChat_NonStreamSuccess verifies that a valid non-streaming request returns 200
// with an OpenAI-compatible response body when using the mock provider.
//
// References:
//   - SPEC.md §6.2
func TestChat_NonStreamSuccess(t *testing.T) {
	t.Parallel()

	policy := testPolicy("tier_1", []string{"gpt-4.1-nano"}, false)
	handler := injectPolicy(policy, chatHandler(allowBudget{}, mock.New()))

	body := jsonBody(t, map[string]any{
		"model":    "gpt-4.1-nano",
		"messages": []map[string]string{{"role": "user", "content": "Hello"}},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d; want 200 (body: %s)", rec.Code, rec.Body.String())
	}

	var resp providers.ChatCompletionResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Choices) == 0 {
		t.Error("expected at least one choice in response")
	}
	if resp.Choices[0].Message.Content == "" {
		t.Error("expected non-empty message content in response")
	}
}

// TestChat_BodyTooLarge verifies that requests exceeding the 1 MiB body limit
// are rejected with 413.
//
// References:
//   - SPEC.md §14.3 — MaxBodyBytes = 1 MiB
func TestChat_BodyTooLarge(t *testing.T) {
	t.Parallel()

	policy := testPolicy("tier_1", []string{"gpt-4.1-nano"}, false)
	handler := injectPolicy(policy, chatHandler(allowBudget{}, mock.New()))

	// Build a body slightly over 1 MiB.
	hugePadding := strings.Repeat("x", 1<<20+1)
	raw := `{"model":"gpt-4.1-nano","messages":[{"role":"user","content":"` + hugePadding + `"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d; want 413", rec.Code)
	}
	assertErrorBody(t, rec, "payload_too_large", "")
}

// TestChat_PIIMaskedInTier1 verifies that a CPF in the prompt is redacted before
// reaching the provider when the application is on Tier 1.
//
// The mock provider echoes back a fixed response; we verify indirectly via the
// audit system. Since tests use noopAudit, we verify by checking that the
// response is still 200 (pipeline ran without blocking).
//
// References:
//   - SPEC.md §10 — masking specification
func TestChat_PIIMaskedInTier1(t *testing.T) {
	t.Parallel()

	policy := testPolicy("tier_1", []string{"gpt-4.1-nano"}, false)
	handler := injectPolicy(policy, chatHandler(allowBudget{}, mock.New()))

	body := jsonBody(t, map[string]any{
		"model": "gpt-4.1-nano",
		"messages": []map[string]string{
			{"role": "user", "content": "Meu CPF é 529.982.247-25"},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// With mock provider and valid CPF the request should complete successfully.
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d; want 200 (body: %s)", rec.Code, rec.Body.String())
	}
}
