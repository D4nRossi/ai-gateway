// Package mock provides a MockProvider for local development and testing.
// It returns deterministic canned responses without calling any external service.
//
// References:
//   - SPEC.md §16 step 8 — mock provider selected via PROVIDER env var
package mock

import (
	"context"
	"fmt"
	"time"

	"github.com/D4nRossi/ai-gateway/internal/providers"
)

// MockProvider is a Provider implementation that returns canned responses.
// It is selected when the PROVIDER environment variable is set to "mock".
type MockProvider struct{}

// New returns a ready-to-use MockProvider.
func New() *MockProvider {
	return &MockProvider{}
}

// ChatCompletions returns a canned non-streaming response for any request.
//
// Reasoning: the mock always succeeds so the full gateway pipeline can be
// exercised (auth, masking, rate limit, budget) without Azure credentials.
//
// References:
//   - SPEC.md §5.1 — ChatCompletionResponse shape
func (m *MockProvider) ChatCompletions(
	ctx context.Context,
	req providers.ChatCompletionRequest,
	deployment string,
) (*providers.ChatCompletionResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("mock provider: context cancelled: %w", err)
	}
	return &providers.ChatCompletionResponse{
		ID:      "mock-chatcmpl-00000000",
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   req.Model,
		Choices: []providers.ChatCompletionChoice{
			{
				Index: 0,
				Message: providers.ChatMessage{
					Role:    "assistant",
					Content: "[mock] This is a canned response from the AI Gateway mock provider.",
				},
				FinishReason: "stop",
			},
		},
		Usage: &providers.Usage{
			PromptTokens:     10,
			CompletionTokens: 20,
			TotalTokens:      30,
		},
	}, nil
}

// StreamChatCompletions returns a channel that emits a few canned SSE chunks
// followed by the [DONE] sentinel, simulating an SSE stream.
//
// References:
//   - SPEC.md §5.1 — StreamChunk
//   - SPEC.md §15 — streaming details
func (m *MockProvider) StreamChatCompletions(
	ctx context.Context,
	req providers.ChatCompletionRequest,
	deployment string,
) (<-chan providers.StreamChunk, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("mock provider: context cancelled: %w", err)
	}

	chunks := []string{
		`{"id":"mock-1","object":"chat.completion.chunk","model":"` + req.Model + `","choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]}`,
		`{"id":"mock-1","object":"chat.completion.chunk","model":"` + req.Model + `","choices":[{"index":0,"delta":{"content":"[mock] "},"finish_reason":null}]}`,
		`{"id":"mock-1","object":"chat.completion.chunk","model":"` + req.Model + `","choices":[{"index":0,"delta":{"content":"canned stream"},"finish_reason":null}]}`,
		`{"id":"mock-1","object":"chat.completion.chunk","model":"` + req.Model + `","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":3,"total_tokens":13}}`,
	}

	ch := make(chan providers.StreamChunk, len(chunks)+1)
	go func() {
		defer close(ch)
		for _, raw := range chunks {
			select {
			case <-ctx.Done():
				return
			default:
			}
			ch <- providers.StreamChunk{Data: []byte(raw)}
		}
		ch <- providers.StreamChunk{Done: true}
	}()

	return ch, nil
}


// Ensure MockProvider satisfies the Provider interface at compile time.
var _ providers.Provider = (*MockProvider)(nil)
