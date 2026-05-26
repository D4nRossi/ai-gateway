package translator

import (
	"errors"
	"strings"
	"testing"

	"github.com/D4nRossi/ai-gateway/internal/domain/endpoint"
)

func TestAzureOpenAITranslate_HappyPath(t *testing.T) {
	t.Parallel()

	in := Input{
		CanonicalPath: "/chat/completions",
		Method:        "POST",
		Body:          []byte(`{"model":"gpt-4.1","messages":[{"role":"user","content":"hi"}]}`),
		Config: endpoint.ProviderConfig{
			"api_version": "2025-01-01-preview",
			"model_to_deployment": map[string]any{
				"gpt-4.1":      "gpt-4.1",
				"gpt-4.1-mini": "gpt-4.1-mini",
			},
		},
	}

	out, err := azureOpenAI{}.Translate(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Path != "/openai/deployments/gpt-4.1/chat/completions" {
		t.Errorf("Path = %q; want /openai/deployments/gpt-4.1/chat/completions", out.Path)
	}
	if !strings.Contains(out.RawQuery, "api-version=2025-01-01-preview") {
		t.Errorf("RawQuery = %q; want to contain api-version=2025-01-01-preview", out.RawQuery)
	}
}

func TestAzureOpenAITranslate_ModelMapsToDifferentDeployment(t *testing.T) {
	t.Parallel()

	out, err := azureOpenAI{}.Translate(Input{
		CanonicalPath: "/chat/completions",
		Method:        "POST",
		Body:          []byte(`{"model":"latest"}`),
		Config: endpoint.ProviderConfig{
			"api_version": "2025-01-01-preview",
			"model_to_deployment": map[string]any{
				"latest": "gpt-4.1-blue", // model name != deployment name
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Path != "/openai/deployments/gpt-4.1-blue/chat/completions" {
		t.Errorf("Path = %q; want deployment 'gpt-4.1-blue'", out.Path)
	}
}

func TestAzureOpenAITranslate_LegacyPathPassthrough(t *testing.T) {
	t.Parallel()

	// Pre-Onda-2 clients that already send the Azure-shaped path keep working.
	in := Input{
		CanonicalPath: "/openai/deployments/gpt-4.1/chat/completions",
		RawQuery:      "api-version=2024-08-01-preview",
		Method:        "POST",
		Body:          []byte(`{"messages":[]}`),
		// Note: no provider_config required for legacy passthrough — it's verbatim.
		Config: endpoint.ProviderConfig{},
	}

	out, err := azureOpenAI{}.Translate(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Path != in.CanonicalPath {
		t.Errorf("Path = %q; want passthrough %q", out.Path, in.CanonicalPath)
	}
	if out.RawQuery != in.RawQuery {
		t.Errorf("RawQuery = %q; want passthrough %q", out.RawQuery, in.RawQuery)
	}
}

func TestAzureOpenAITranslate_UnsupportedPath(t *testing.T) {
	t.Parallel()

	_, err := azureOpenAI{}.Translate(Input{
		CanonicalPath: "/embeddings",
		Method:        "POST",
		Body:          []byte(`{"model":"gpt-4.1","input":"x"}`),
		Config: endpoint.ProviderConfig{
			"api_version":         "2025-01-01-preview",
			"model_to_deployment": map[string]any{"gpt-4.1": "gpt-4.1"},
		},
	})
	if !errors.Is(err, ErrUnsupportedOperation) {
		t.Errorf("err = %v; want ErrUnsupportedOperation", err)
	}
}

func TestAzureOpenAITranslate_UnknownModel(t *testing.T) {
	t.Parallel()

	_, err := azureOpenAI{}.Translate(Input{
		CanonicalPath: "/chat/completions",
		Method:        "POST",
		Body:          []byte(`{"model":"gpt-5"}`),
		Config: endpoint.ProviderConfig{
			"api_version": "2025-01-01-preview",
			"model_to_deployment": map[string]any{
				"gpt-4.1":      "gpt-4.1",
				"gpt-4.1-mini": "gpt-4.1-mini",
			},
		},
	})
	if !errors.Is(err, ErrUnknownModel) {
		t.Fatalf("err = %v; want ErrUnknownModel", err)
	}
	// The error message must enumerate available models so the client can
	// self-correct without round-tripping to the admin UI.
	if !strings.Contains(err.Error(), "gpt-4.1") || !strings.Contains(err.Error(), "gpt-4.1-mini") {
		t.Errorf("error message %q must list available models", err.Error())
	}
}

func TestAzureOpenAITranslate_MissingApiVersion(t *testing.T) {
	t.Parallel()

	_, err := azureOpenAI{}.Translate(Input{
		CanonicalPath: "/chat/completions",
		Method:        "POST",
		Body:          []byte(`{"model":"gpt-4.1"}`),
		Config: endpoint.ProviderConfig{
			"model_to_deployment": map[string]any{"gpt-4.1": "gpt-4.1"},
		},
	})
	if !errors.Is(err, ErrEndpointMisconfigured) {
		t.Errorf("err = %v; want ErrEndpointMisconfigured", err)
	}
}

func TestAzureOpenAITranslate_EmptyMapping(t *testing.T) {
	t.Parallel()

	_, err := azureOpenAI{}.Translate(Input{
		CanonicalPath: "/chat/completions",
		Method:        "POST",
		Body:          []byte(`{"model":"gpt-4.1"}`),
		Config: endpoint.ProviderConfig{
			"api_version":         "2025-01-01-preview",
			"model_to_deployment": map[string]any{},
		},
	})
	if !errors.Is(err, ErrEndpointMisconfigured) {
		t.Errorf("err = %v; want ErrEndpointMisconfigured", err)
	}
}

func TestAzureOpenAITranslate_BodyWithoutModel(t *testing.T) {
	t.Parallel()

	_, err := azureOpenAI{}.Translate(Input{
		CanonicalPath: "/chat/completions",
		Method:        "POST",
		Body:          []byte(`{"messages":[]}`),
		Config: endpoint.ProviderConfig{
			"api_version":         "2025-01-01-preview",
			"model_to_deployment": map[string]any{"gpt-4.1": "gpt-4.1"},
		},
	})
	if !errors.Is(err, ErrUnknownModel) {
		t.Errorf("err = %v; want ErrUnknownModel", err)
	}
}

func TestAzureOpenAITranslate_ClientQueryParamsPreserved(t *testing.T) {
	t.Parallel()

	out, err := azureOpenAI{}.Translate(Input{
		CanonicalPath: "/chat/completions",
		RawQuery:      "x-custom=hello&api-version=ignored-by-client",
		Method:        "POST",
		Body:          []byte(`{"model":"gpt-4.1"}`),
		Config: endpoint.ProviderConfig{
			"api_version":         "2025-01-01-preview",
			"model_to_deployment": map[string]any{"gpt-4.1": "gpt-4.1"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Endpoint config wins for api-version; other params survive.
	if !strings.Contains(out.RawQuery, "x-custom=hello") {
		t.Errorf("RawQuery = %q; want to preserve x-custom=hello", out.RawQuery)
	}
	if !strings.Contains(out.RawQuery, "api-version=2025-01-01-preview") {
		t.Errorf("RawQuery = %q; want api-version overridden to 2025-01-01-preview", out.RawQuery)
	}
	if strings.Contains(out.RawQuery, "ignored-by-client") {
		t.Errorf("RawQuery = %q; client's api-version value should be overridden", out.RawQuery)
	}
}

func TestFor_RegisteredKinds(t *testing.T) {
	t.Parallel()

	cases := []struct {
		kind     endpoint.ProviderKind
		expected bool
	}{
		{endpoint.ProviderAzureOpenAI, true},
		{endpoint.ProviderCustom, false},
		{endpoint.ProviderOpenAI, false},   // not implemented yet
		{endpoint.ProviderAnthropic, false},
		{endpoint.ProviderGemini, false},
		{endpoint.ProviderKind("unknown"), false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(string(tc.kind), func(t *testing.T) {
			t.Parallel()
			_, ok := For(tc.kind)
			if ok != tc.expected {
				t.Errorf("For(%q) = %v; want %v", tc.kind, ok, tc.expected)
			}
		})
	}
}
