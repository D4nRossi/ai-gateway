package proxyservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/D4nRossi/ai-gateway/internal/domain/endpoint"
	"github.com/D4nRossi/ai-gateway/internal/infra/keyvault"
)

// CredentialResolver returns the plaintext TargetAuth for a proxy Target based
// on its CredentialStorageMode (ADR-0020).
//
// The proxy engine calls Resolve once per request right before injecting
// credentials into the forwarded request. This lets the resolver short-circuit
// to the in-memory AES copy when mode=aes (status quo) and reach out to Key
// Vault only when explicitly opted in by the operator.
type CredentialResolver interface {
	Resolve(ctx context.Context, t endpoint.Target) (endpoint.TargetAuth, error)
}

// DefaultKVTimeout is the maximum time the resolver waits for a Key Vault
// fetch before failing (mode=kv) or falling back to the AES cache (mode=both).
//
// Reasoning: 200 ms was agreed with owner in the ADR-0020 discussion as the
// budget that keeps the p99 of the proxy plane within an acceptable envelope
// while still allowing the KV cache (5 min TTL inside keyvault.Client) to
// absorb most calls without ever hitting this deadline. Per-target override
// is out of scope for V1; future ADR may revisit.
const DefaultKVTimeout = 200 * time.Millisecond

// ErrKVTimeout signals that a Key Vault fetch exceeded DefaultKVTimeout.
//
// Internal sentinel used to drive fallback in mode=both; in mode=kv it
// propagates to the caller which maps it to HTTP 503.
var ErrKVTimeout = errors.New("key vault fetch timed out")

// kvCredentialResolver implements CredentialResolver against a SecretGetter
// (typically *keyvault.Client) and a configurable timeout.
type kvCredentialResolver struct {
	kv        keyvault.SecretGetter
	kvTimeout time.Duration
	logger    *slog.Logger
}

// NewCredentialResolver constructs a CredentialResolver backed by kv and using
// DefaultKVTimeout. logger may be nil — falls back to slog.Default().
//
// kv may also be nil, which signals "Key Vault not configured" (e.g.
// KEYVAULT_URI is empty during local development). In that case:
//   - mode aes (default): works as before
//   - mode kv: every Resolve fails with a clear configuration error
//   - mode both: falls back to the AES cache and logs kv_fallback_used with
//     reason=kv_not_configured (once per request, not once per resolver)
//
// Reasoning: the resolver only needs the SecretGetter contract, not the full
// keyvault.Client, so tests can substitute a fake. Allowing nil kv keeps the
// boot path simple for the AES-only setup that V1 explicitly supports.
func NewCredentialResolver(kv keyvault.SecretGetter, logger *slog.Logger) CredentialResolver {
	return &kvCredentialResolver{
		kv:        kv,
		kvTimeout: DefaultKVTimeout,
		logger:    logger,
	}
}

// WithKVTimeout overrides the default KV timeout. Intended for tests and
// tuning experiments only — production code should use DefaultKVTimeout.
func (r *kvCredentialResolver) WithKVTimeout(d time.Duration) *kvCredentialResolver {
	r.kvTimeout = d
	return r
}

// Resolve returns the plaintext credentials for the target according to its
// CredentialStorageMode.
//
// Mode mapping (ADR-0020):
//   - CredentialModeAES (or empty for legacy rows): returns t.Auth as already
//     decrypted by the repository at load time. No KV interaction.
//   - CredentialModeKV: fetches from Key Vault with DefaultKVTimeout. Failure
//     surfaces as an error — no fallback.
//   - CredentialModeBoth: tries KV first; on error (timeout or otherwise),
//     returns t.Auth (the AES freshness cache) and emits a warn-level log
//     event_type=kv_fallback_used.
//
// References:
//   - ADR-0020 — credential storage mode per target
//   - ADR-0018 — Key Vault resolver with 5 min cache consumed via SecretGetter
//   - ADR-0012 — AES-256-GCM encryption of the auth_config_enc cache
func (r *kvCredentialResolver) Resolve(ctx context.Context, t endpoint.Target) (endpoint.TargetAuth, error) {
	mode := t.CredentialStorageMode
	if mode == "" {
		// Implicit AES default for rows persisted before migration 011.
		mode = endpoint.CredentialModeAES
	}

	if mode == endpoint.CredentialModeAES {
		// Repository already decrypted t.Auth on load.
		return t.Auth, nil
	}

	if mode != endpoint.CredentialModeKV && mode != endpoint.CredentialModeBoth {
		return endpoint.TargetAuth{}, fmt.Errorf("target id=%d: unsupported credential_storage_mode %q", t.ID, mode)
	}

	// Modes kv and both require a configured Key Vault. When kv is nil the
	// resolver short-circuits: kv-only surfaces an error; both silently
	// degrades to the AES cache so the proxy plane keeps serving.
	if r.kv == nil {
		if mode == endpoint.CredentialModeBoth {
			r.log().Warn("credential_resolver: kv not configured, using AES fallback",
				"event_type", "kv_fallback_used",
				"target_id", t.ID,
				"kv_secret_name", t.KVSecretName,
				"reason", "kv_not_configured",
			)
			return t.Auth, nil
		}
		return endpoint.TargetAuth{}, fmt.Errorf("target id=%d in mode %q: key vault not configured (KEYVAULT_URI is empty)", t.ID, mode)
	}

	auth, err := r.fetchFromKV(ctx, t)
	if err == nil {
		return auth, nil
	}
	if mode == endpoint.CredentialModeBoth {
		r.log().Warn("credential_resolver: kv fallback used",
			"event_type", "kv_fallback_used",
			"target_id", t.ID,
			"kv_secret_name", t.KVSecretName,
			"kv_error", err.Error(),
		)
		return t.Auth, nil
	}
	return endpoint.TargetAuth{}, err
}

// fetchFromKV performs the Key Vault fetch under a bounded timeout.
// The returned error wraps ErrKVTimeout when the deadline expired so callers
// (and the both-mode fallback) can recognise the cause.
func (r *kvCredentialResolver) fetchFromKV(ctx context.Context, t endpoint.Target) (endpoint.TargetAuth, error) {
	if t.KVSecretName == "" {
		return endpoint.TargetAuth{}, fmt.Errorf("target id=%d in mode %q: kv_secret_name is empty", t.ID, t.CredentialStorageMode)
	}

	kvCtx, cancel := context.WithTimeout(ctx, r.kvTimeout)
	defer cancel()

	raw, err := r.kv.Get(kvCtx, t.KVSecretName)
	if err != nil {
		// Both the underlying error and the derived context can carry the
		// deadline signal depending on where cancellation was observed.
		// Check both so callers see ErrKVTimeout consistently.
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(kvCtx.Err(), context.DeadlineExceeded) {
			return endpoint.TargetAuth{}, fmt.Errorf("fetching kv secret %q for target id=%d: %w", t.KVSecretName, t.ID, ErrKVTimeout)
		}
		return endpoint.TargetAuth{}, fmt.Errorf("fetching kv secret %q for target id=%d: %w", t.KVSecretName, t.ID, err)
	}

	var auth endpoint.TargetAuth
	if err := json.Unmarshal([]byte(raw), &auth); err != nil {
		return endpoint.TargetAuth{}, fmt.Errorf("parsing kv secret %q for target id=%d: %w", t.KVSecretName, t.ID, err)
	}
	return auth, nil
}

func (r *kvCredentialResolver) log() *slog.Logger {
	if r.logger != nil {
		return r.logger
	}
	return slog.Default()
}
