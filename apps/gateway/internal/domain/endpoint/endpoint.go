// Package endpoint defines the core domain types for generic HTTP proxy endpoints
// and their upstream targets. These are pure value objects with no infrastructure dependency.
//
// A ProxyEndpoint is a named, slug-addressable route that aggregates one or more Targets.
// The proxy engine selects a Target on each request using the endpoint's LBStrategy.
//
// References:
//   - ADR-0010 — generic HTTP proxy engine
//   - ADR-0012 — AES-256-GCM for target credentials at rest
//   - ADR-0013 — load balancing strategies
//   - ADR-0015 — domain/app/infra layering
//   - ADR-0020 — credential storage mode per target (aes/kv/both)
package endpoint

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound is returned by Repository methods when the requested ProxyEndpoint
// or Target does not exist. Callers use errors.Is(err, endpoint.ErrNotFound).
var ErrNotFound = errors.New("endpoint not found")

// LBStrategy names the algorithm used to select a target for each incoming request.
type LBStrategy string

const (
	// LBRoundRobin distributes requests evenly across active targets in sequence.
	LBRoundRobin LBStrategy = "round_robin"

	// LBWeightedRR distributes requests proportionally to each target's Weight.
	LBWeightedRR LBStrategy = "weighted_round_robin"

	// LBRandom selects a target uniformly at random on each request.
	LBRandom LBStrategy = "random"

	// LBLeastConnections routes to the target with the fewest in-flight requests.
	// Counters are per-process (not globally consistent in multi-replica deployments).
	LBLeastConnections LBStrategy = "least_connections"

	// LBIPHash selects a target deterministically based on the client's IP address,
	// providing sticky routing for stateful upstream sessions.
	LBIPHash LBStrategy = "ip_hash"
)

// ProviderKind classifies a ProxyEndpoint by the upstream service family it
// proxies for. The value is metadata only — the proxy engine remains a pure
// HTTP passthrough (ADR-0016). The frontend uses this tag to render provider
// branding, pre-fill base URLs and auth methods, and surface analytics.
//
// `custom` is the catch-all for any HTTP API not in the curated catalog and
// preserves the original passthrough behavior.
type ProviderKind string

const (
	ProviderAzureOpenAI ProviderKind = "azure_openai"
	ProviderOpenAI      ProviderKind = "openai"
	ProviderAnthropic   ProviderKind = "anthropic"
	ProviderGemini      ProviderKind = "gemini"
	ProviderMistral     ProviderKind = "mistral"
	ProviderCohere      ProviderKind = "cohere"
	ProviderGroq        ProviderKind = "groq"
	ProviderTogether    ProviderKind = "together"
	ProviderOllama      ProviderKind = "ollama"
	ProviderVLLM        ProviderKind = "vllm"
	ProviderCustom      ProviderKind = "custom"
)

// validProviders mirrors the CHECK constraint on proxy_endpoints.provider_kind
// (migration 005). Update both together when adding a provider.
var validProviders = map[ProviderKind]struct{}{
	ProviderAzureOpenAI: {},
	ProviderOpenAI:      {},
	ProviderAnthropic:   {},
	ProviderGemini:      {},
	ProviderMistral:     {},
	ProviderCohere:      {},
	ProviderGroq:        {},
	ProviderTogether:    {},
	ProviderOllama:      {},
	ProviderVLLM:        {},
	ProviderCustom:      {},
}

// Valid reports whether p is a known provider kind. Empty string is invalid;
// callers should default to ProviderCustom when migrating older data.
func (p ProviderKind) Valid() bool {
	_, ok := validProviders[p]
	return ok
}

// AuthType names the authentication method the proxy should apply when forwarding
// a request to a Target.
type AuthType string

const (
	// AuthNone forwards the request with no additional authentication.
	AuthNone AuthType = "none"

	// AuthBearerToken injects "Authorization: Bearer {Token}" into the forwarded request.
	AuthBearerToken AuthType = "bearer_token"

	// AuthAPIKeyHeader injects a custom header ({Header}: {Value}) into the forwarded request.
	AuthAPIKeyHeader AuthType = "api_key_header"

	// AuthBasic injects RFC 7617 HTTP Basic auth using Username and Password.
	AuthBasic AuthType = "basic_auth"
)

