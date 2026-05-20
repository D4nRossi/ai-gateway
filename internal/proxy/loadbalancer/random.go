package loadbalancer

import (
	"math/rand/v2"

	"github.com/D4nRossi/ai-gateway/internal/domain/endpoint"
)

// Random picks a uniformly random target on each call.
//
// Uses math/rand/v2.IntN which is concurrency-safe out of the box and seeds itself
// from a runtime-provided entropy source; no manual seeding required.
//
// Reasoning: not cryptographically random — that would be wasteful for load
// distribution. Statistical uniformity is what matters, and IntN delivers it.
type Random struct{}

// NewRandom constructs a Random balancer. It has no state.
func NewRandom() *Random {
	return &Random{}
}

// Select returns a uniformly random active target, or ErrNoTargets if none exist.
func (r *Random) Select(targets []endpoint.Target, _ string) (endpoint.Target, error) {
	active := filterActive(targets)
	if len(active) == 0 {
		return endpoint.Target{}, ErrNoTargets
	}
	return active[rand.IntN(len(active))], nil
}

func (r *Random) OnRequestStart(_ int64) {}
func (r *Random) OnRequestEnd(_ int64)   {}
