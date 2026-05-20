// Package usage defines the UsageEvent type and the async writer that persists
// usage records to the usage_events table after each gateway request.
//
// References:
//   - SPEC.md §5.4 — UsageEvent struct
//   - SPEC.md §9.1 steps 11–12 — emit usage event
package usage

import "time"

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
