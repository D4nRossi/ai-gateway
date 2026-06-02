package azlanguage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Client calls the Azure AI Language "Analyze Text" endpoint to detect PII
// entities and rebuilds the input text with [CATEGORY_REDACTED] placeholders.
//
// The client is safe for concurrent use. It shares a single *http.Client with
// keep-alive enabled so back-to-back requests reuse TCP/TLS connections.
type Client struct {
	endpoint   string
	apiKey     string
	apiVersion string
	language   string
	httpClient *http.Client
}

// New constructs a Client. endpoint should be the bare resource URL
// (e.g. "https://tp-language-pii.cognitiveservices.azure.com/"). A trailing
// slash is tolerated and trimmed.
//
// language is the BCP-47 tag the API uses to pick the right model
// (e.g. "pt-BR", "en"). When empty, defaults to "pt-BR".
//
// timeout is the per-request budget. Pass 0 to disable (not recommended in
// prod — the chat handler relies on this as a backstop against hung calls).
func New(endpoint, apiKey, apiVersion, language string, timeout time.Duration) *Client {
	if language == "" {
		language = "pt-BR"
	}
	return &Client{
		endpoint:   strings.TrimRight(endpoint, "/"),
		apiKey:     apiKey,
		apiVersion: apiVersion,
		language:   language,
		httpClient: &http.Client{Timeout: timeout},
	}
}

// Entity is a single PII detection returned by the Language API.
//
// Offsets are expressed in the unit chosen by stringIndexType in the request
// parameters. We request "UnicodeCodePoint" so offsets/lengths match Go's
// rune count exactly — that lets redactEntities slice over []rune without
// any conversion math, and handles ã/ç/é correctly (UTF-8 multi-byte chars
// that count as 1 code point).
type Entity struct {
	Text            string  `json:"text"`
	Category        string  `json:"category"`
	Subcategory     string  `json:"subcategory,omitempty"`
	Offset          int     `json:"offset"`
	Length          int     `json:"length"`
	ConfidenceScore float64 `json:"confidenceScore"`
}

// AnalyzeResult is what Mask returns to the caller.
type AnalyzeResult struct {
	// Redacted is the input text with each entity span replaced by
	// "[CATEGORY_REDACTED]".
	Redacted string

	// Entities is the raw detection list, kept for audit/observability.
	Entities []Entity

	// Categories counts entities per category, useful for audit metadata.
	// Example: {"Person": 2, "Email": 1}.
	Categories map[string]int
}

