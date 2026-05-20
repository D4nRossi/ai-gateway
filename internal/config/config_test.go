package config

import (
	"strings"
	"testing"
)

// baseValidConfig returns a Config that passes Validate() with zero modifications,
// used as the baseline for mutation-based test cases.
func baseValidConfig() Config {
	return Config{
		Server: ServerConfig{Port: 8080},
		AzureOpenAI: AzureOpenAIConfig{
			Endpoint:   "https://example.openai.azure.com",
			APIKey:     "test-key",
			APIVersion: "2024-10-21",
		},
		Database: DatabaseConfig{URL: "postgres://u:p@localhost/db"},
		Models: []ModelConfig{
			{PublicName: "gpt-4.1-nano", Deployment: "nano-deploy", Provider: "azure"},
		},
		Applications: []ApplicationConfig{
			{
				Name:          "TestApp",
				KeyPrefix:     "gwk_test",
				KeyHash:       "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				Tier:          "tier_1",
				AllowedModels: []string{"gpt-4.1-nano"},
			},
		},
	}
}

// TestValidate_ValidConfig confirms that a fully populated config passes validation.
//
// References:
//   - SPEC.md §4.1 — required validations
func TestValidate_ValidConfig(t *testing.T) {
	t.Parallel()
	cfg := baseValidConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected no error for valid config, got: %v", err)
	}
}

// TestValidate_InvalidPort verifies that out-of-range ports are rejected.
func TestValidate_InvalidPort(t *testing.T) {
	t.Parallel()

	for _, port := range []int{0, -1, 65536, 99999} {
		port := port
		t.Run("port="+itoa(port), func(t *testing.T) {
			t.Parallel()
			cfg := baseValidConfig()
			cfg.Server.Port = port
			err := cfg.Validate()
			if err == nil {
				t.Fatalf("expected error for port %d, got nil", port)
			}
			if !strings.Contains(err.Error(), "server.port") {
				t.Errorf("error should mention server.port, got: %v", err)
			}
		})
	}
}

// TestValidate_MissingEndpoint verifies that an empty Azure endpoint is rejected.
func TestValidate_MissingEndpoint(t *testing.T) {
	t.Parallel()
	cfg := baseValidConfig()
	cfg.AzureOpenAI.Endpoint = ""
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for missing azure endpoint, got nil")
	}
	if !strings.Contains(err.Error(), "endpoint") {
		t.Errorf("error should mention endpoint, got: %v", err)
	}
}

// TestValidate_MissingAPIKey verifies that an empty API key is rejected.
func TestValidate_MissingAPIKey(t *testing.T) {
	t.Parallel()
	cfg := baseValidConfig()
	cfg.AzureOpenAI.APIKey = ""
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for missing api_key, got nil")
	}
}

// TestValidate_NoModels verifies that at least one model is required.
func TestValidate_NoModels(t *testing.T) {
	t.Parallel()
	cfg := baseValidConfig()
	cfg.Models = nil
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for empty models list, got nil")
	}
	if !strings.Contains(err.Error(), "models") {
		t.Errorf("error should mention models, got: %v", err)
	}
}

// TestValidate_InvalidTier verifies that an unrecognised tier string is rejected.
func TestValidate_InvalidTier(t *testing.T) {
	t.Parallel()
	cfg := baseValidConfig()
	cfg.Applications[0].Tier = "tier_99"
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for invalid tier, got nil")
	}
	if !strings.Contains(err.Error(), "tier") {
		t.Errorf("error should mention tier, got: %v", err)
	}
}

// TestValidate_MalformedKeyHash verifies that a key_hash that is not 64 lowercase hex chars is rejected.
//
// References:
//   - SPEC.md §4.1 — "key_hash must be a 64-character lowercase hex SHA-256 digest"
func TestValidate_MalformedKeyHash(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		hash string
	}{
		{name: "too short", hash: "abc123"},
		{name: "too long", hash: strings.Repeat("a", 65)},
		{name: "placeholder value", hash: "<sha256 hex>"},
		{name: "uppercase hex", hash: strings.Repeat("A", 64)},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := baseValidConfig()
			cfg.Applications[0].KeyHash = tc.hash
			err := cfg.Validate()
			if err == nil {
				t.Fatalf("expected error for key_hash=%q, got nil", tc.hash)
			}
			if !strings.Contains(err.Error(), "key_hash") {
				t.Errorf("error should mention key_hash, got: %v", err)
			}
		})
	}
}

// TestValidate_AllowedModelNotInCatalog verifies that an app referencing an
// undefined model is rejected.
func TestValidate_AllowedModelNotInCatalog(t *testing.T) {
	t.Parallel()
	cfg := baseValidConfig()
	cfg.Applications[0].AllowedModels = []string{"gpt-4.1-mini"} // not in models list
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for model not in catalog, got nil")
	}
}

// TestValidate_ContentSafetyRequiresAllFields verifies that a partial
// azure_content_safety block (missing endpoint or key) is rejected.
func TestValidate_ContentSafetyRequiresAllFields(t *testing.T) {
	t.Parallel()

	t.Run("missing endpoint", func(t *testing.T) {
		t.Parallel()
		cfg := baseValidConfig()
		cfg.AzureContentSafety = &AzureContentSafetyConfig{
			APIKey:     "key",
			APIVersion: "2024-09-01",
		}
		if err := cfg.Validate(); err == nil {
			t.Fatal("expected error for missing CS endpoint")
		}
	})

	t.Run("missing api_key", func(t *testing.T) {
		t.Parallel()
		cfg := baseValidConfig()
		cfg.AzureContentSafety = &AzureContentSafetyConfig{
			Endpoint:   "https://cs.example.com",
			APIVersion: "2024-09-01",
		}
		if err := cfg.Validate(); err == nil {
			t.Fatal("expected error for missing CS api_key")
		}
	})
}

// TestModelByName verifies prefix-exact lookup in the model catalog.
func TestModelByName(t *testing.T) {
	t.Parallel()
	cfg := baseValidConfig()
	cfg.Models = append(cfg.Models, ModelConfig{
		PublicName: "gpt-4.1-mini",
		Deployment: "mini-deploy",
		Provider:   "azure",
	})

	t.Run("found", func(t *testing.T) {
		t.Parallel()
		m, ok := cfg.ModelByName("gpt-4.1-nano")
		if !ok {
			t.Fatal("expected ok=true for gpt-4.1-nano")
		}
		if m.Deployment != "nano-deploy" {
			t.Errorf("Deployment = %q; want %q", m.Deployment, "nano-deploy")
		}
	})

	t.Run("not found", func(t *testing.T) {
		t.Parallel()
		_, ok := cfg.ModelByName("gpt-5.0-turbo")
		if ok {
			t.Error("expected ok=false for unknown model name")
		}
	})
}

// itoa converts int to string without importing strconv.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	buf := make([]byte, 0, 10)
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	if neg {
		buf = append([]byte{'-'}, buf...)
	}
	return string(buf)
}
