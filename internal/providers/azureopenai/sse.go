package azureopenai

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/D4nRossi/ai-gateway/internal/providers"
)

const (
	// sseDataPrefix is the prefix on every SSE data line.
	sseDataPrefix = "data: "

	// sseDone is the SSE stream termination sentinel emitted by Azure OpenAI.
	sseDone = "[DONE]"

	// sseReadBufSize is the bufio reader buffer for SSE body reads.
	// Default bufio size (4 KiB) can be too small for long response chunks;
	// 64 KiB covers the largest realistic single SSE event.
	// See SPEC.md §15.2.
	sseReadBufSize = 64 * 1024
)

// parseSSEStream reads body line-by-line, converts each SSE data event into a
// StreamChunk, and sends it to ch. It closes ch when it returns (EOF, [DONE],
// or read error).
//
// Reasoning: reading with bufio.NewReaderSize + ReadString instead of
// bufio.Scanner avoids the ErrTooLong panic that occurs when a single SSE
// line exceeds the scanner's default 64 KiB token limit (SPEC §15.2).
//
// References:
//   - SPEC.md §15.1 — SSE parsing requirements
//   - SPEC.md §15.2 — buffer sizing
//   - https://html.spec.whatwg.org/multipage/server-sent-events.html
func parseSSEStream(body io.ReadCloser, ch chan<- providers.StreamChunk) {
	defer body.Close()
	defer close(ch)

	reader := bufio.NewReaderSize(body, sseReadBufSize)

	for {
		line, err := reader.ReadString('\n')

		// Strip CRLF / LF from the line before inspecting content.
		line = strings.TrimRight(line, "\r\n")

		if line == "" {
			// Empty line is SSE keepalive or event separator; skip.
			if err == io.EOF {
				return
			}
			if err != nil {
				ch <- providers.StreamChunk{Err: fmt.Errorf("reading SSE stream: %w", err)}
				return
			}
			continue
		}

		if !strings.HasPrefix(line, sseDataPrefix) {
			// Non-data field (e.g. "event:", "id:", "retry:"); skip for forward compatibility.
			if err == io.EOF {
				return
			}
			if err != nil {
				ch <- providers.StreamChunk{Err: fmt.Errorf("reading SSE stream: %w", err)}
				return
			}
			continue
		}

		payload := line[len(sseDataPrefix):]

		if payload == sseDone {
			ch <- providers.StreamChunk{Done: true}
			return
		}

		if payload == "" {
			// Empty data field used as keepalive; continue.
			if err == io.EOF {
				return
			}
			if err != nil {
				ch <- providers.StreamChunk{Err: fmt.Errorf("reading SSE stream: %w", err)}
				return
			}
			continue
		}

		// Copy payload bytes — the underlying buffer belongs to the reader.
		data := make([]byte, len(payload))
		copy(data, payload)
		ch <- providers.StreamChunk{Data: data}

		if err == io.EOF {
			return
		}
		if err != nil {
			ch <- providers.StreamChunk{Err: fmt.Errorf("reading SSE stream: %w", err)}
			return
		}
	}
}

// usagePayload is the token-count envelope embedded in the final SSE chunk
// when stream_options.include_usage is true.
type usagePayload struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// extractUsageFromChunk attempts to read usage counters from a raw SSE chunk.
// Returns nil if the chunk does not contain a usage field.
//
// Reasoning: Azure emits usage only in the final data chunk when
// stream_options.include_usage=true (SPEC §15.4). We decode only the usage
// field to avoid allocating a full response struct on every chunk.
func extractUsageFromChunk(data []byte) *usagePayload {
	var envelope struct {
		Usage *usagePayload `json:"usage"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil
	}
	return envelope.Usage
}
