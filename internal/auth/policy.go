// Package auth implements bearer-token authentication and per-application
// policy lookup for the AI Gateway.
//
// This package is responsible for:
//   - Defining the AppPolicy domain type
//   - Providing the PolicyStore interface for O(1) prefix-based lookup
//   - Supplying NewPolicyStore, the Phase 1 in-memory implementation
//
// The in-memory store is populated once at bootstrap from configs/gateway.yaml
// and is safe for concurrent reads without locks (immutable after construction).
// A future DB-backed implementation would satisfy the same PolicyStore interface
// without changing any call sites (see ADR-0002).
//
// References:
//   - SPEC.md §5.2 — AppPolicy and PolicyStore types
//   - CLAUDE.md §10 — configuration and secrets policy
//   - ADR-0002 — YAML config (Phase 1) vs. DB-backed policies (Phase 2)
package auth

import "github.com/D4nRossi/ai-gateway/internal/config"

// AppPolicy represents the runtime authorization policy of a consumer application.
// It is loaded from configs/gateway.yaml at boot and held in memory through PolicyStore.
//
// Reasoning: keys + policies live in config (not DB) for Phase 1 because the Admin
// API is out of scope; migrating to DB is planned in Phase 2 (see ADR-0002).
//
// References:
//   - SPEC.md §5.2
//   - ADR-0002
type AppPolicy struct {
	// Name is the unique application identifier used in logs and audit events.
	Name string

	// KeyPrefix is the public prefix of the bearer token used for O(1) policy lookup.
	KeyPrefix string

	// KeyHash is the SHA-256 hex digest of the full bearer token.
	// Comparison is performed in constant time (see middleware/auth.go).
	KeyHash string

	// Tier is the security pipeline tier: "tier_1", "tier_2", or "tier_3".
	Tier string

	// AllowedModels lists the public model names this application may request.
	AllowedModels []string

	// StreamingAllowed indicates whether SSE streaming is permitted for this application.
	StreamingAllowed bool

	// MaxRPM is the maximum requests per minute enforced by the rate limiter.
	MaxRPM int

	// MaxTPM is the maximum tokens per minute (informational for Phase 1; enforced in Phase 2).
	MaxTPM int

	// MonthlyBudgetBRL is the monthly spend cap in Brazilian Reals.
	MonthlyBudgetBRL float64
}

// PolicyStore is the interface for looking up an AppPolicy by bearer token prefix.
//
// Reasoning: abstracting behind an interface allows swapping the in-memory
// Phase 1 store with a DB-backed implementation in Phase 2 without touching
// any middleware or handler code.
//
// References:
//   - SPEC.md §5.2
//   - ADR-0002
type PolicyStore interface {
	// Lookup returns the AppPolicy for the given token prefix.
	// Returns false if no policy exists for that prefix.
	Lookup(prefix string) (AppPolicy, bool)
}

// inMemoryStore is the Phase 1 in-memory implementation of PolicyStore.
// It is keyed by KeyPrefix for O(1) lookup and is never mutated after construction.
type inMemoryStore struct {
	byPrefix map[string]AppPolicy
}

// NewPolicyStore builds an in-memory PolicyStore from the application configs
// loaded from gateway.yaml. Safe for concurrent reads; never mutated after return.
//
// Reasoning: a map populated at startup and never modified requires no locking for
// concurrent reads; this makes auth hot-path allocation-free.
//
// References:
//   - SPEC.md §5.2
//   - CLAUDE.md §10.1 — configuration loading policy
func NewPolicyStore(apps []config.ApplicationConfig) PolicyStore {
	m := make(map[string]AppPolicy, len(apps))
	for _, a := range apps {
		m[a.KeyPrefix] = AppPolicy{
			Name:             a.Name,
			KeyPrefix:        a.KeyPrefix,
			KeyHash:          a.KeyHash,
			Tier:             a.Tier,
			AllowedModels:    a.AllowedModels,
			StreamingAllowed: a.StreamingAllowed,
			MaxRPM:           a.MaxRPM,
			MaxTPM:           a.MaxTPM,
			MonthlyBudgetBRL: a.MonthlyBudgetBRL,
		}
	}
	return &inMemoryStore{byPrefix: m}
}

// Lookup returns the AppPolicy associated with the given token prefix.
func (s *inMemoryStore) Lookup(prefix string) (AppPolicy, bool) {
	p, ok := s.byPrefix[prefix]
	return p, ok
}
