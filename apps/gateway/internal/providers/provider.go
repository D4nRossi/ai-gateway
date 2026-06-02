// Package providers defines the Provider interface and all shared OpenAI-compatible
// request/response types used by the gateway.
//
// Both the Azure OpenAI adapter and the mock provider implement [Provider].
// The gateway's chat handler is written exclusively against this interface,
// making provider substitution transparent.
//
// References:
//   - SPEC.md §5.1 — OpenAI-compatible contracts
//   - SPEC.md §7 — Azure OpenAI mapping
//   - https://platform.openai.com/docs/api-reference/chat
package providers

import "context"

// ChatMessage represents a single turn in a conversation.
//
// References:
//   - SPEC.md §5.1
type ChatMessage struct {
	Role    string `json:"role"`    // "system" | "user" | "assistant"
	Content string `json:"content"`
}

// StreamOptions carries optional streaming behaviour flags.
//
// References:
//   - SPEC.md §5.1
type StreamOptions struct {
	IncludeUsage bool `json:"include_usage,omitempty"`
}

// ChatCompletionRequest is the OpenAI-compatible request body sent by consumer apps.
//
// References:
//   - SPEC.md §5.1
//   - https://platform.openai.com/docs/api-reference/chat/create
type ChatCompletionRequest struct {
	Model         string         `json:"model"`
	Messages      []ChatMessage  `json:"messages"`
	Temperature   *float64       `json:"temperature,omitempty"`
	MaxTokens     *int           `json:"max_tokens,omitempty"`
	TopP          *float64       `json:"top_p,omitempty"`
	Stream        bool           `json:"stream,omitempty"`
	StreamOptions *StreamOptions `json:"stream_options,omitempty"`
}

// ChatCompletionChoice is one candidate response returned by the model.
//
// References:
//   - SPEC.md §5.1
type ChatCompletionChoice struct {
	Index        int         `json:"index"`
	Message      ChatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"` // "stop" | "length" | "content_filter"
}

// Usage holds token consumption counters for a request.
//
// References:
//   - SPEC.md §5.1
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ChatCompletionResponse is the OpenAI-compatible non-streaming response.
//
// References:
//   - SPEC.md §5.1
//   - https://platform.openai.com/docs/api-reference/chat/object
type ChatCompletionResponse struct {
	ID      string                 `json:"id"`
	Object  string                 `json:"object"`  // "chat.completion"
	Created int64                  `json:"created"` // Unix timestamp
	Model   string                 `json:"model"`
	Choices []ChatCompletionChoice `json:"choices"`
	Usage   *Usage                 `json:"usage,omitempty"`
}

// StreamChunk carries a single event from an SSE stream.
//
// References:
//   - SPEC.md §5.1
//   - SPEC.md §15 — streaming details
type StreamChunk struct {
	Data []byte // Raw JSON payload (the part after "data: "; never includes the prefix)
	Done bool   // True on the "[DONE]" sentinel — signals end of stream
	Err  error  // Non-nil if the upstream connection errored mid-stream
}

// Provider is the interface implemented by all LLM backends (Azure OpenAI, Mock, …).
//
// Reasoning: the gateway's business logic depends only on this interface,
// making it possible to run the full pipeline against a mock provider during
// development without touching Azure.
//
// References:
//   - SPEC.md §5.1
//   - ADR-0001 — Go as gateway core vs. LiteLLM
type Provider interface {
	// ChatCompletions sends a non-streaming request and returns the full response.
	ChatCompletions(ctx context.Context, req ChatCompletionRequest, deployment string) (*ChatCompletionResponse, error)

	// StreamChatCompletions opens a streaming request and returns a channel of chunks.
	// The channel is closed after the [DONE] sentinel or on error.
	StreamChatCompletions(ctx context.Context, req ChatCompletionRequest, deployment string) (<-chan StreamChunk, error)
}
