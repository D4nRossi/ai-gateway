package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// SecretResolver fetches secrets by name. Implemented by
// *internal/infra/keyvault.Client; declared as an interface here to keep
// internal/config decoupled from the concrete Azure SDK (ADR-0018).
//
// When Load is called with a non-nil resolver, ${kv:NAME} references in the
// YAML are replaced with the resolved value. When resolver is nil, any
// ${kv:NAME} reference is a fatal config error.
type SecretResolver interface {
	Get(ctx context.Context, name string) (string, error)
}

// kvRefRe matches a ${kv:NAME} reference inside the raw YAML.
// NAME is constrained to Key Vault's allowed character set: [a-zA-Z0-9-].
//
// References:
//   - https://learn.microsoft.com/azure/key-vault/general/about-keys-secrets-certificates#vault-name-and-object-name
var kvRefRe = regexp.MustCompile(`\$\{kv:([A-Za-z0-9\-]+)\}`)

// Config is the top-level gateway configuration.
type Config struct {
	Server             ServerConfig              `yaml:"server"`
	AzureOpenAI        AzureOpenAIConfig         `yaml:"azure_openai"`
	AzureContentSafety *AzureContentSafetyConfig `yaml:"azure_content_safety,omitempty"`
	AzureLanguage      *AzureLanguageConfig      `yaml:"azure_language,omitempty"`
	Database           DatabaseConfig            `yaml:"database"`
	Logging            LoggingConfig             `yaml:"logging"`
	Models             []ModelConfig             `yaml:"models"`
	Applications       []ApplicationConfig       `yaml:"applications"`
}

