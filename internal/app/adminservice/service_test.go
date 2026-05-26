package adminservice

import (
	"errors"
	"testing"

	"github.com/D4nRossi/ai-gateway/internal/domain/endpoint"
)

// TestDeriveKeyPrefix verifies that the prefix derivation is ASCII-only,
// truncates at keyPrefixMaxLen, and drops any byte outside [a-z0-9].
//
// References:
//   - ADR-0009 — DB-backed admin plane, api_keys.key_prefix is the index
//   - RFC 7230 §3.2.4 — Field Value Components (header encoding)
func TestDeriveKeyPrefix(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		appName string
		want    string
	}{
		{
			name:    "simple ASCII name",
			appName: "AppDemo",
			want:    "gwk_appdemo",
		},
		{
			name:    "mixed case and punctuation",
			appName: "My-Service-v2",
			want:    "gwk_myservicev",
		},
		{
			name:    "Unicode letters (ç, ã) are dropped",
			appName: "Aplicação",
			want:    "gwk_aplicao",
		},
		{
			name:    "Unicode + space, truncated at limit (10 chars)",
			appName: "Aplicação Demo",
			want:    "gwk_aplicaodem",
		},
		{
			name:    "all non-ASCII letters yields bare prefix",
			appName: "中文",
			want:    "gwk_",
		},
		{
			name:    "digits are preserved",
			appName: "App123",
			want:    "gwk_app123",
		},
		{
			name:    "name longer than limit is truncated",
			appName: "verylongapplicationname",
			want:    "gwk_verylongap",
		},
		{
			name:    "empty name returns bare prefix",
			appName: "",
			want:    "gwk_",
		},
		{
			name:    "name with only symbols returns bare prefix",
			appName: "!@#$%^&*()",
			want:    "gwk_",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := deriveKeyPrefix(tc.appName)
			if got != tc.want {
				t.Errorf("deriveKeyPrefix(%q) = %q; want %q", tc.appName, got, tc.want)
			}
		})
	}
}

// TestValidateProviderConfig covers the ADR-0017 contract: azure_openai
// endpoints must have api_version AND a non-empty model_to_deployment of
// non-empty string values. Other kinds accept any (or no) config.
func TestValidateProviderConfig(t *testing.T) {
	t.Parallel()

	validAzure := endpoint.ProviderConfig{
		"api_version": "2025-01-01-preview",
		"model_to_deployment": map[string]any{
			"gpt-4.1": "gpt-4.1",
		},
	}

	cases := []struct {
		name    string
		kind    endpoint.ProviderKind
		cfg     endpoint.ProviderConfig
		wantErr bool
	}{
		{"azure happy path", endpoint.ProviderAzureOpenAI, validAzure, false},
		{"azure missing api_version", endpoint.ProviderAzureOpenAI, endpoint.ProviderConfig{
			"model_to_deployment": map[string]any{"gpt-4.1": "gpt-4.1"},
		}, true},
		{"azure missing mapping", endpoint.ProviderAzureOpenAI, endpoint.ProviderConfig{
			"api_version": "2025-01-01-preview",
		}, true},
		{"azure empty mapping", endpoint.ProviderAzureOpenAI, endpoint.ProviderConfig{
			"api_version":         "2025-01-01-preview",
			"model_to_deployment": map[string]any{},
		}, true},
		{"azure mapping value is empty string", endpoint.ProviderAzureOpenAI, endpoint.ProviderConfig{
			"api_version":         "2025-01-01-preview",
			"model_to_deployment": map[string]any{"gpt-4.1": ""},
		}, true},
		{"azure mapping value is non-string", endpoint.ProviderAzureOpenAI, endpoint.ProviderConfig{
			"api_version":         "2025-01-01-preview",
			"model_to_deployment": map[string]any{"gpt-4.1": 42},
		}, true},
		{"azure mapping is not an object", endpoint.ProviderAzureOpenAI, endpoint.ProviderConfig{
			"api_version":         "2025-01-01-preview",
			"model_to_deployment": "string-not-object",
		}, true},
		{"custom accepts empty config", endpoint.ProviderCustom, endpoint.ProviderConfig{}, false},
		{"custom accepts any config", endpoint.ProviderCustom, endpoint.ProviderConfig{
			"anything": "goes",
		}, false},
		{"openai (no translator yet) accepts empty", endpoint.ProviderOpenAI, endpoint.ProviderConfig{}, false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateProviderConfig(tc.kind, tc.cfg)
			gotErr := err != nil
			if gotErr != tc.wantErr {
				t.Errorf("validateProviderConfig(%q) err=%v; wantErr=%v", tc.kind, err, tc.wantErr)
			}
			if gotErr && !errors.Is(err, ErrInvalidProviderConfig) {
				t.Errorf("error is not ErrInvalidProviderConfig: %v", err)
			}
		})
	}
}

// TestDeriveKeyPrefix_AlwaysASCII guards the invariant that no byte in the
// returned prefix is outside printable ASCII, no matter what bytes go in.
// This is the property that prevents the Postgres SQLSTATE 22021 failure
// path documented on deriveKeyPrefix.
func TestDeriveKeyPrefix_AlwaysASCII(t *testing.T) {
	t.Parallel()

	inputs := []string{
		"Aplicação",
		"Aplicação Demo",
		"中文测试",
		"App\x80\xff",
		"App\xe7\xe3",
		"日本語",
		"Ümlaut",
	}

	for _, in := range inputs {
		got := deriveKeyPrefix(in)
		for i := 0; i < len(got); i++ {
			b := got[i]
			if b < 0x21 || b > 0x7E {
				t.Errorf("deriveKeyPrefix(%q) = %q contains non-printable-ASCII byte 0x%02x at index %d",
					in, got, b, i)
			}
		}
	}
}
