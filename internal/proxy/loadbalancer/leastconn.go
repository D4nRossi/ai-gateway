package loadbalancer

import (
	"sync"

	"github.com/D4nRossi/ai-gateway/internal/domain/endpoint"
)

// LeastConnections routes each request to the target with the fewest in-flight
// requests, breaking ties by appearance order in the input list.
//
// In-flight counts are maintained via the OnRequestStart / OnRequestEnd hooks.
// The proxy handler calls Start immediately before issuing the upstream request
// and End (deferred) immediately after the response body is fully consumed.
//
// Reasoning: counters are per-process and NOT distributed; in a multi-replica
// deployment each replica sees only its own load. This is documented in ADR-0013
// as an accepted limitation for Phase 1.
//
// References:
//   - ADR-0013 — least_connections semantics and limitations
type LeastConnections struct {
	mu     sync.Mutex
	counts map[int64]int // target ID → in-flight count
}

// NewLeastConnections constructs a LeastConnections balancer with empty counters.
func NewLeastConnections() *LeastConnections {
	return &LeastConnections{counts: make(map[int64]int)}
}

// Select scans active targets and returns the one with the lowest in-flight count.
// On ties it returns the earlier target in the input order, which gives stable
// behavior when counters are all zero (effectively round-robin first-arrival).
func (l *LeastConnections) Select(targets []endpoint.Target, _ string) (endpoint.Target, error) {
	active := filterActive(targets)
	if len(active) == 0 {
		return endpoint.Target{}, ErrNoTargets
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	best := active[0]
	bestCount := l.counts[best.ID]
	for i := 1; i < len(active); i++ {
		if c := l.counts[active[i].ID]; c < bestCount {
			best = active[i]
			bestCount = c
		}
	}
	return best, nil
}

// OnRequestStart increments the in-flight count for the given target.
func (l *LeastConnections) OnRequestStart(targetID int64) {
	l.mu.Lock()
	l.counts[targetID]++
	l.mu.Unlock()
}

// OnRequestEnd decrements the in-flight count for the given target, never below 0.
// The floor exists to defend against double-End calls from handler bugs.
func (l *LeastConnections) OnRequestEnd(targetID int64) {
	l.mu.Lock()
	if l.counts[targetID] > 0 {
		l.counts[targetID]--
	}
	l.mu.Unlock()
}
