// Package application defines the core domain types for consumer applications and their
// API keys. These are pure value objects with no dependency on infrastructure.
//
// In Phase 1, applications were loaded from YAML config (SPEC.md §4). In V2, the
// authoritative store is PostgreSQL (ADR-0009). This package defines the types that
// both layers share.
//
// References:
//   - ADR-0009 — DB-backed admin plane
//   - ADR-0015 — domain/app/infra layering
//   - SPEC.md §5.2 — AppPolicy (Phase 1 equivalent)
package application

import "time"

// TierLevel represents the security tier assigned to an application.
// Each tier enables a different set of guardrails (SPEC.md §5.3).
type TierLevel string

const (
	Tier1 TierLevel = "tier_1"
	Tier2 TierLevel = "tier_2"
	Tier3 TierLevel = "tier_3"
)

// Application is the V2 domain entity for a consumer application registered in the gateway.
// It supersedes the Phase 1 AppPolicy struct for admin-plane operations.
//
// Reasoning: AppPolicy in auth/policy.go is kept for Phase 1 middleware compatibility.
// Application is the DB-backed counterpart used by the Admin API and, in a future migration,
// by the auth middleware itself (ADR-0009).
type Application struct {
	// ID is the database-assigned surrogate key.
	ID int64

	// Name is the unique human-readable application identifier.
	// Used in logs, audit events, and rate-limit keys.
	Name string

	// Tier controls which security guardrails are applied to this application's requests.
	Tier TierLevel

	// AllowedModels is the list of model public names this application may request.
	// The gateway returns 403 for any model not in this list.
	AllowedModels []string

	// StreamingAllowed controls whether this application may use stream: true.
	StreamingAllowed bool

	// MaxRPM is the requests-per-minute cap. 0 means no gateway-level limit.
	MaxRPM int

	// MaxTPM is the tokens-per-minute cap. 0 means no gateway-level limit.
	MaxTPM int

	// MonthlyBudgetBRL is the maximum estimated spend per calendar month in BRL.
	// 0 means no budget limit.
	MonthlyBudgetBRL float64

	// Active controls whether this application may authenticate. Soft-delete semantics.
	Active bool

	CreatedAt time.Time
	UpdatedAt time.Time
}

// APIKey holds the authentication credential for one Application.
// There is at most one active APIKey per Application at any time (ADR-0009 response C).
//
// The raw token is never stored — only its SHA-256 hex digest (KeyHash). The gateway
// generates the raw token once on creation/rotation and returns it in the API response.
type APIKey struct {
	// ID is the database-assigned surrogate key.
	ID int64

	// ApplicationID is the FK referencing the owning Application.
	ApplicationID int64

	// KeyPrefix is the gwk_{name}_ portion used for O(1) candidate lookup before
	// the constant-time hash comparison (SPEC.md §14.1).
	KeyPrefix string

	// KeyHash is the SHA-256 hex digest (64 chars) of the full raw bearer token.
	// Compared using crypto/subtle.ConstantTimeCompare (SPEC.md §14.2).
	KeyHash string

	CreatedAt time.Time

	// RotatedAt is non-nil when this key has been superseded by a new one.
	// The row is retained for audit history.
	RotatedAt *time.Time
}

// Repository defines the persistence contract for Application and APIKey entities.
// The implementation lives in internal/infra/postgres/applicationrepo.go.
//
// References:
//   - ADR-0015 — repository interfaces belong in the domain package
type Repository interface {
	// Create persists a new Application and returns it with ID and timestamps filled in.
	Create(app Application) (Application, error)

	// Get retrieves an Application by its surrogate ID.
	Get(id int64) (Application, error)

	// GetByName retrieves an active Application by its unique name.
	GetByName(name string) (Application, error)

	// List returns all Applications (active and inactive), ordered by name.
	List() ([]Application, error)

	// Update persists changes to an existing Application. ID must be set.
	Update(app Application) (Application, error)

	// Delete soft-deletes an Application by setting active=false.
	Delete(id int64) error

	// CreateAPIKey persists a new APIKey and returns it with ID filled in.
	// Replaces any existing active key for the same ApplicationID (ADR-0009).
	CreateAPIKey(key APIKey) (APIKey, error)

	// GetAPIKeyByPrefix retrieves the APIKey matching the given prefix.
	// Used by the auth middleware for bearer token validation.
	GetAPIKeyByPrefix(prefix string) (APIKey, error)

	// RotateAPIKey atomically replaces the current APIKey for an application:
	// sets rotated_at on the old key and inserts newKey in a single transaction.
	RotateAPIKey(applicationID int64, newKey APIKey) (APIKey, error)
}
