# ADR-0003: slog as the Official Logger

- **Status**: accepted
- **Date**: 2026-05-19
- **Decision makers**: Danirek

## Context

The gateway needs structured JSON logging for production observability and text logging for local development. Several Go logging libraries were evaluated.

## Decision

Use `log/slog` (Go standard library, introduced in Go 1.21) as the only logger. No third-party logging library is permitted.

## Options considered

### Option 1: go.uber.org/zap
- Pros: high performance; structured; widely adopted.
- Cons: external dependency; API differs from standard library; performance advantage irrelevant at gateway scale.

### Option 2: github.com/sirupsen/logrus
- Pros: widely adopted; structured.
- Cons: external dependency; not actively maintained; not structured-first.

### Option 3 (chosen): log/slog
- Pros: zero external dependency; idiomatic Go 1.21+; structured; supports JSON and text handlers; injectable logger pattern aligns with our design.
- Cons: slightly less ergonomic than zap for very high-throughput hot paths (not relevant at this scale).
- Why: eliminates a dependency, is the official Go structured logging solution, and has a stable interface.

## Consequences

### Positive
- No new dependency.
- Standard interface recognized by any Go developer.
- `slog.Logger` is trivially injectable (dependency injection by value, not global).

### Negative / Trade-offs
- Slightly more verbose than zap for structured fields (`"key", value` pairs instead of `zap.String("key", value)`).

### Mitigations
- The verbosity difference is cosmetic and irrelevant at this project's scale.

## References
- https://pkg.go.dev/log/slog
- CLAUDE.md §4.3 — standard library list
