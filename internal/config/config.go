package config

import (
	"errors"
	"fmt"
	"os"
	"regexp"

	"gopkg.in/yaml.v3"
)

// Config is the top-level gateway configuration.
type Config struct {
	Server             ServerConfig              `yaml:"server"`
	AzureOpenAI        AzureOpenAIConfig         `yaml:"azure_openai"`
	AzureContentSafety *AzureContentSafetyConfig `yaml:"azure_content_safety,omitempty"`
	Database           DatabaseConfig            `yaml:"database"`
	Logging            LoggingConfig             `yaml:"logging"`
	Models             []ModelConfig             `yaml:"models"`
	Applications       []ApplicationConfig       `yaml:"applications"`
}

// ServerConfig holds HTTP server tuning parameters.
type ServerConfig struct {
	Port                     int `yaml:"port"`
	ReadTimeoutSeconds       int `yaml:"read_timeout_seconds"`
	ReadHeaderTimeoutSeconds int `yaml:"read_header_timeout_seconds"`
	WriteTimeoutSeconds      int `yaml:"write_timeout_seconds"`
	IdleTimeoutSeconds       int `yaml:"idle_timeout_seconds"`
	MaxHeaderBytes           int `yaml:"max_header_bytes"`
}

// AzureOpenAIConfig holds the Azure OpenAI endpoint and authentication settings.
type AzureOpenAIConfig struct {
	Endpoint              string `yaml:"endpoint"`
	APIKey                string `yaml:"api_key"`
	APIVersion            string `yaml:"api_version"`
	RequestTimeoutSeconds int    `yaml:"request_timeout_seconds"`
}

// AzureContentSafetyConfig holds optional Azure Content Safety settings.
// When this section is absent from YAML the field is nil and Tier 3 falls
// back to local heuristics (fail-closed).
type AzureContentSafetyConfig struct {
	Endpoint               string `yaml:"endpoint"`
	APIKey                 string `yaml:"api_key"`
	APIVersion             string `yaml:"api_version"`
	PromptShieldTimeoutMs  int    `yaml:"prompt_shield_timeout_ms"`
	ContentSafetyTimeoutMs int    `yaml:"content_safety_timeout_ms"`
}

// DatabaseConfig holds PostgreSQL connection settings.
type DatabaseConfig struct {
	URL      string `yaml:"url"`
	MaxConns int    `yaml:"max_conns"`
	MinConns int    `yaml:"min_conns"`
}

// LoggingConfig controls log verbosity and output format.
type LoggingConfig struct {
	Level            string `yaml:"level"`
	Format           string `yaml:"format"`
	RawPromptLogging bool   `yaml:"raw_prompt_logging"`
}

// ModelConfig describes one LLM deployment available through the gateway.
type ModelConfig struct {
	PublicName         string  `yaml:"public_name"`
	Deployment         string  `yaml:"deployment"`
	Provider           string  `yaml:"provider"`
	CostInputPer1kBRL  float64 `yaml:"cost_input_per_1k_brl"`
	CostOutputPer1kBRL float64 `yaml:"cost_output_per_1k_brl"`
}

// ApplicationConfig represents one consumer application registered in the gateway.
type ApplicationConfig struct {
	Name             string   `yaml:"name"`
	KeyPrefix        string   `yaml:"key_prefix"`
	KeyHash          string   `yaml:"key_hash"`
	Tier             string   `yaml:"tier"`
	AllowedModels    []string `yaml:"allowed_models"`
	StreamingAllowed bool     `yaml:"streaming_allowed"`
	MaxRPM           int      `yaml:"max_rpm"`
	MaxTPM           int      `yaml:"max_tpm"`
	MonthlyBudgetBRL float64  `yaml:"monthly_budget_brl"`
}

// validTiers is the complete set of recognized tier identifiers.
var validTiers = map[string]struct{}{
	"tier_1": {},
	"tier_2": {},
	"tier_3": {},
}

// hexSHA256Re matches a valid 64-character lowercase SHA-256 hex digest.
var hexSHA256Re = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Load reads the YAML file at path, expands ${VAR} environment placeholders,
// unmarshals the result into Config, and calls Validate. Returns the validated
// Config or an error that combines all validation failures.
//
// Reasoning: env expansion happens before YAML parsing so secret placeholders
// never appear in the struct; this isolates secret handling to the OS layer.
//
// References:
//   - SPEC.md §4, §4.1
//   - CLAUDE.md §10.1 — configuration loading policy
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file %s: %w", path, err)
	}
	expanded := os.ExpandEnv(string(raw))
	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("parsing config YAML %s: %w", path, err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Validate checks all required fields and business invariants, collecting every
// violation into a single combined error so the operator sees the full list at once.
//
// Reasoning: fail-fast at boot — surfaces config mistakes before the server
// accepts any traffic, preventing hard-to-diagnose runtime failures.
//
// References:
//   - SPEC.md §4.1 — required validations list
func (c *Config) Validate() error {
	var errs []error

	if c.Server.Port < 1 || c.Server.Port > 65535 {
		errs = append(errs, fmt.Errorf("server.port must be 1–65535, got %d", c.Server.Port))
	}
	if c.AzureOpenAI.Endpoint == "" {
		errs = append(errs, errors.New("azure_openai.endpoint is required"))
	}
	if c.AzureOpenAI.APIKey == "" {
		errs = append(errs, errors.New("azure_openai.api_key is required"))
	}
	if c.Database.URL == "" {
		errs = append(errs, errors.New("database.url is required"))
	}
	if len(c.Models) == 0 {
		errs = append(errs, errors.New("at least one entry in models is required"))
	}
	if len(c.Applications) == 0 {
		errs = append(errs, errors.New("at least one entry in applications is required"))
	}

	modelIndex := make(map[string]struct{}, len(c.Models))
	for _, m := range c.Models {
		modelIndex[m.PublicName] = struct{}{}
	}

	for _, app := range c.Applications {
		if _, ok := validTiers[app.Tier]; !ok {
			errs = append(errs, fmt.Errorf(
				"application %q: tier %q is invalid; must be tier_1, tier_2, or tier_3",
				app.Name, app.Tier,
			))
		}
		if !hexSHA256Re.MatchString(app.KeyHash) {
			errs = append(errs, fmt.Errorf(
				"application %q: key_hash must be a 64-character lowercase hex SHA-256 digest",
				app.Name,
			))
		}
		for _, m := range app.AllowedModels {
			if _, ok := modelIndex[m]; !ok {
				errs = append(errs, fmt.Errorf(
					"application %q: allowed_model %q is not defined in the models list",
					app.Name, m,
				))
			}
		}
	}

	if cs := c.AzureContentSafety; cs != nil {
		if cs.Endpoint == "" {
			errs = append(errs, errors.New("azure_content_safety.endpoint is required when section is present"))
		}
		if cs.APIKey == "" {
			errs = append(errs, errors.New("azure_content_safety.api_key is required when section is present"))
		}
		if cs.APIVersion == "" {
			errs = append(errs, errors.New("azure_content_safety.api_version is required when section is present"))
		}
	}

	return errors.Join(errs...)
}

// ModelByName returns the ModelConfig whose PublicName matches name.
// Returns false if no model is found.
func (c *Config) ModelByName(name string) (ModelConfig, bool) {
	for _, m := range c.Models {
		if m.PublicName == name {
			return m, true
		}
	}
	return ModelConfig{}, false
}
