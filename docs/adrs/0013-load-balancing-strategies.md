# ADR-0013: Load balancing strategies for generic proxy endpoints

- **Status**: accepted
- **Date**: 2026-05-20
- **Decision makers**: Danirek (Software Architect, Digital Innovation — Teleperformance Brasil)

## Context

Each proxy endpoint registered in V2 may have multiple upstream targets (e.g., two Azure Speech
instances for HA, or a primary + fallback TTS provider). The gateway must select a target on each
incoming request.

Different workloads need different selection strategies:
- AI/LLM calls have variable latency (5ms to 120s); a "least connections" strategy avoids piling
  requests onto a slow instance.
- Voice/TTS requests may need sticky routing per user session to avoid mid-session provider switch.
- Simple REST upstreams do well with round-robin.
- Upstreams with different compute quotas (e.g., Azure tier S0 vs S1) need weighted distribution.

## Decision

Implement **5 strategies** in `internal/proxy/loadbalancer.go`, all in-memory:

| Strategy | Implementation | Use case |
|---|---|---|
| `round_robin` (default) | Atomic `uint64` counter mod `len(targets)` | Homogeneous upstreams |
| `weighted_round_robin` | Per-target weight; counter mod total-weight, binary search | Heterogeneous quota/capacity |
| `random` | `math/rand/v2.IntN` (stdlib, Go 1.22+) | High availability, stateless |
| `least_connections` | Per-target `int64` atomic counter; increment on acquire, decrement on complete | Variable-duration requests (LLM, audio) |
| `ip_hash` | FNV-1a hash of `r.RemoteAddr` (IP only) mod `len(targets)` | Sticky sessions |

**V3** will add Redis-backed `least_connections` for multi-instance deployments (when multiple
gateway replicas share traffic behind a load balancer).

The operator configures the strategy per endpoint in the Admin UI / CRUD API. Default: `round_robin`.

## Options considered

### Option 1: Round-robin only
- Pros: minimal implementation
- Cons: insufficient for LLM workloads (variable latency makes connections pile up on slow instance)
  and for sticky-session requirements

### Option 2: Full implementation — 5 strategies (chosen)
- Pros: covers all common production patterns; all in-memory (no new dependencies); pluggable
  interface allows adding strategies without changing caller code
- Cons: more code to maintain; least-connections counter is per-instance (not globally consistent
  in multi-replica deployments)
- Why: the operator chose strategies up front during the v2-alignment session; all 5 are needed for
  the range of backends (REST, LLM, voice, internal APIs)

### Option 3: Delegate to external load balancer (nginx, HAProxy)
- Pros: battle-tested, feature-rich
- Cons: moves policy enforcement (auth, rate limit, budget) out of the gateway; consumer apps would
  bypass gateway features; defeats the purpose of routing through the gateway

### Option 4: Consistent hashing
- Pros: useful for cache-tier backends
- Cons: not applicable for the target backends (Azure OpenAI, Speech, REST APIs are stateless or
  session-managed at the application level)

## Consequences

### Positive
- Single gateway handles all routing policy for all backends — uniform auth, audit, budget
- `least_connections` prevents hot-spotting on slow LLM instances
- `ip_hash` enables stateful voice session stickiness without session cookies
- All strategies are deterministic and testable (inject fixed target list, verify selection order)

### Negative / Trade-offs
- `least_connections` and `ip_hash` counters are per-process — not consistent across multiple
  gateway replicas. Documented limitation for V2 (single-instance).

### Mitigations
- V2 is single-instance (ADR-0006 rate limit also in-memory). Multi-instance is a V3 concern.
- Document in README: "for multi-replica deployments, use `round_robin` or `random` until V3"

## References

- docs/v2-alignment.md — response I (load balancing strategies)
- System Design Interview Vol. 1, Alex Xu — Chapter 4 (rate limiter), Chapter 21 (design a gateway)
- https://pkg.go.dev/math/rand/v2
- https://pkg.go.dev/sync/atomic
