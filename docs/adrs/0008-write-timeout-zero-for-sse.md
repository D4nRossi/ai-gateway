# ADR-0008: WriteTimeout: 0 to Support Long SSE Streams

- **Status**: accepted
- **Date**: 2026-05-19
- **Decision makers**: Danirek

## Context

The gateway supports Server-Sent Events (SSE) streaming for chat completions. HTTP server `WriteTimeout` sets the deadline for writing the entire response. For SSE streams that may last many seconds, a non-zero `WriteTimeout` would prematurely close the connection.

## Decision

Set `http.Server.WriteTimeout = 0` (disabled) to allow SSE streams to run for their natural duration without a server-imposed write deadline.

## Options considered

### Option 1: Non-zero WriteTimeout (e.g. 60s)
- Pros: DoS protection — slow clients cannot hold server goroutines indefinitely.
- Cons: any streaming response longer than WriteTimeout is forcibly terminated. Azure OpenAI responses for complex prompts can easily exceed 30–60 seconds.

### Option 2 (chosen): WriteTimeout = 0 (disabled)
- Pros: SSE streams can run to completion regardless of duration.
- Cons: a malicious slow consumer could hold a connection open indefinitely.
- Why: Phase 1 deploys behind an internal network; consumers are authenticated corporate apps, not anonymous public users. DoS risk is acceptable at this stage.

## Consequences

### Positive
- Streaming works reliably for any response length.
- No premature connection termination surprises.

### Negative / Trade-offs
- Slow or disconnected consumers may hold goroutines open longer than necessary.

### Mitigations
- `ReadTimeout` and `ReadHeaderTimeout` are still set to protect against slow request attacks.
- `ctx.Done()` detection in the streaming loop cancels the upstream Azure call when the client disconnects, bounding resource hold time to the Azure response time.
- Phase 2: consider per-request streaming timeout or NGINX/load-balancer idle timeout as an additional guard.

## References
- SPEC.md §14.4 — HTTP server timeouts
- SPEC.md §15.3 — cancellation on client disconnect
- https://pkg.go.dev/net/http#Server
