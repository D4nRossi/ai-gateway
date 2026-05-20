package loadbalancer

import (
	"sync"

	"github.com/D4nRossi/ai-gateway/internal/domain/endpoint"
)

// Registry holds one Balancer per ProxyEndpoint ID, keyed by endpoint ID, with
// strategy-change detection: if an operator switches an endpoint's lb_strategy
// via the Admin API, the next call to For replaces the stale Balancer.
//
// Reasoning: each strategy carries internal state (counters, current-weight maps)
// that must persist across requests for correct distribution. Keeping that state
// per endpoint avoids cross-talk between endpoints; detecting strategy changes
// avoids stale-state bugs when configuration evolves.
type Registry struct {
	mu    sync.RWMutex
	cache map[int64]registryEntry
}

// registryEntry pairs a Balancer with the LBStrategy that produced it, so we can
// detect a strategy change and rebuild on demand.
type registryEntry struct {
	strategy endpoint.LBStrategy
	balancer Balancer
}

// NewRegistry constructs an empty Registry.
func NewRegistry() *Registry {
	return &Registry{cache: make(map[int64]registryEntry)}
}

// For returns the Balancer associated with the given endpoint, building it on
// first access and rebuilding it on strategy change.
//
// Returns an error only when New fails (i.e. unknown strategy); callers should
// treat that as a 500 Internal Server Error after logging — it means the DB
// stored an LBStrategy value that this build does not implement.
func (r *Registry) For(ep endpoint.ProxyEndpoint) (Balancer, error) {
	r.mu.RLock()
	e, ok := r.cache[ep.ID]
	r.mu.RUnlock()
	if ok && e.strategy == ep.LBStrategy {
		return e.balancer, nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	// Double-check after acquiring the write lock — another goroutine may have
	// already rebuilt this entry while we were waiting.
	if e, ok := r.cache[ep.ID]; ok && e.strategy == ep.LBStrategy {
		return e.balancer, nil
	}

	b, err := New(ep.LBStrategy)
	if err != nil {
		return nil, err
	}
	r.cache[ep.ID] = registryEntry{strategy: ep.LBStrategy, balancer: b}
	return b, nil
}

// Forget removes the Balancer for the given endpoint ID. Intended for admin-driven
// invalidation when an endpoint is deleted; calling For afterwards will build a
// fresh Balancer.
func (r *Registry) Forget(endpointID int64) {
	r.mu.Lock()
	delete(r.cache, endpointID)
	r.mu.Unlock()
}