// TargetAuth holds the (decrypted) credentials the proxy must inject when forwarding
// to a Target. Only fields relevant to the chosen AuthType are populated.
//
// Reasoning: credentials are stored encrypted in the DB (ADR-0012). The infra layer
// decrypts them before populating this struct; the domain layer and proxy engine
// work exclusively with plaintext in memory.
type TargetAuth struct {
	// Type determines which fields below are meaningful.
	Type AuthType

	// Token is used when Type = AuthBearerToken.
	Token string

	// Header is the custom header name used when Type = AuthAPIKeyHeader.
	Header string
	// Value is the custom header value used when Type = AuthAPIKeyHeader.
	Value string

	// Username and Password are used when Type = AuthBasic.
	Username string
	Password string
}

// CredentialStorageMode controls where a Target's credentials are persisted
// and how the proxy resolves them at request time (ADR-0020).
//
//   - CredentialModeAES (default): credentials live in proxy_targets.auth_config_enc
//     and are decrypted with the AES master key (ADR-0012). Status quo behavior.
//   - CredentialModeKV: credentials live in Azure Key Vault under KVSecretName.
//     The resolver fetches with a 200 ms timeout; failure surfaces to the caller
//     (no fallback). Auth is empty/AuthNone on load.
//   - CredentialModeBoth: KV is authoritative; auth_config_enc is a freshness
//     cache. The resolver tries KV first (200 ms timeout); on error or timeout
//     it falls back to the decrypted AES value and logs kv_fallback_used.
type CredentialStorageMode string

const (
	CredentialModeAES  CredentialStorageMode = "aes"
	CredentialModeKV   CredentialStorageMode = "kv"
	CredentialModeBoth CredentialStorageMode = "both"
)

// validCredentialModes mirrors the CHECK constraint on
// proxy_targets.credential_storage_mode (migration 011).
var validCredentialModes = map[CredentialStorageMode]struct{}{
	CredentialModeAES:  {},
	CredentialModeKV:   {},
	CredentialModeBoth: {},
}

// Valid reports whether m is a recognised credential storage mode.
// Empty string is invalid; callers should default to CredentialModeAES
// when migrating older payloads that predate ADR-0020.
func (m CredentialStorageMode) Valid() bool {
	_, ok := validCredentialModes[m]
	return ok
}

// Target is a single upstream URL that the proxy may route to.
// It belongs to exactly one ProxyEndpoint.
type Target struct {
	// ID is the database-assigned surrogate key.
	ID int64

	// EndpointID is the FK referencing the parent ProxyEndpoint.
	EndpointID int64

	// URL is the full upstream base URL (e.g. "https://speech.azure.com").
	// The proxy appends the original request path and query string.
	URL string

	// Weight controls relative traffic share in the weighted_round_robin strategy.
	// Ignored by other strategies. Must be > 0.
	Weight int

	// Auth holds the decrypted credentials for this target (ADR-0012). In
	// CredentialModeKV the repository leaves this zero (AuthNone) and the
	// resolver fetches from Key Vault. In CredentialModeBoth this is the
	// freshness cache used when the KV read times out.
	Auth TargetAuth

	// CredentialStorageMode selects where Auth comes from at request time.
	// Default CredentialModeAES preserves the pre-ADR-0020 behavior.
	CredentialStorageMode CredentialStorageMode

	// KVSecretName is the Key Vault secret name backing this target's
	// credentials. Required when CredentialStorageMode is CredentialModeKV
	// or CredentialModeBoth; empty otherwise. Default naming convention is
	// "gateway-target-{uuid_v7}" generated at migration/creation time and
	// kept immutable thereafter; custom names are accepted by the admin API.
	KVSecretName string

	// Active controls whether this target is eligible for selection.
	Active bool

	CreatedAt time.Time
}

