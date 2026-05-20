// Package azureopenai implements the Provider interface backed by Azure OpenAI
// (Azure AI Foundry). It handles both non-streaming and SSE streaming requests.
//
// URL pattern (SPEC §7.1):
//
//	{endpoint}/openai/deployments/{deployment}/chat/completions?api-version={version}
//
// Authentication uses the "api-key" header (SPEC §7.2), not Bearer.
//
// References:
//   - SPEC.md §7 — Azure OpenAI mapping
//   - https://learn.microsoft.com/en-us/azure/ai-services/openai/reference
package azureopenai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/D4nRossi/ai-gateway/internal/providers"
)

// Client is the Azure OpenAI HTTP client. Create via New.
type Client struct {
	endpoint   string // trimmed of trailing slash
	apiKey     string
	apiVersion string
	httpClient *http.Client
}

// New creates a Client for the given Azure OpenAI endpoint.
// requestTimeout controls the per-request HTTP deadline applied in addition to
// any context deadline the caller provides.
//
// References:
//   - SPEC.md §7 — Azure OpenAI mapping
//   - SPEC.md §4 — azure_openai.request_timeout_seconds
func New(endpoint, apiKey, apiVersion string, requestTimeout time.Duration) *Client {
	return &Client{
		endpoint:   strings.TrimRight(endpoint, "/"),
		apiKey:     apiKey,
		apiVersion: apiVersion,
		httpClient: &http.Client{Timeout: requestTimeout},
	}
}

// ChatCompletions sends a non-streaming chat completion request to Azure OpenAI
// and returns the fully buffered response.
//
// References:
//   - SPEC.md §7 — Azure OpenAI mapping
//   - SPEC.md §9.1 step 9 — provider call
func (c *Client) ChatCompletions(
	ctx context.Context,
	req providers.ChatCompletionRequest,
	deployment string,
) (*providers.ChatCompletionResponse, error) {
	// Ensure stream is off for non-streaming path.
	req.Stream = false

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshalling chat request: %w", err)
	}

	httpReq, err := c.newRequest(ctx, deployment, body)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("azure openai request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.upstreamError(resp)
	}

	var result providers.ChatCompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding azure openai response: %w", err)
	}

	return &result, nil
}

// StreamChatCompletions opens a streaming request to Azure OpenAI and returns a
// channel of raw SSE chunks. The channel is closed after [DONE] or on error.
// The caller must consume the channel and respect ctx cancellation.
//
// References:
//   - SPEC.md §7 — Azure OpenAI mapping
//   - SPEC.md §15 — streaming details
func (c *Client) StreamChatCompletions(
	ctx context.Context,
	req providers.ChatCompletionRequest,
	deployment string,
) (<-chan providers.StreamChunk, error) {
	req.Stream = true

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshalling stream request: %w", err)
	}

	// Use a client without a read deadline for streaming — the response body
	// remains open for the duration of the stream.
	streamClient := &http.Client{}

	httpReq, err := c.newRequest(ctx, deployment, body)
	if err != nil {
		return nil, err
	}

	resp, err := streamClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("azure openai stream request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, c.upstreamError(resp)
	}

	// Buffer size 64: each chunk is tiny; back-pressure from a slow consumer
	// is acceptable here since the handler reads immediately.
	ch := make(chan providers.StreamChunk, 64)
	go parseSSEStream(resp.Body, ch)

	return ch, nil
}

// newRequest builds an authenticated HTTP POST to the Azure OpenAI chat endpoint.
func (c *Client) newRequest(ctx context.Context, deployment string, body []byte) (*http.Request, error) {
	url := fmt.Sprintf(
		"%s/openai/deployments/%s/chat/completions?api-version=%s",
		c.endpoint, deployment, c.apiVersion,
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating azure openai request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	// Azure uses api-key header, not Authorization: Bearer (SPEC §7.2).
	req.Header.Set("api-key", c.apiKey)
	return req, nil
}

// upstreamError reads the error body from a non-200 response and returns a
// descriptive error. The body is read and discarded to allow connection reuse.
func (c *Client) upstreamError(resp *http.Response) error {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return fmt.Errorf("azure openai returned %d: %s", resp.StatusCode, string(raw))
}

// Ensure Client satisfies the Provider interface at compile time.
var _ providers.Provider = (*Client)(nil)
