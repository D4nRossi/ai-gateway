package proxy

import (
	"net/http"
	"testing"

	"github.com/D4nRossi/ai-gateway/internal/config"
	"github.com/D4nRossi/ai-gateway/internal/domain/endpoint"
)

func TestExtractModelFromRequestBody(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
		want string
	}{
		{"happy path", `{"model":"gpt-4.1-mini","messages":[]}`, "gpt-4.1-mini"},
		{"trims whitespace", `{"model":"  gpt-4.1   ","messages":[]}`, "gpt-4.1"},
		{"missing field", `{"messages":[]}`, ""},
		{"empty body", ``, ""},
		{"non-json", `not json{`, ""},
		{"extra fields tolerated", `{"model":"foo","extra":{"nested":true},"messages":[]}`, "foo"},
		{"null model", `{"model":null}`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractModelFromRequestBody([]byte(tc.body))
			if got != tc.want {
				t.Errorf("got %q; want %q", got, tc.want)
			}
		})
	}
}

func TestExtractUsageFromResponseBody(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		body        string
		wantOK      bool
		wantInput   int
		wantOutput  int
		wantTotal   int
		wantModel   string
	}{
		{
			name: "openai canonical",
			body: `{
				"id": "chatcmpl-xyz",
				"object": "chat.completion",
				"model": "gpt-4.1-mini-2025-01-15",
				"choices": [{"message":{"content":"hi"}}],
				"usage": {"prompt_tokens":15, "completion_tokens":7, "total_tokens":22}
			}`,
			wantOK: true, wantInput: 15, wantOutput: 7, wantTotal: 22,
			wantModel: "gpt-4.1-mini-2025-01-15",
		},
		{
			name: "azure mirrors openai",
			body: `{"model":"gpt-4.1","usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`,
			wantOK: true, wantInput: 1, wantOutput: 2, wantTotal: 3, wantModel: "gpt-4.1",
		},
		{
			name:   "no usage object",
			body:   `{"model":"gpt-4.1","choices":[]}`,
			wantOK: false,
		},
		{
			name:   "all zero tokens",
			body:   `{"usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}}`,
			wantOK: false,
		},
		{
			name:   "empty body",
			body:   ``,
			wantOK: false,
		},
		{
			name:   "non-json",
			body:   `not json`,
			wantOK: false,
		},
		{
			name: "partial — only prompt tokens",
			body: `{"usage":{"prompt_tokens":5,"completion_tokens":0,"total_tokens":5}}`,
			wantOK: true, wantInput: 5, wantOutput: 0, wantTotal: 5,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := extractUsageFromResponseBody([]byte(tc.body))
			if ok != tc.wantOK {
				t.Fatalf("ok = %v; want %v", ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if got.InputTokens != tc.wantInput || got.OutputTokens != tc.wantOutput || got.TotalTokens != tc.wantTotal {
				t.Errorf("tokens = (%d/%d/%d); want (%d/%d/%d)",
					got.InputTokens, got.OutputTokens, got.TotalTokens,
					tc.wantInput, tc.wantOutput, tc.wantTotal)
			}
			if tc.wantModel != "" && got.ResolvedModel != tc.wantModel {
				t.Errorf("resolved model = %q; want %q", got.ResolvedModel, tc.wantModel)
			}
		})
	}
}

func TestIsStreamResponse(t *testing.T) {
	t.Parallel()

	cases := []struct {
		ct   string
		want bool
	}{
		{"text/event-stream", true},
		{"text/event-stream; charset=utf-8", true},
		{"application/json", false},
		{"text/plain", false},
		{"", false},
	}
	for _, tc := range cases {
		t.Run(tc.ct, func(t *testing.T) {
			h := http.Header{}
			h.Set("Content-Type", tc.ct)
			resp := &http.Response{Header: h}
			if got := isStreamResponse(resp); got != tc.want {
				t.Errorf("got %v; want %v", got, tc.want)
			}
		})
	}
}

func TestIsIASchemaProvider(t *testing.T) {
	t.Parallel()

	cases := []struct {
		kind endpoint.ProviderKind
		want bool
	}{
		{endpoint.ProviderAzureOpenAI, true},
		{endpoint.ProviderOpenAI, true},
		{endpoint.ProviderAnthropic, false},
		{endpoint.ProviderGemini, false},
		{endpoint.ProviderCustom, false},
		{endpoint.ProviderKind("unknown"), false},
	}
	for _, tc := range cases {
		t.Run(string(tc.kind), func(t *testing.T) {
			if got := isIASchemaProvider(tc.kind); got != tc.want {
				t.Errorf("got %v; want %v", got, tc.want)
			}
		})
	}
}

func TestProviderFromKind(t *testing.T) {
	t.Parallel()

	cases := []struct {
		kind endpoint.ProviderKind
		want string
	}{
		{endpoint.ProviderAzureOpenAI, "azure"}, // alinhado com handler legacy
		{endpoint.ProviderOpenAI, "openai"},
		{endpoint.ProviderAnthropic, "anthropic"},
		{endpoint.ProviderCustom, "custom"},
		{endpoint.ProviderKind(""), "custom"},
	}
	for _, tc := range cases {
		t.Run(string(tc.kind), func(t *testing.T) {
			if got := providerFromKind(tc.kind); got != tc.want {
				t.Errorf("got %q; want %q", got, tc.want)
			}
		})
	}
}

func TestComputeCostBRL(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		mc        config.ModelConfig
		in, out   int
		want      float64
	}{
		{
			name: "happy path",
			mc:   config.ModelConfig{CostInputPer1kBRL: 0.01, CostOutputPer1kBRL: 0.04},
			in:   1000, out: 500,
			want: 0.01 + 0.5*0.04, // 0.03
		},
		{
			name: "zero tokens",
			mc:   config.ModelConfig{CostInputPer1kBRL: 0.01, CostOutputPer1kBRL: 0.04},
			in:   0, out: 0,
			want: 0,
		},
		{
			name: "no prices configured",
			mc:   config.ModelConfig{}, // both costs are zero
			in:   100, out: 50,
			want: 0,
		},
		{
			name: "only input price",
			mc:   config.ModelConfig{CostInputPer1kBRL: 0.02},
			in:   2000, out: 100,
			want: 0.04,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := computeCostBRL(tc.mc, tc.in, tc.out)
			if abs(got-tc.want) > 1e-9 {
				t.Errorf("got %v; want %v", got, tc.want)
			}
		})
	}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
