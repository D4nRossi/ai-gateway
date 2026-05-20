# ADR-0012: Encryption at rest for proxy target credentials (AES-256-GCM)

- **Status**: accepted
- **Date**: 2026-05-20
- **Decision makers**: Danirek (Software Architect, Digital Innovation — Teleperformance Brasil)

## Context

V2's generic HTTP proxy forwards requests to external upstreams that may require authentication:
Bearer tokens, API keys in custom headers, or HTTP basic auth. These credentials must be stored
in the database (`proxy_targets.auth_config_enc`) so the proxy engine can retrieve and inject them
at runtime.

Storing credentials as plaintext in the database is unacceptable: a SQL injection, a DB backup
without access controls, or a compromised read replica would expose all upstream credentials.

The deployment context is OnPrem (Oracle Linux, Docker Compose) without access to Azure Key Vault
or HashiCorp Vault.

## Decision

**AES-256-GCM symmetric encryption** using only Go standard library (`crypto/aes`, `crypto/cipher`,
`crypto/rand`). Zero new dependencies.

**Key management:**
- Encryption key stored in env var `GATEWAY_ENCRYPTION_KEY` as 64-char hex (32 bytes = AES-256).
- Generated once by the operator: `openssl rand -hex 32`
- Never stored in the DB, never logged, never in config files.

**Encryption scheme:**
- Plaintext: JSON-marshalled `TargetAuth` struct (auth type + credentials)
- Nonce: 12 bytes from `crypto/rand` (GCM standard nonce size), generated fresh per encryption
- Ciphertext: `nonce (12 bytes) || AES-256-GCM-ciphertext`
- Storage: BYTEA column `auth_config_enc`; NULL when `auth_type = 'none'`

**Location:** `internal/infra/crypto/crypto.go` — `Encrypter` interface + `AESGCMEncrypter` impl.

## Options considered

### Option 1: Plaintext credentials in DB
- Pros: none (unacceptable)
- Cons: exposes all upstream credentials on any DB read

### Option 2: AES-256-GCM stdlib (chosen)
- Pros: AEAD (authenticated + encrypted — prevents tampering as well as disclosure);
  nonce-per-message ensures ciphertext uniqueness even for identical plaintexts;
  zero new dependencies; well-understood
- Cons: symmetric — key rotation requires re-encrypting all rows (manual admin operation)
- Why: the standard recommendation for symmetric encryption at rest; no external KMS available

### Option 3: External KMS (Azure Key Vault)
- Pros: key never in process memory; hardware-backed key; audit trail for key usage
- Cons: adds cloud dependency; out of scope for OnPrem V2 without Azure connectivity for KMS;
  adds latency on every credential fetch; requires additional Azure RBAC config

### Option 4: HashiCorp Vault
- Pros: purpose-built secrets management; dynamic secrets
- Cons: heavy new infrastructure component; not available in the OnPrem Docker Compose stack

## Consequences

### Positive
- Credentials encrypted in transit (DB TLS) and at rest (AES-256-GCM)
- Tampering detected: GCM authentication tag will fail on any bit flip in ciphertext
- No new runtime dependencies

### Negative / Trade-offs
- Key rotation: changing `GATEWAY_ENCRYPTION_KEY` requires re-encrypting all `auth_config_enc`
  rows. There is no automatic key versioning.
- The encryption key itself must be protected (operator responsibility: chmod 600 on `.env`,
  restrict Docker secret access)

### Mitigations
- Document key rotation procedure in README: decrypt all + re-encrypt with new key via admin tool
- Key rotation is infrequent for stable deployments; risk window is acceptable for OnPrem V2

## References

- docs/v2-alignment.md — response E (auth types for proxy targets)
- https://pkg.go.dev/crypto/aes
- https://pkg.go.dev/crypto/cipher
- https://pkg.go.dev/crypto/rand
- NIST SP 800-38D — GCM specification: https://csrc.nist.gov/publications/detail/sp/800-38d/final
- docs/security.md — §7 (secrets management OnPrem)
