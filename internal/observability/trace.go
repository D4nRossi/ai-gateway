package observability

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// LatencyBuckets enumerates the canonical bucket names instrumented by the
// chat handler (ADR-0021). Order is significant only for header rendering —
// the map storage doesn't preserve insertion order.
//
// Adding a new bucket: update this list, the LatencyTrace docstring, the
// usage.UsageEvent struct, the migration adding the column, and the
// chat handler to call Mark with the new name.
var LatencyBuckets = []string{
	"auth",       // bearer extract, prefix lookup, hash compare, policy fetch
	"mask",       // regex local + Azure Language PII (cloud)
	"guardrails", // local injection + Azure Prompt Shield + Content Safety
	"provider",   // upstream LLM call (marshal + roundtrip + unmarshal)
	"encode",     // JSON serialization of the response to the client
}

// LatencyTrace accumulates per-bucket time spent during one request.
//
// Usage:
//
//	trace := observability.StartTrace()
//	// ... do auth work ...
//	trace.Mark("auth")
//	// ... do mask work ...
//	trace.Mark("mask")
//	// ...
//	w.Header().Set("X-Gateway-Latency-Breakdown", trace.Header())
//
// Each Mark records the elapsed time since the previous Mark (or StartTrace)
// into the named bucket. Calling Mark with the same name twice ACCUMULATES —
// useful if a bucket has multiple disjoint segments (e.g. retry of a
// provider call).
//
// LatencyTrace is safe for concurrent use within a single request (the chat
// handler is single-goroutine per request, but the streaming path may use
// multiple goroutines that Mark independently — the mutex makes this safe).
//
// References:
//   - ADR-0021 — latency breakdown observável
type LatencyTrace struct {
	mu      sync.Mutex
	start   time.Time
	last    time.Time
	buckets map[string]time.Duration
}

// StartTrace returns a new trace anchored to the current monotonic clock.
// Use observability.StartTrace at the very top of the handler, BEFORE any
// step the trace should cover.
func StartTrace() *LatencyTrace {
	now := time.Now()
	return &LatencyTrace{
		start:   now,
		last:    now,
		buckets: make(map[string]time.Duration, len(LatencyBuckets)),
	}
}

// Mark records the elapsed time since the previous Mark (or StartTrace) into
// the named bucket. Subsequent Marks for the same bucket accumulate.
//
// If trace is nil (e.g. an old code path that doesn't pass it down), Mark is
// a no-op — keeps the call sites safe during incremental instrumentation.
func (t *LatencyTrace) Mark(bucket string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	delta := now.Sub(t.last)
	t.buckets[bucket] += delta
	t.last = now
}

// Bucket returns the elapsed time in milliseconds for the given bucket.
// Returns 0 when the bucket was never marked OR when the cumulative time is
// sub-millisecond. Consistent with how the header renders the same data.
func (t *LatencyTrace) Bucket(name string) int {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	d, ok := t.buckets[name]
	if !ok {
		return 0
	}
	return int(d.Milliseconds())
}

// TotalMs returns the wall time since StartTrace, in milliseconds. Useful as
// a sanity check (latency_ms ≈ sum of buckets + "other").
func (t *LatencyTrace) TotalMs() int {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return int(time.Since(t.start).Milliseconds())
}

// Header renders the buckets as the X-Gateway-Latency-Breakdown header value
// (ADR-0021). Format: "auth=2;mask=180;guardrails=0;provider=2400;encode=3".
// Buckets are rendered in LatencyBuckets order so the output is deterministic
// and the client can split on ';' to parse.
//
// Buckets with no recorded time render as "<name>=0" so the consumer can
// always count on 5 fields being present.
func (t *LatencyTrace) Header() string {
	if t == nil {
		return ""
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	parts := make([]string, 0, len(LatencyBuckets))
	for _, name := range LatencyBuckets {
		ms := int64(0)
		if d, ok := t.buckets[name]; ok {
			ms = d.Milliseconds()
		}
		parts = append(parts, fmt.Sprintf("%s=%d", name, ms))
	}
	return strings.Join(parts, ";")
}