// Mask sends text to the Azure Language API and returns it with detected PII
// substituted by named placeholders. Empty input returns an empty result
// without calling the API.
//
// Errors:
//   - context cancellation/timeout: wrapped, callers can errors.Is(ctx.Err)
//   - HTTP non-2xx from upstream: wrapped with status + truncated body
//   - JSON shape mismatch: wrapped with a clear message
//
// Reasoning: returning the substituted text plus the raw entities (instead of
// just the redacted text) lets the caller log "what was masked" without
// re-running detection. Categories is a pre-aggregated convenience for the
// audit event metadata field.
func (c *Client) Mask(ctx context.Context, text string) (AnalyzeResult, error) {
	if text == "" {
		return AnalyzeResult{Redacted: "", Categories: map[string]int{}}, nil
	}

	reqBody := analyzeRequest{
		Kind: "PiiEntityRecognition",
		Parameters: analyzeParameters{
			ModelVersion: "latest",
			// UnicodeCodePoint = Go rune count. Default is TextElement_v8
			// (grapheme cluster) which doesn't map cleanly to Go strings.
			// Utf16CodeUnit forces us to do UTF-16 conversion math. The
			// UnicodeCodePoint setting lets redactEntities slice []rune
			// directly using offset/length.
			StringIndexType: "UnicodeCodePoint",
		},
		AnalysisInput: analysisInput{
			Documents: []document{
				{ID: "1", Language: c.language, Text: text},
			},
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return AnalyzeResult{}, fmt.Errorf("marshalling analyze request: %w", err)
	}

	url := fmt.Sprintf("%s/language/:analyze-text?api-version=%s", c.endpoint, c.apiVersion)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return AnalyzeResult{}, fmt.Errorf("creating analyze request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	// Azure AI Language uses the Cognitive Services subscription-key header,
	// not Authorization: Bearer (separate auth scheme from Azure OpenAI).
	httpReq.Header.Set("Ocp-Apim-Subscription-Key", c.apiKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return AnalyzeResult{}, fmt.Errorf("azure language request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return AnalyzeResult{}, fmt.Errorf("azure language returned %d: %s", resp.StatusCode, string(raw))
	}

	var parsed analyzeResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return AnalyzeResult{}, fmt.Errorf("decoding azure language response: %w", err)
	}

	if len(parsed.Results.Documents) == 0 {
		// No document echoed back — treat as nothing detected. Safer than
		// erroring; matches behavior of the Cognitive Services API when it
		// can't process the text (returns empty documents + warnings list).
		return AnalyzeResult{Redacted: text, Categories: map[string]int{}}, nil
	}

	doc := parsed.Results.Documents[0]
	redacted, cats := redactEntities(text, doc.Entities)

	return AnalyzeResult{
		Redacted:   redacted,
		Entities:   doc.Entities,
		Categories: cats,
	}, nil
}

// redactEntities rebuilds text by replacing each entity span with
// "[CATEGORY_REDACTED]". Iterates from the highest offset down so earlier
// replacements don't shift the offsets we still need.
//
// Offsets are in Unicode code points (== Go runes) because we request
// stringIndexType=UnicodeCodePoint. Operating on []rune avoids the UTF-8
// multi-byte alignment problem ("João" has 4 runes but 5 bytes — the ã
// occupies 2 bytes — so byte-indexed slicing splits the ã character).
//
// Reasoning: this is preferred over Azure's redactedText (which substitutes
// asterisks) because named placeholders are more useful for both debugging
// and for downstream models that may want to know the entity TYPE without
// seeing the value.
func redactEntities(text string, entities []Entity) (string, map[string]int) {
	if len(entities) == 0 {
		return text, map[string]int{}
	}

	// Defensive copy so we don't mutate the caller's slice.
	sorted := make([]Entity, len(entities))
	copy(sorted, entities)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Offset > sorted[j].Offset
	})

	runes := []rune(text)
	cats := make(map[string]int, len(sorted))
	for _, ent := range sorted {
		cats[ent.Category]++
		// Guard against malformed offsets — the API can theoretically return
		// offsets past the text length if the model is fed pre-tokenized input.
		// Skip rather than panic; the caller still gets the category count.
		if ent.Offset < 0 || ent.Offset+ent.Length > len(runes) {
			continue
		}
		placeholder := "[" + strings.ToUpper(ent.Category) + "_REDACTED]"
		prefix := string(runes[:ent.Offset])
		suffix := string(runes[ent.Offset+ent.Length:])
		joined := prefix + placeholder + suffix
		runes = []rune(joined)
	}
	return string(runes), cats
}

// ─── Wire types (lowercase = unexported, JSON shape matches Azure exactly) ───

type analyzeRequest struct {
	Kind          string            `json:"kind"`
	Parameters    analyzeParameters `json:"parameters"`
	AnalysisInput analysisInput     `json:"analysisInput"`
}

type analyzeParameters struct {
	ModelVersion    string `json:"modelVersion"`
	StringIndexType string `json:"stringIndexType,omitempty"`
}

type analysisInput struct {
	Documents []document `json:"documents"`
}

type document struct {
	ID       string `json:"id"`
	Language string `json:"language"`
	Text     string `json:"text"`
}

type analyzeResponse struct {
	Kind    string         `json:"kind"`
	Results analyzeResults `json:"results"`
}

type analyzeResults struct {
	Documents []resultDocument `json:"documents"`
	Errors    []any            `json:"errors,omitempty"`
}

type resultDocument struct {
	ID           string   `json:"id"`
	RedactedText string   `json:"redactedText"`
	Entities     []Entity `json:"entities"`
}
