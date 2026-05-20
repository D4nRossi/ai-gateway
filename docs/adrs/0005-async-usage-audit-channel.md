# ADR-0005: Async Usage/Audit via Buffered Channel (vs. Synchronous Write)

- **Status**: accepted
- **Date**: 2026-05-19
- **Decision makers**: Danirek

## Context

After each gateway request, usage events and audit events must be persisted to PostgreSQL. The write must not block the HTTP response to the consumer. Two approaches were evaluated.

## Decision

Use a buffered `chan` (capacity 10 000) per writer (usage, audit, budget). A background goroutine drains each channel and performs DB inserts. The send is non-blocking: if the channel is full, the event is dropped with a `warn` log.

## Options considered

### Option 1: Synchronous write inside handler
- Pros: simpler; guaranteed persistence before response.
- Cons: adds DB latency to every request's hot path; DB hiccup directly impacts consumer latency; unacceptable for streaming.

### Option 2: goroutine per request
- Pros: simple; non-blocking.
- Cons: unbounded goroutine creation under load; no back-pressure mechanism.

### Option 3 (chosen): buffered channel + single worker goroutine
- Pros: bounded memory (max 10 000 events in flight); single goroutine per writer; back-pressure via drop-and-warn; clean shutdown via context cancellation + drain loop.
- Cons: events may be dropped under extreme load; potential data loss if process crashes with events in channel.
- Why: for a demo gateway, a bounded drop is acceptable. The 10 000-event buffer assumes ≤ 1 000 req/s peak and a worker doing > 100 inserts/s, giving 10 s of burst capacity.

## Consequences

### Positive
- DB latency is off the critical path; consumer sees no impact from slow inserts.
- Bounded memory use.
- Clean graceful shutdown: context cancel triggers drain loop.

### Negative / Trade-offs
- Events may be dropped under extreme load.
- Process crash may lose buffered events.

### Mitigations
- Drop warning logged with `event_type=usage_dropped` / `audit_dropped`.
- For Phase 2: replace channel with durable queue (e.g. Kafka, Redis Streams).

## References
- SPEC.md §9.1 steps 11–12 — "non-blocking on full channel; warn if dropped"
- CLAUDE.md §6.4 — buffer sizing comment example
