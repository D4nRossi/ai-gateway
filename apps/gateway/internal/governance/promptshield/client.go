package promptshield

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client calls the Azure Content Safety APIs for Prompt Shield and Text Analyze.
//
// References:
//   - SPEC.md §11.1 — Azure CS Prompt Shield
//   - SPEC.md §11.2 — Azure CS Text Analyze
//   - https://learn.microsoft.com/en-us/azure/ai-services/content-safety/quickstart-prompt-shield
type Client struct {
	endpoint   string
	apiKey     string
	apiVersion string
	httpClient *http.Client
}

// NewClient creates a Client for the given Azure Content Safety endpoint.
// shieldTimeout is the per-call deadline for Prompt Shield requests.
//
// References:
//   - SPEC.md §4 — azure_content_safety.prompt_shield_timeout_ms
func NewClient(endpoint, apiKey, apiVersion string, shieldTimeout time.Duration) *Client {
	return &Client{
		endpoint:   strings.TrimRight(endpoint, "/"),
		apiKey:     apiKey,
		apiVersion: apiVersion,
		httpClient: &http.Client{Timeout: shieldTimeout},
	}
}

// ShieldResult holds the outcome of a Prompt Shield call.
type ShieldResult struct {
	AttackDetected bool
}

// AnalyzeResult holds the outcome of a Text Analyze call.
type AnalyzeResult struct {
	// Blocked is true if any category reached severity ≥ 4 (SPEC §11.2).
	Blocked bool
	// MaxSeverity is the highest severity value returned across all categories.
	MaxSeverity int
}

// shieldRequest is the Prompt Shield request body (SPEC §11.1).
type shieldRequest struct {
	UserPrompt string   `json:"userPrompt"`
	Documents  []string `json:"documents"`
}

// shieldResponse is the relevant portion of the Prompt Shield response.
type shieldResponse struct {
	UserPromptAnalysis struct {
		AttackDetected bool `json:"attackDetected"`
	} `json:"userPromptAnalysis"`
}

// analyzeRequest is the Text Analyze request body (SPEC §11.2).
type analyzeRequest struct {
	Text       string   `json:"text"`
	Categories []string `json:"categories"`
}

// analyzeResponse is the relevant portion of the Text Analyze response.
type analyzeResponse struct {
	CategoriesAnalysis []struct {
		Category string `json:"category"`
		Severity int    `json:"severity"`
	} `json:"categoriesAnalysis"`
}

// ShieldPrompt calls the Azure Content Safety Prompt Shield API.
//
// References:
//   - SPEC.md §11.1
//   - https://learn.microsoft.com/en-us/azure/ai-services/content-safety/quickstart-prompt-shield
func (c *Client) ShieldPrompt(ctx context.Context, prompt string) (*ShieldResult, error) {
	reqBody := shieldRequest{
		UserPrompt: prompt,
		Documents:  []string{},
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshalling prompt shield request: %w", err)
	}

	url := fmt.Sprintf(
		"%s/contentsafety/text:shieldPrompt?api-version=%s",
		c.endpoint, c.apiVersion,
	)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating prompt shield request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Ocp-Apim-Subscription-Key", c.apiKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("calling prompt shield: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("prompt shield returned %d: %s", resp.StatusCode, string(raw))
	}

	var result shieldResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding prompt shield response: %w", err)
	}

	return &ShieldResult{AttackDetected: result.UserPromptAnalysis.AttackDetected}, nil
}

// AnalyzeText calls the Azure Content Safety Text Analyze API.
// Returns Blocked=true if any category reaches severity ≥ 4 (SPEC §11.2).
//
// References:
//   - SPEC.md §11.2
func (c *Client) AnalyzeText(ctx context.Context, text string) (*AnalyzeResult, error) {
	reqBody := analyzeRequest{
		Text:       text,
		Categories: []string{"Hate", "SelfHarm", "Sexual", "Violence"},
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshalling analyze request: %w", err)
	}

	url := fmt.Sprintf(
		"%s/contentsafety/text:analyze?api-version=%s",
		c.endpoint, c.apiVersion,
	)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating analyze request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Ocp-Apim-Subscription-Key", c.apiKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("calling text analyze: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("text analyze returned %d: %s", resp.StatusCode, string(raw))
	}

	var result analyzeResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding analyze response: %w", err)
	}

	maxSev := 0
	blocked := false
	for _, cat := range result.CategoriesAnalysis {
		if cat.Severity > maxSev {
			maxSev = cat.Severity
		}
		if cat.Severity >= 4 {
			blocked = true
		}
	}

	return &AnalyzeResult{Blocked: blocked, MaxSeverity: maxSev}, nil
}
