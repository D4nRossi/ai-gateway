package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/D4nRossi/ai-gateway/internal/config"
	"github.com/D4nRossi/ai-gateway/internal/domain/endpoint"
)

// maxResponseBodyBytes caps how much of an upstream response body the usage
// extractor will read into memory. Responses bigger than this fall back to
// "minimal event" (no tokens/cost) — extracting from huge payloads isn't
// worth the memory cost, and IA responses rarely exceed a few hundred KiB.
//
// Mirrors maxProxyBodyBytes (request side) for symmetry.
const maxResponseBodyBytes = 1 << 20 // 1 MiB

// requestModelPayload mirrors the minimal subset of an OpenAI-style request
// body the usage extractor needs. Extra fields are ignored by encoding/json
// at unmarshal time.
type requestModelPayload struct {
	Model string `json:"model"`
}

// responseUsagePayload mirrors the subset of an OpenAI / Azure OpenAI chat
// completion response body the extractor needs. The schema is stable across
// the two providers (Azure mirrors OpenAI verbatim for `usage`).
type responseUsagePayload struct {
	Model string `json:"model"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// extractedUsage carries the parsed usage payload for the proxy handler to
// populate a UsageEvent.
type extractedUsage struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
	// ResolvedModel is the model name the provider echoes back. Azure
	// OpenAI may include the deployment name here; we don't use it for cost
	// lookup (the request model is more trustworthy) but expose it for
	// diagnostics.
	ResolvedModel string
}

// extractModelFromRequestBody returns the "model" field from an OpenAI-style
// chat completions request body. Returns "" when the body is empty, not JSON,
// or doesn't have a "model" key. Tolerates extra fields and unknown structure.
//
// Reasoning: the request body is the most reliable source of the *public*
// model name — the consumer addresses the gateway with the name the admin
// configured, which is what the cost catalog is indexed by. The response's
// echoed `model` is sometimes the resolved Azure deployment (e.g.
// "gpt-4.1-mini-2025-...") and is unsuitable for cost lookup.
func extractModelFromRequestBody(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var payload requestModelPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(payload.Model)
}

// extractUsageFromResponseBody returns the usage block parsed from an
// OpenAI-style response body. The second return value is false when no usable
// usage info could be extracted (empty body, not JSON, no usage object,
// or all token counts are zero).
func extractUsageFromResponseBody(body []byte) (extractedUsage, bool) {
	if len(body) == 0 {
		return extractedUsage{}, false
	}
	var payload responseUsagePayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return extractedUsage{}, false
	}
	if payload.Usage.PromptTokens == 0 &&
		payload.Usage.CompletionTokens == 0 &&
		payload.Usage.TotalTokens == 0 {
		return extractedUsage{}, false
	}
	return extractedUsage{
		InputTokens:   payload.Usage.PromptTokens,
		OutputTokens:  payload.Usage.CompletionTokens,
		TotalTokens:   payload.Usage.TotalTokens,
		ResolvedModel: payload.Model,
	}, true
}

// readCappedResponseBody reads up to maxResponseBodyBytes from resp.Body and
// restores resp.Body so the downstream ReverseProxy can stream the same bytes
// to the client. Returns (bodyBytes, truncated, error).
//
// Reasoning: ModifyResponse must put the body back — failing to restore it
// would deliver a zero-byte response to the consumer. Even when extraction
// fails, the original bytes are preserved.
func readCappedResponseBody(resp *http.Response) ([]byte, bool, error) {
	if resp.Body == nil {
		return nil, false, nil
	}
	limited := io.LimitReader(resp.Body, maxResponseBodyBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, false, err
	}
	truncated := len(body) > maxResponseBodyBytes
	if truncated {
		body = body[:maxResponseBodyBytes]
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	resp.ContentLength = int64(len(body))
	return body, truncated, nil
}

// isStreamResponse reports whether the upstream is streaming (SSE). The proxy
// V1 of usage tracking (ADR-0024) skips token extraction for streams; emitting
// a minimal event is still useful (request count + latency + status).
func isStreamResponse(resp *http.Response) bool {
	ct := resp.Header.Get("Content-Type")
	return strings.HasPrefix(ct, "text/event-stream")
}

// isJSONResponse reports whether the upstream returned a JSON body the
// extractor can parse. Defensive: some upstreams return JSON without a
// Content-Type header (rare), so we accept both explicit and implicit cases.
func isJSONResponse(resp *http.Response) bool {
	ct := resp.Header.Get("Content-Type")
	return strings.HasPrefix(ct, "application/json")
}

// isIASchemaProvider reports whether the endpoint's provider_kind uses the
// canonical OpenAI request/response shape (model in request, usage object in
// response). V1 covers azure_openai + openai. Anthropic, Gemini and others
// have their own schemas and fall through to "minimal event" until adapters
// are written (ADR-0024 §Open questions).
func isIASchemaProvider(kind endpoint.ProviderKind) bool {
	switch kind {
	case endpoint.ProviderAzureOpenAI, endpoint.ProviderOpenAI:
		return true
	default:
		return false
	}
}

// providerFromKind maps a provider_kind to the legacy `provider` string used
// in usage_events for the chat handler. Keeping the same vocabulary across
// the two emit paths means dashboards group both legacy and proxy traffic
// under a single bucket.
func providerFromKind(kind endpoint.ProviderKind) string {
	switch kind {
	case endpoint.ProviderAzureOpenAI:
		return "azure"
	case endpoint.ProviderOpenAI:
		return "openai"
	case endpoint.ProviderAnthropic:
		return "anthropic"
	case endpoint.ProviderGemini:
		return "gemini"
	case endpoint.ProviderMistral:
		return "mistral"
	case endpoint.ProviderCohere:
		return "cohere"
	case endpoint.ProviderGroq:
		return "groq"
	case endpoint.ProviderTogether:
		return "together"
	case endpoint.ProviderOllama:
		return "ollama"
	case endpoint.ProviderVLLM:
		return "vllm"
	default:
		return "custom"
	}
}

// computeCostBRL returns the estimated cost of a request given the model's
// per-1k token prices and the actual token counts. Returns 0 when the model
// has no price configured — operator hasn't filled the catalog for this
// model yet.
//
// Formula: (input_tokens / 1000) * cost_input_per_1k +
//          (output_tokens / 1000) * cost_output_per_1k.
//
// Reasoning: same formula as the chat legacy handler — keeping the math in
// one place would be ideal, but the legacy handler inlines it. Aligning
// the formulas is a follow-up (low priority — both call sites are short).
func computeCostBRL(m config.ModelConfig, inputTokens, outputTokens int) float64 {
	if m.CostInputPer1kBRL == 0 && m.CostOutputPer1kBRL == 0 {
		return 0
	}
	return float64(inputTokens)/1000.0*m.CostInputPer1kBRL +
		float64(outputTokens)/1000.0*m.CostOutputPer1kBRL
}

// readAndRestoreRequestBody reads up to maxProxyBodyBytes from r.Body and
// restores r.Body so subsequent consumers (path translator, ReverseProxy)
// see the same bytes. Returns empty slice when the request has no body.
//
// This duplicates the read-and-restore pattern inside applyTranslator but
// is callable independently — when there's no translator registered for the
// endpoint, the body still needs to be read once for usage extraction.
func readAndRestoreRequestBody(r *http.Request) ([]byte, error) {
	if r.Body == nil || r.ContentLength == 0 {
		return nil, nil
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxProxyBodyBytes))
	if err != nil {
		return nil, err
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))
	return body, nil
}
