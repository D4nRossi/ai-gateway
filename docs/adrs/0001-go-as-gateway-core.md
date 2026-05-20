# ADR-0001: Go as Gateway Core Language

- **Status**: accepted
- **Date**: 2026-05-19
- **Decision makers**: Danirek (Software Architect, Teleperformance Brasil)

## Context

An internal AI Gateway is needed to mediate traffic between Teleperformance applications and Azure OpenAI. The gateway must enforce per-application policy, PII masking, rate limits, budgets, and streaming support. The team evaluated several implementation approaches.

## Decision

Build the gateway in Go (1.24+) as a purpose-built HTTP proxy, rather than adopting LiteLLM as the core routing layer.

## Options considered

### Option 1: LiteLLM as core
- Pros: ready-made multi-provider support; lower initial development time.
- Cons: Python runtime adds operational complexity; PII masking and Brazilian CPF/CNPJ/Luhn logic would be harder to audit; less control over middleware chain and streaming behavior; vendor lock-in risk.

### Option 2 (chosen): Go custom gateway
- Pros: single static binary; explicit control over every pipeline stage; excellent standard library (`net/http`, `crypto/subtle`, `log/slog`); strong typing makes policy enforcement robust; easy to audit for security.
- Cons: more initial development effort; no built-in multi-provider routing (acceptable for Phase 1 — Azure OpenAI only).
- Why: the PII masking and security guardrail requirements demand full control. The static binary simplifies container deployment and auditing.

## Consequences

### Positive
- Single binary; no Python runtime dependency.
- Standard library covers most needs; minimal external dependencies.
- Explicit middleware chain makes security guarantees auditable.

### Negative / Trade-offs
- Multi-provider support must be implemented manually (deferred to Phase 2).
- No built-in admin UI or dashboard (deferred to Phase 2).

### Mitigations
- Provider abstraction (`internal/providers.Provider` interface) allows adding providers without changing business logic.

## References
- SPEC.md §1.1 — mission and non-goals
- SPEC.md §18 — out of scope for Phase 1
