package loadbalancer

import (
	"sync"

	"github.com/D4nRossi/ai-gateway/internal/domain/endpoint"
)

// WeightedRR implements Nginx's "smooth weighted round-robin" algorithm. Unlike
// the naive expanded-list approach (which can send several consecutive requests
// to the same heavy target), smooth WRR interleaves selections so the realized
// distribution closely matches the configured weights at every prefix of the
// request stream.
//
// State per target: a "current weight" that is incremented by the configured weight
// before each pick. The target with the highest current weight is selected, then
// has the sum of all configured weights subtracted. Over a full cycle, every
// target is picked exactly weight[i] times.
//
// Reasoning: smoothness matters when targets have very different latencies or
// capacities; a streak of requests to the heavy target would produce uneven tail
// latency. The cost is one map read/write per selection, which is negligible at
// gateway request rates.
//
// References:
//   - ADR-0013 — weighted_round_robin algorithm choice
//   - https://github.com/phusion/nginx/commit/27e94984486058d73157038f7950a0a36ecc6e35
type WeightedRR struct {
	mu      sync.Mutex
	current map[int64]int // target ID → current weight; created on first Select
}

// NewWeightedRR constructs a WeightedRR balancer with empty state.
func NewWeightedRR() *WeightedRR {
	return &WeightedRR{current: make(map[int64]int)}
}

// Select runs one iteration of smooth WRR over the active targets.
// Returns ErrNoTargets when no active target is provided.
func (w *WeightedRR) Select(targets []endpoint.Target, _ string) (endpoint.Target, error) {
	active := filterActive(targets)
	if len(active) == 0 {
		return endpoint.Target{}, ErrNoTargets
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	totalWeight := 0
	for _, t := range active {
		if t.Weight <= 0 {
			// Defensive: schema CHECK already rejects this, but if it slipped through
			// treat as weight 1 to avoid stalling the rotation.
			w.current[t.ID] += 1
			totalWeight += 1
		} else {
			w.current[t.ID] += t.Weight
			totalWeight += t.Weight
		}
	}

	bestIdx := 0
	bestVal := w.current[active[0].ID]
	for i := 1; i < len(active); i++ {
		if v := w.current[active[i].ID]; v > bestVal {
			bestIdx = i
			bestVal = v
		}
	}

	chosen := active[bestIdx]
	w.current[chosen.ID] -= totalWeight
	return chosen, nil
}

func (w *WeightedRR) OnRequestStart(_ int64) {}
func (w *WeightedRR) OnRequestEnd(_ int64)   {}
