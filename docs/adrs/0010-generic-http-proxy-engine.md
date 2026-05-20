# ADR-0010: Generic HTTP proxy engine — verbatim forwarding + header sanitization

- **Status**: accepted
- **Date**: 2026-05-20
- **Decision makers**: Danirek (Software Architect, Digital Innovation — Teleperformance Brasil)

## Context

V2 adds a second data plane alongside the AI path: a generic HTTP proxy that routes any HTTP
request to a registered upstream endpoint. Use cases include:

- Azure Speech (voice synthesis / recognition) — binary audio streams
- ElevenLabs / other TTS providers — streaming audio
- Internal REST APIs that consumer apps need to reach through the gateway (for centralized auth,
  rate limiting, and audit)
- Any future HTTP-based AI provider that doesn't use the OpenAI chat completions contract

The proxy must:
1. Be completely transparent to the content (no content-type assumptions)
2. Support all HTTP methods (GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS)
3. Support all response types: JSON, SSE, chunked transfer, binary, WebSocket upgrade
4. Add gateway-level features: auth enforcement, rate limiting, request/byte accounting
5. Inject destination authentication credentials without exposing them to the consumer

## Decision

Implement verbatim HTTP forwarding via `io.Copy` between the incoming request body and the upstream
request body, and between the upstream response body and the outgoing response writer.

Header handling follows RFC 7230 §6.1:
- **Remove** hop-by-hop headers before forwarding upstream: `Connection`, `Transfer-Encoding`,
  `TE`, `Trailers`, `Upgrade`, `Proxy-Authorization`, `Proxy-Authenticate`, `Keep-Alive`
- **Add** `X-Forwarded-For`, `X-Request-Id`
- **Replace** `Authorization` with the target's configured auth credentials (see ADR-0012)

The implementation lives in `internal/proxy/`:
- `director.go` — header sanitization + auth injection (request mutation before forwarding)
- `transport.go` — `http.RoundTripper` with per-target timeout; wraps `http.DefaultTransport`
- `loadbalancer.go` — target selection (see ADR-0013)

Route: `{METHOD} /v1/proxy/{endpoint_slug}`

## Options considered

### Option 1: Transform-and-re-serialize (parse body, modify, re-marshal)
- Pros: allows content inspection, filtering
- Cons: breaks binary payloads (audio, image); adds latency; incompatible with SSE/streaming;
  complex content-type branching
- Cons: PII masking on proxy is explicitly out of scope for V2 (docs/v2-alignment.md response H)

### Option 2: Verbatim forwarding with header sanitization (chosen)
- Pros: supports all content types including binary and streaming; near-zero latency overhead;
  simple, auditable implementation
- Cons: no content inspection (by design)
- Why: matches the stated requirement "be agnóstico ao method"; consumer apps choose what they send

### Option 3: `httputil.ReverseProxy` from stdlib
- Pros: battle-tested; handles hop-by-hop removal, X-Forwarded-For
- Cons: couples load balancing to Director function (harder to plug in multiple strategies);
  less transparent for custom auth injection; limits control over streaming behavior
- Note: `httputil.ReverseProxy` is a valid future option but the custom implementation gives
  better integration with the load balancer interface

## Consequences

### Positive
- Supports SSE, chunked transfer, binary audio, WebSocket (V2) without special casing
- Gateway features (auth, rate limit, budget) apply to all traffic uniformly
- Near-zero latency overhead on the proxy path — just header manipulation and `io.Copy`

### Negative / Trade-offs
- No content-aware filtering on the proxy path (by design — see v2-alignment.md response H)
- Guardrails (PII masking, injection detection) are exclusive to the AI path

### Mitigations
- Flag `run_guardrails: bool` is reserved in the endpoint schema for future opt-in guardrails
  on specific endpoints (V3+)

## References

- RFC 7230 §6.1 — hop-by-hop headers: https://datatracker.ietf.org/doc/html/rfc7230#section-6.1
- docs/v2-alignment.md — responses D, E, F, G, H
- ADR-0008 — WriteTimeout: 0 for SSE (applies to proxy streaming as well)
- ADR-0013 — load balancing strategies
