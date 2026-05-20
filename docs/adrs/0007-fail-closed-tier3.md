# ADR-0007: fail-closed for Tier 3 vs. fail-open for Tier 1/2

- **Status**: accepted
- **Date**: 2026-05-19
- **Decision makers**: Danirek

## Context

When an external security service (Azure Content Safety) is unavailable or times out, the gateway must decide whether to block or allow the request. Different applications have different risk profiles.

## Decision

- **Tier 1 and Tier 2**: fail-open — Azure CS unavailable → continue with `warn` log and audit event.
- **Tier 3**: fail-closed — Azure CS unavailable → 503 + audit event + block request.

## Options considered

### Option 1: Always fail-open
- Pros: maximum availability; Azure CS outage never blocks business traffic.
- Cons: Tier 3 applications (highest risk, most sensitive data) would send unscreened prompts to the model during CS outages.

### Option 2: Always fail-closed
- Pros: strongest security guarantee.
- Cons: Azure CS outage blocks all traffic, including Tier 1 and Tier 2 apps with minimal risk profiles.

### Option 3 (chosen): Fail mode per tier
- Pros: risk-proportional response — Tier 3 security is never compromised; Tier 1/2 maintain availability.
- Cons: slightly more complex implementation.
- Why: Tier 3 applications send sensitive data and must have strong guardrail guarantees. Tier 1 apps (basic PII masking only) do not have this risk profile.

## Consequences

### Positive
- Tier 3 security guarantees are maintained even under Azure CS outage.
- Tier 1/2 availability is not impacted by external service outages.

### Negative / Trade-offs
- Tier 3 availability depends on Azure CS availability.

### Mitigations
- Local heuristics (promptshield/local.go) provide a fallback layer for Tier 2.
- Tier 3 fail-closed blocks are audited so operators can respond quickly.

## References
- SPEC.md §11.4 — fail-mode semantics
- SPEC.md §5.3 — tier pipeline table
