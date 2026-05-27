// Package usage defines the UsageEvent type and the async writer that persists
// usage records to the usage_events table after each gateway request.
//
// References:
//   - SPEC.md §5.4 — UsageEvent struct
//   - SPEC.md §9.1 steps 11–12 — emit usage event
package usage

import "time"

// Emitter is the interface satisfied by Writer and any test stub.
// Decoupling handlers from the concrete async writer enables unit testing
// without a live database connection.
//
// References:
//   - SPEC.md §5.4
//   - CLAUDE.md §14 — testability via interface injection
type Emitter interface {
	Emit(UsageEvent)
}

// UsageEvent captures per-request token consumption and latency.
//
// LatencyMs is the total wall time of the request; the Lat*Ms fields below
// decompose it by pipeline bucket (ADR-0021). All Lat*Ms default to 0 and
// are persisted as NULL when 0 — the writer treats 0 as "not instrumented"
// so legacy callers that don't populate them don't dirty the new columns
// with zeros that look like measurements.
//
// References:
//   - SPEC.md §5.4
//   - ADR-0021 — latency breakdown observável
type UsageEvent struct {
	RequestID        string
	ApplicationName  string
	Tier             string
	Model            string
	Provider         string // "azure" | "mock"
	InputTokens      int
	OutputTokens     int
	TotalTokens      int
	LatencyMs        int
	StatusCode       int
	EstimatedCostBRL float64
	CreatedAt        time.Time

	// Per-bucket breakdown of LatencyMs (ADR-0021). 0 means "not instrumented"
	// and is stored as NULL in usage_events. Callers that instrument should
	// populate all 5 fields, even if some buckets are 0 (Tier 1 has no
	// guardrails, etc.).
	LatAuthMs       int
	LatMaskMs       int
	LatGuardrailsMs int
	LatProviderMs   int
	LatEncodeMs     int
}
