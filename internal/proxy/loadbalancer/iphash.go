package loadbalancer

import (
	"crypto/sha256"
	"encoding/binary"

	"github.com/D4nRossi/ai-gateway/internal/domain/endpoint"
)

// IPHash routes requests from the same client IP to the same target, enabling
// upstream stickiness for stateful sessions.
//
// The hash function is SHA-256 truncated to the first 4 bytes interpreted as a
// big-endian uint32, then taken modulo the number of active targets. SHA-256 is
// not cryptographic overkill — it ensures even bucketing across the target count
// regardless of the IP distribution (e.g. a single /24 contains 256 highly
// correlated values that a weaker hash like fnv-32 might bucket unevenly).
//
// Caveat: when the active target list changes (target added/removed or weight
// change), most clients will rehash to a different target. This is acceptable
// for the AI-gateway use case where upstream sessions are short-lived.
//
// Reasoning: simpler than consistent hashing rings, which would minimize remapping
// at the cost of significant code/state complexity. ADR-0013 accepts this trade-off.
//
// References:
//   - ADR-0013 — ip_hash semantics and rehashing behavior
type IPHash struct{}

// NewIPHash constructs an IPHash balancer. It has no state.
func NewIPHash() *IPHash {
	return &IPHash{}
}

// Select picks the target whose index equals hash(clientIP) modulo the number of
// active targets. An empty clientIP hashes to a deterministic value, which means
// requests with no detectable IP all go to the same target — preferable to a
// random pick because it is debuggable.
func (h *IPHash) Select(targets []endpoint.Target, clientIP string) (endpoint.Target, error) {
	active := filterActive(targets)
	if len(active) == 0 {
		return endpoint.Target{}, ErrNoTargets
	}

	sum := sha256.Sum256([]byte(clientIP))
	bucket := binary.BigEndian.Uint32(sum[:4]) % uint32(len(active))
	return active[bucket], nil
}

func (h *IPHash) OnRequestStart(_ int64) {}
func (h *IPHash) OnRequestEnd(_ int64)   {}
