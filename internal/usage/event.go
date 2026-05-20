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
// References:
//   - SPEC.md §5.4
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
}
