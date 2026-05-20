package loadbalancer

import (
	"sync/atomic"

	"github.com/D4nRossi/ai-gateway/internal/domain/endpoint"
)

// RoundRobin distributes requests evenly across targets using an atomic counter.
// It is stateless across calls beyond the counter; the lifecycle hooks are no-ops.
//
// The counter is uint64 to avoid sign issues with the modulo operation. Wrap-around
// happens after ~1.8×10¹⁹ requests, which is effectively unreachable.
type RoundRobin struct {
	counter atomic.Uint64
}

// NewRoundRobin constructs a RoundRobin balancer with the counter at zero.
func NewRoundRobin() *RoundRobin {
	return &RoundRobin{}
}

// Select advances the atomic counter and picks targets[counter % len(targets)].
// Returns ErrNoTargets when the active subset is empty.
//
// Reasoning: atomic.Add returns the post-increment value; subtracting 1 makes the
// first call use index 0 (the principle of least surprise for "round robin").
func (r *RoundRobin) Select(targets []endpoint.Target, _ string) (endpoint.Target, error) {
	active := filterActive(targets)
	if len(active) == 0 {
		return endpoint.Target{}, ErrNoTargets
	}
	i := r.counter.Add(1) - 1
	return active[i%uint64(len(active))], nil
}

func (r *RoundRobin) OnRequestStart(_ int64) {}
func (r *RoundRobin) OnRequestEnd(_ int64)   {}