// AzureLanguageConfig holds optional Azure AI Language PII settings.
// When this section is absent, Tier 2 and Tier 3 skip the remote PII step
// (the regex-based masking still runs). When present, Tier 2 runs fail-open
// and Tier 3 runs fail-closed (ADR-0019).
type AzureLanguageConfig struct {
	Endpoint   string `yaml:"endpoint"`
	APIKey     string `yaml:"api_key"`
	APIVersion string `yaml:"api_version"`
	// TimeoutMs is the per-request budget for the Analyze Text call.
	// Default 1500ms (set in main.go when 0).
	TimeoutMs int `yaml:"timeout_ms"`
	// Language is the BCP-47 tag for the model selection. Default "pt-BR"
	// (applied in the client when empty).
	Language string `yaml:"language"`
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

// DatabaseConfig holds SQL Server connection settings (ADR-0022).
//
// The previous PostgreSQL `url` field was replaced by structured fields so
// the secret (password) can come isolated from ${kv:...} while the rest of
// the connection metadata stays in plain YAML. The driver microsoft/go-mssqldb
// is the only one supported here.
type DatabaseConfig struct {
	// Driver currently must be "sqlserver". Kept as a field for forward
	// compatibility (e.g. a future "sqlserver-azure" mode with token auth).
	Driver string `yaml:"driver"`

	// Host is the SQL Server hostname (e.g. BRSPVPDEV003).
	Host string `yaml:"host"`

	// Port is the TCP port; defaults to 1433 when 0.
	Port int `yaml:"port"`

	// Database is the catalog name (e.g. AzureAI_Gateway_hom).
	Database string `yaml:"database"`

	// User is the SQL Server login (e.g. usr_sist_AzureAI_Gateway_hom).
	User string `yaml:"user"`

	// Password is the secret for User. MUST come from a Key Vault reference
	// (${kv:NAME}) — never plaintext in YAML. The Validate() method ensures
	// the value is non-empty after ${kv:...} resolution.
	Password string `yaml:"password"`

	// Schema is the SQL Server schema where the gateway's own tables live
	// (default "gogateway"). All queries qualify tables explicitly with this
	// schema. The schema_migrations bookkeeping table created by
	// golang-migrate is intentionally NOT placed in this schema — it stays
	// in the user's default schema (typically dbo) so a partially-applied
	// migration history can still be inspected even if the gogateway schema
	// is dropped manually.
	Schema string `yaml:"schema"`

	// Encrypt requests TLS encryption on the wire. Default true.
	Encrypt bool `yaml:"encrypt"`

	// TrustServerCertificate disables server certificate verification.
	// Useful for homologation where the SQL Server uses a self-signed cert;
	// MUST be false in production (operator's responsibility).
	TrustServerCertificate bool `yaml:"trust_server_certificate"`

	// MaxConns caps the connection pool size (db.SetMaxOpenConns).
	MaxConns int `yaml:"max_conns"`

	// MinConns hints the minimum idle connections (db.SetMaxIdleConns).
	// SQL Server's connection pool semantics are slightly different from
	// pgxpool — there is no hard minimum; this value caps idle connections.
	MinConns int `yaml:"min_conns"`

	// EncryptionKeyHex is a 64-character lowercase hex string (32 bytes) used
	// as the AES-256-GCM key for encrypting proxy target credentials at rest
	// (ADR-0012). Must be set via environment variable (e.g. ${kv:DB-ENCRYPTION-KEY}).
	// Never log or include this value in error messages.
	EncryptionKeyHex string `yaml:"encryption_key_hex"`
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

// hexAES256Re matches a valid 64-character lowercase hex string (32 bytes = AES-256 key).
// The pattern is identical to hexSHA256Re; a separate variable makes intent explicit.
var hexAES256Re = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Load reads the YAML file at path, resolves placeholders, unmarshals the
// result into Config, and calls Validate. Returns the validated Config or an
// error that combines all resolution and validation failures.
//
// Placeholder resolution order:
//  1. ${VAR} — expanded from process environment via os.ExpandEnv.
//  2. ${kv:NAME} — fetched from `secrets` (typically backed by Azure Key
//     Vault). All references are resolved before unmarshal; all errors are
//     collected via errors.Join so the operator sees the full list at once.
//
// When secrets is nil, any ${kv:NAME} present in the YAML is treated as a
// fatal config error (ADR-0018, fail-fast on missing KV).
//
// Reasoning: placeholder resolution happens before YAML parsing so secret
// values never appear in the struct as ${...} markers and so the parser
// sees a clean document. Env expansion runs first to allow patterns like
// "${kv:${SECRET_NAME_VAR}}" if ever needed (currently no caller does it).
//
// References:
//   - SPEC.md §4, §4.1
//   - CLAUDE.md §10.1 — configuration loading policy
//   - ADR-0018 — Azure Key Vault como provider de segredos
func Load(ctx context.Context, path string, secrets SecretResolver) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file %s: %w", path, err)
	}

	expanded := expandEnvPreservingKV(string(raw))

	resolved, err := resolveKVRefs(ctx, expanded, secrets)
	if err != nil {
		return nil, fmt.Errorf("resolving Key Vault references in %s: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal([]byte(resolved), &cfg); err != nil {
		return nil, fmt.Errorf("parsing config YAML %s: %w", path, err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// expandEnvPreservingKV is os.ExpandEnv with one difference: ${kv:NAME}
// markers are passed through verbatim instead of being looked up in the
// process environment (which would always miss and substitute empty string,
// silently destroying the placeholder before resolveKVRefs sees it).
//
// Implementation note: os.Expand calls the callback for every ${...} or $word
// match. Returning "${name}" for a kv: prefix preserves the marker so the
// downstream KV resolver can do its job. Everything else delegates to
// os.Getenv exactly as os.ExpandEnv would.
func expandEnvPreservingKV(s string) string {
	return os.Expand(s, func(name string) string {
		if strings.HasPrefix(name, "kv:") {
			return "${" + name + "}"
		}
		return os.Getenv(name)
	})
}

// resolveKVRefs scans yaml for ${kv:NAME} markers and replaces each with the
// value fetched from secrets. Unique names are fetched once even if referenced
// multiple times. All resolution errors are collected and returned as a
// single joined error so the operator gets the full picture in one shot
// instead of fix-restart-fix-restart cycles.
func resolveKVRefs(ctx context.Context, yaml string, secrets SecretResolver) (string, error) {
	matches := kvRefRe.FindAllStringSubmatch(yaml, -1)
	if len(matches) == 0 {
		return yaml, nil
	}

	if secrets == nil {
		// Build a unique, sorted list of names for a readable error message.
		seen := map[string]struct{}{}
		var names []string
		for _, m := range matches {
			if _, ok := seen[m[1]]; ok {
				continue
			}
			seen[m[1]] = struct{}{}
			names = append(names, m[1])
		}
		return "", fmt.Errorf(
			"config references %d Key Vault secret(s) (%v) but KEYVAULT_URI is not configured",
			len(names), names,
		)
	}

	// Fetch unique names once; build a substitution table.
	subs := make(map[string]string)
	var fetchErrs []error
	for _, m := range matches {
		name := m[1]
		if _, ok := subs[name]; ok {
			continue
		}
		val, err := secrets.Get(ctx, name)
		if err != nil {
			fetchErrs = append(fetchErrs, err)
			continue
		}
		subs[name] = val
	}
	if len(fetchErrs) > 0 {
		return "", errors.Join(fetchErrs...)
	}

	// Replace in a single pass — ReplaceAllStringFunc reads the regex output,
	// looks up the captured name in subs, and substitutes the value wrapped in
	// YAML single quotes.
	//
	// Reasoning: bare scalar substitution corrompe o parse quando o segredo
	// contém qualquer um dos caracteres reservados do YAML — `#` (início de
	// comentário quando seguido de espaço), `!` (tag indicator), `@` e `` ` ``
	// (reserved), `:` (separador key:value), `[]{}` (flow style), `&*` (anchor
	// / alias), `|>` (block scalars), `?` (complex key marker), espaços líderes
	// etc. Senhas reais frequentemente contêm vários desses.
	//
	// YAML single-quoted strings têm a propriedade mais simples possível: TODO
	// caractere literal é aceito, EXCETO o próprio apóstrofo, que precisa ser
	// duplicado. Forçando aspas simples na substituição, qualquer secret é
	// armazenado como string opaca e o tipo nunca colide com YAML booleans
	// (`yes`, `no`, `on`, `off`) ou numbers — comportamento desejado para
	// segredos.
	//
	// Limitação conhecida: newlines literais (`\n`) num secret sofrem YAML
	// line folding e viram espaço. Senhas e API keys realistas não têm
	// newlines, então essa limitação é aceitável; secrets multiline exigiriam
	// double-quoted style com escape adicional (`\` e `"`) e ficam fora do
	// escopo aqui. Coberto por TestResolveKVRefs_SpecialCharactersInSecret.
	//
	// Pré-condição: o `${kv:NAME}` no YAML deve ser bare scalar (sem aspas em
	// volta). Como a convenção do gateway.yaml é exatamente essa, há zero
	// regressão. Se um dia alguém escrever `password: "${kv:NAME}"`, a
	// substituição resultaria em `password: "'valor'"` (string literal com
	// aspas) — pegamos isso em revisão.
	out := kvRefRe.ReplaceAllStringFunc(yaml, func(match string) string {
		sub := kvRefRe.FindStringSubmatch(match)
		// sub[0] is the full match, sub[1] is the capture group (NAME).
		val := subs[sub[1]]
		return "'" + strings.ReplaceAll(val, "'", "''") + "'"
	})
	return out, nil
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
	// Database — SQL Server estruturado (ADR-0022).
	if c.Database.Driver == "" {
		c.Database.Driver = "sqlserver"
	}
	if c.Database.Driver != "sqlserver" {
		errs = append(errs, fmt.Errorf("database.driver %q is not supported; only \"sqlserver\" is allowed (ADR-0022)", c.Database.Driver))
	}
	if c.Database.Host == "" {
		errs = append(errs, errors.New("database.host is required"))
	}
	if c.Database.Port == 0 {
		c.Database.Port = 1433
	} else if c.Database.Port < 1 || c.Database.Port > 65535 {
		errs = append(errs, fmt.Errorf("database.port must be 1–65535, got %d", c.Database.Port))
	}
	if c.Database.Database == "" {
		errs = append(errs, errors.New("database.database is required"))
	}
	if c.Database.User == "" {
		errs = append(errs, errors.New("database.user is required"))
	}
	if c.Database.Password == "" {
		errs = append(errs, errors.New("database.password is required (use ${kv:AzureAIGateway-DB-Password-hom} from Key Vault)"))
	}
	if c.Database.Schema == "" {
		c.Database.Schema = "gogateway"
	}
	if !hexAES256Re.MatchString(c.Database.EncryptionKeyHex) {
		errs = append(errs, errors.New("database.encryption_key_hex must be a 64-character lowercase hex string (32 bytes for AES-256)"))
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

	if al := c.AzureLanguage; al != nil {
		if al.Endpoint == "" {
			errs = append(errs, errors.New("azure_language.endpoint is required when section is present"))
		}
		if al.APIKey == "" {
			errs = append(errs, errors.New("azure_language.api_key is required when section is present"))
		}
		if al.APIVersion == "" {
			errs = append(errs, errors.New("azure_language.api_version is required when section is present"))
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
