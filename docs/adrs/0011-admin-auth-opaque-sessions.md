# ADR-0011: Admin authentication — opaque session tokens over JWT

- **Status**: accepted
- **Date**: 2026-05-20
- **Decision makers**: Danirek (Software Architect, Digital Innovation — Teleperformance Brasil)

## Context

The Admin API introduced in V2 requires its own authentication mechanism, separate from the
consumer-facing Bearer tokens used by applications. Admins are humans operating through a
browser-based React UI. The requirements are:

1. Credentials must be verifiable without storing plaintext passwords
2. Sessions must be revocable (immediately, on logout or security incident)
3. No new signing key infrastructure — the gateway is self-contained and OnPrem
4. The mechanism must work from a browser (cookie) and from API clients (Authorization header)
5. Minimal new dependencies — the gateway already avoids unnecessary third-party libs

## Decision

**Opaque session tokens** with bcrypt-hashed admin passwords.

**Admin credentials:**
- `admin_users` table stores `password_hash` as bcrypt (`cost=12`). Never plaintext.
- Library: `golang.org/x/crypto/bcrypt` (pinned to v0.38.0 in go.mod). This is the first
  direct use of a `golang.org/x` package beyond `golang.org/x/time/rate` (already authorized
  by CLAUDE.md §4.2).

**Session flow:**
1. `POST /admin/v1/auth/login` — verify bcrypt; on success, generate 32 random bytes via
   `crypto/rand`, encode as hex (64-char token), store SHA-256(token) in `admin_sessions`.
2. Return raw token **once** in the login response body. Client stores in memory or cookie.
3. On every admin request, middleware extracts token, computes SHA-256, looks up in
   `admin_sessions` (valid, not revoked, not expired).
4. `DELETE /admin/v1/auth/logout` — sets `revoked_at = NOW()` on the session row.

**Session expiry:** configurable via `admin_session_ttl_hours` (default: 8 hours).

## Options considered

### Option 1: Reuse consumer Bearer token system
- Pros: zero new code
- Cons: consumer tokens are app-level, not user-level; no concept of role; can't distinguish
  an admin action from a consumer API call in audit logs

### Option 2: JWT (JSON Web Tokens)
- Pros: stateless; standard; widely understood
- Cons: requires a signing key (new secret to manage and rotate); tokens cannot be revoked
  without a blocklist (which requires DB anyway, negating statelessness); adds `golang-jwt/jwt`
  dependency (CLAUDE.md §1.5 requires ADR for new dependencies); overkill for single-tenant admin

### Option 3: Opaque session tokens (chosen)
- Pros: revocable (set `revoked_at`); no signing key required; DB lookup is acceptable for
  admin paths (not on the hot data path); simple to audit (`SELECT token_hash FROM admin_sessions`)
- Cons: DB read on every admin request (mitigated: admin traffic is low-volume)
- Why: matches the stated requirement from v2-alignment.md response A: "Auth separada no banco"

### Option 4: mTLS / SSO (corporate auth)
- Pros: enterprise-grade; no password management
- Cons: explicitly deferred to Phase 3+ (SPEC.md §18); requires PKI infrastructure

## Consequences

### Positive
- Sessions are immediately revocable: logout, ban, or key rotation takes effect on next request
- No new signing key to manage; entropy comes from `crypto/rand`
- Full audit trail: `admin_sessions` records `created_at`, `expires_at`, `revoked_at`
- bcrypt cost=12 makes offline dictionary attacks computationally infeasible

### Negative / Trade-offs
- `admin_sessions` table grows over time; requires periodic purge of expired/revoked rows
- One DB round-trip per admin request (acceptable for low-volume admin plane)

### Mitigations
- `DELETE FROM admin_sessions WHERE expires_at < NOW() OR revoked_at IS NOT NULL` runs at
  gateway startup and can be triggered via admin endpoint (V2+)

## References

- docs/v2-alignment.md — response A
- https://pkg.go.dev/golang.org/x/crypto/bcrypt
- https://pkg.go.dev/crypto/rand
- ADR-0009 — DB-backed admin plane (admin_users and admin_sessions tables)
