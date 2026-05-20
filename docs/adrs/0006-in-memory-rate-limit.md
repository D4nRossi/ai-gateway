# ADR-0006: In-Memory Rate Limit (Phase 1) vs. Redis (Phase 2)

- **Status**: accepted
- **Date**: 2026-05-19
- **Decision makers**: Danirek

## Context

The gateway must enforce per-application RPM limits. The implementation needs to be consistent with the Phase 1 single-instance deployment model.

## Decision

Use `golang.org/x/time/rate` token bucket limiters, one per application, stored in a mutex-guarded map in process memory. Redis-backed distributed limiting is deferred to Phase 2.

## Options considered

### Option 1: Redis-backed distributed limiter
- Pros: survives restarts; works across multiple instances.
- Cons: adds Redis as a runtime dependency; operational overhead; unnecessary for single-instance Phase 1 demo.

### Option 2 (chosen): In-memory token bucket (golang.org/x/time/rate)
- Pros: zero external dependency; extremely fast; correct for single-instance deployment.
- Cons: rate limits reset on restart; cannot coordinate across multiple instances.
- Why: Phase 1 is single-instance. The `golang.org/x/time/rate` package is the official Go rate limiting library.

## Consequences

### Positive
- No Redis dependency.
- Sub-microsecond check latency.

### Negative / Trade-offs
- Rate limit state is lost on restart.
- Will not work correctly if multiple gateway instances are deployed (Phase 2 concern).

### Mitigations
- Phase 2: abstract behind `RateLimiter` interface and swap concrete implementation to Redis.

## References
- SPEC.md §12.1 — rate limit specification
- https://pkg.go.dev/golang.org/x/time/rate
