package loadbalancer

import (
	"errors"
	"fmt"

	"github.com/D4nRossi/ai-gateway/internal/domain/endpoint"
)

// ErrNoTargets is returned by Select when the input target list is empty
// or contains only inactive entries. Callers should respond 503 Service Unavailable.
var ErrNoTargets = errors.New("no active targets available")

// ErrUnknownStrategy is returned by New when the requested strategy is not
// implemented in this package.
var ErrUnknownStrategy = errors.New("unknown load balancing strategy")

// Balancer is the contract implemented by every load-balancing strategy.
//
// Implementations must be safe for concurrent use by multiple goroutines.
//
// Select chooses one Target from the supplied list. clientIP is provided for sticky
// strategies (ip_hash); stateless implementations ignore it.
//
// OnRequestStart and OnRequestEnd are lifecycle hooks. The proxy handler calls
// OnRequestStart immediately before dispatching the upstream call and OnRequestEnd
// (deferred) immediately after the response body has been fully relayed (or on error).
// Stateful strategies (least_connections) use these to maintain accurate counters;
// stateless strategies implement them as no-ops.
//
// References:
//   - ADR-0013 — algorithm semantics and trade-offs
type Balancer interface {
	Select(targets []endpoint.Target, clientIP string) (endpoint.Target, error)
	OnRequestStart(targetID int64)
	OnRequestEnd(targetID int64)
}

// New constructs the Balancer for the given strategy. Returns ErrUnknownStrategy
// for any value not enumerated in domain/endpoint.LBStrategy.
//
// Reasoning: dispatching through a factory keeps callers (Registry, proxyservice)
// decoupled from concrete struct names — adding a new strategy only touches this
// file and the new strategy's implementation file.
func New(strategy endpoint.LBStrategy) (Balancer, error) {
	switch strategy {
	case endpoint.LBRoundRobin:
		return NewRoundRobin(), nil
	case endpoint.LBWeightedRR:
		return NewWeightedRR(), nil
	case endpoint.LBRandom:
		return NewRandom(), nil
	case endpoint.LBLeastConnections:
		return NewLeastConnections(), nil
	case endpoint.LBIPHash:
		return NewIPHash(), nil
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnknownStrategy, strategy)
	}
}

// filterActive returns only targets with Active=true.
// Returned slice shares backing memory with input on the fast path (all active);
// allocates a new slice when filtering is required.
func filterActive(targets []endpoint.Target) []endpoint.Target {
	allActive := true
	for _, t := range targets {
		if !t.Active {
			allActive = false
			break
		}
	}
	if allActive {
		return targets
	}
	out := make([]endpoint.Target, 0, len(targets))
	for _, t := range targets {
		if t.Active {
			out = append(out, t)
		}
	}
	return out
}