// ProxyEndpoint is the gateway's representation of a proxied external service.
// It is accessible to authorized consumer applications at /v1/proxy/{Slug}.
type ProxyEndpoint struct {
	// ID is the database-assigned surrogate key.
	ID int64

	// Slug is the URL-safe identifier used in /v1/proxy/{slug}.
	Slug string

	// Name is the human-readable display name shown in the admin UI.
	Name string

	// ProviderKind tags this endpoint with the upstream service family
	// (azure_openai, openai, anthropic, …). Metadata-only in ADR-0016; activated
	// by ADR-0017 to drive path translation for non-custom kinds.
	ProviderKind ProviderKind

	// ProviderConfig holds the kind-specific configuration consumed by the path
	// translator (ADR-0017). The shape is determined by ProviderKind:
	//
	//   azure_openai: {"api_version": "...", "model_to_deployment": {model: deployment}}
	//   custom:       ignored — translator is no-op
	//
	// Persisted as JSONB. Validation per kind happens in internal/app/adminservice;
	// the database accepts any valid JSON. Empty (`{}`) is the safe default for
	// pre-translation rows and for kinds that don't need extra config.
	ProviderConfig ProviderConfig

	// LBStrategy controls how Targets are selected. Default: round_robin.
	LBStrategy LBStrategy

	// MaxRPS is the per-endpoint requests-per-second cap. 0 = no limit.
	MaxRPS int

	// MaxMonthlyRequests is the monthly request count cap. 0 = no limit.
	MaxMonthlyRequests int64

	// Active controls whether the endpoint is reachable. Soft-delete semantics.
	Active bool

	// Targets is the list of active upstream URLs for this endpoint.
	// Populated by the repository on full-load queries.
	Targets []Target

	CreatedAt time.Time
	UpdatedAt time.Time
}

// ProviderConfig is the in-memory representation of proxy_endpoints.provider_config
// (JSONB). Stored as a map to keep the schema open per kind without a tagged
// union; concrete typed accessors live in internal/proxy/translator.
//
// Reasoning: a Go interface with one struct per kind would be cleaner at use
// site but forces the repository to know all kinds at compile time. A loose
// map keeps the persistence layer pure persistence and lets the translator
// layer own the schema decisions per kind.
type ProviderConfig map[string]any

// Repository defines the persistence contract for ProxyEndpoint, Target, and grant entities.
// All methods accept a context.Context as first argument (CLAUDE.md §5.5).
// The implementation lives in internal/infra/postgres/endpointrepo.go.
//
// References:
//   - ADR-0010 — generic HTTP proxy engine
//   - ADR-0015 — repository interfaces belong in the domain package
type Repository interface {
	// Create persists a new ProxyEndpoint (without targets) and returns it with ID set.
	Create(ctx context.Context, ep ProxyEndpoint) (ProxyEndpoint, error)

	// Get retrieves a ProxyEndpoint by ID including its active Targets.
	// Returns ErrNotFound if no row matches.
	Get(ctx context.Context, id int64) (ProxyEndpoint, error)

	// GetBySlug retrieves an active ProxyEndpoint by slug including its active Targets.
	// Used by the proxy engine on every request.
	// Returns ErrNotFound if no active row matches.
	GetBySlug(ctx context.Context, slug string) (ProxyEndpoint, error)

	// List returns all ProxyEndpoints (active and inactive), without Targets.
	List(ctx context.Context) ([]ProxyEndpoint, error)

	// Update persists changes to an existing ProxyEndpoint. ID must be set.
	// Returns ErrNotFound if no row matches.
	Update(ctx context.Context, ep ProxyEndpoint) (ProxyEndpoint, error)

	// Delete soft-deletes a ProxyEndpoint (active=false).
	// Returns ErrNotFound if no row matches.
	Delete(ctx context.Context, id int64) error

	// AddTarget persists a new Target for an endpoint and returns it with ID set.
	AddTarget(ctx context.Context, target Target) (Target, error)

	// UpdateTarget persists changes to an existing Target. ID must be set.
	UpdateTarget(ctx context.Context, target Target) (Target, error)

	// RemoveTarget soft-deletes a Target (active=false).
	RemoveTarget(ctx context.Context, targetID int64) error

	// Grant allows an application to call a proxy endpoint. Idempotent.
	Grant(ctx context.Context, applicationID, endpointID int64) error

	// Revoke removes an application's access to a proxy endpoint.
	Revoke(ctx context.Context, applicationID, endpointID int64) error

	// HasGrant reports whether the application has been granted access to the endpoint.
	HasGrant(ctx context.Context, applicationID, endpointID int64) (bool, error)

	// ListGrantedApplicationIDs returns all application IDs granted access to an endpoint.
	ListGrantedApplicationIDs(ctx context.Context, endpointID int64) ([]int64, error)

	// ListGrantedEndpointIDs returns all endpoint IDs that an application has been
	// granted access to. Used by the admin detail page to render the access matrix.
	ListGrantedEndpointIDs(ctx context.Context, applicationID int64) ([]int64, error)
}
