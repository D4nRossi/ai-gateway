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

	// Auth holds the decrypted credentials for this target (ADR-0012).
	Auth TargetAuth

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
}
