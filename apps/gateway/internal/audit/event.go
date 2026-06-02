// Package audit defines AuditEvent types and the async writer that persists
// policy-decision records to the audit_events table.
//
// References:
//   - SPEC.md §5.4 — AuditEvent struct and EventType constants
//   - SPEC.md §9.1 steps 13 — emit audit event
package audit

import "time"

// Emitter is the interface satisfied by Writer and any test stub.
// Decoupling handlers and middleware from the concrete async writer enables
// unit testing without a live database connection.
//
// References:
//   - SPEC.md §5.4
//   - CLAUDE.md §14 — testability via interface injection
type Emitter interface {
	Emit(AuditEvent)
}

// AuditEvent captures a security or policy decision made during request processing.
//
// References:
//   - SPEC.md §5.4
type AuditEvent struct {
	RequestID       string
	ApplicationName string
	EventType       string         // one of the Event* constants below
	Severity        string         // "info" | "warn" | "error"
	Metadata        map[string]any // serialised as JSONB; never contains prompt content
	CreatedAt       time.Time
}

// Event type constants for AuditEvent.EventType.
//
// References:
//   - SPEC.md §5.4
const (
	EventAuthFailed         = "auth_failed"
	EventModelBlocked       = "model_blocked"
	EventPIIMasked          = "pii_masked"
	EventInjectionDetected  = "injection_detected"
	EventPromptShieldBlock  = "prompt_shield_block"
	EventContentSafetyBlock = "content_safety_block"
	EventRateLimited        = "rate_limited"
	EventBudgetExceeded     = "budget_exceeded"
	EventProviderError      = "provider_error"
	EventStreamCancelled    = "stream_cancelled"
	// EventStreamNoUsage is emitted when a streaming response completes without
	// usage data (consumer did not set stream_options.include_usage or Azure
	// omitted the usage chunk). References: SPEC.md §15.4.
	EventStreamNoUsage = "stream_no_usage"
	// EventPIIDetectedRemote is emitted when the Azure AI Language PII step
	// (ADR-0019) detects one or more entities AFTER the local regex pass.
	// Kept distinct from EventPIIMasked so operators can compare regex
	// precision vs cloud precision and tune categories over time.
	EventPIIDetectedRemote = "pii_detected_remote"
	// EventPIIRemoteUnavailable is emitted when the Azure Language call fails
	// (timeout, 5xx, network) — Tier 2 logs and continues (fail-open),
	// Tier 3 also emits this event but additionally returns 503 to the
	// consumer (fail-closed). ADR-0019.
	EventPIIRemoteUnavailable = "pii_remote_unavailable"
)
