package proxyservice

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/D4nRossi/ai-gateway/internal/domain/endpoint"
)

// fakeKV is an in-memory SecretGetter for tests. It supports configurable
// latency (to exercise the 200 ms timeout path) and a sticky error injection.
//
// References:
//   - ADR-0020 — credential storage mode per target
type fakeKV struct {
	values map[string]string
	err    error
	delay  time.Duration
	calls  int32
}

func (f *fakeKV) Get(ctx context.Context, name string) (string, error) {
	atomic.AddInt32(&f.calls, 1)
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	if f.err != nil {
		return "", f.err
	}
	v, ok := f.values[name]
	if !ok {
		return "", errors.New("secret not found")
	}
	return v, nil
}

func (f *fakeKV) callCount() int32 { return atomic.LoadInt32(&f.calls) }

// silentLogger discards log output so tests don't pollute stdout.
func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// makeAuth builds a populated TargetAuth for use as the "AES-decrypted" Auth
// field on a Target. Distinct from the KV payload so tests can tell which
// source the resolver returned.
func makeAuth(token string) endpoint.TargetAuth {
	return endpoint.TargetAuth{Type: endpoint.AuthBearerToken, Token: token}
}

// kvJSON returns a marshalled TargetAuth ready to be stored in fakeKV.values.
// The shape matches what cmd/migrate-targets-to-kv and the admin handlers
// will persist (ADR-0020).
func kvJSON(t *testing.T, a endpoint.TargetAuth) string {
	t.Helper()
	b, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshalling test auth: %v", err)
	}
	return string(b)
}

// newResolverWith builds a kvCredentialResolver against the given fake and a
// custom KV timeout. Returns the concrete type (not the interface) so tests
// can keep tweaking internals without re-casting.
func newResolverWith(kv *fakeKV, timeout time.Duration) *kvCredentialResolver {
	r := &kvCredentialResolver{
		kv:        kv,
		kvTimeout: timeout,
		logger:    silentLogger(),
	}
	if kv == nil {
		r.kv = nil
	}
	return r
}

// ── Happy paths ──────────────────────────────────────────────────────────────

func TestResolve_AESMode_ReturnsTargetAuth(t *testing.T) {
	t.Parallel()

	aesAuth := makeAuth("from-aes")
	kv := &fakeKV{} // configured but not expected to be hit
	r := newResolverWith(kv, DefaultKVTimeout)

	got, err := r.Resolve(context.Background(), endpoint.Target{
		ID:                    1,
		CredentialStorageMode: endpoint.CredentialModeAES,
		Auth:                  aesAuth,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Token != "from-aes" {
		t.Errorf("token = %q; want from-aes", got.Token)
	}
	if kv.callCount() != 0 {
		t.Errorf("kv calls = %d; want 0 (aes mode must not touch KV)", kv.callCount())
	}
}

func TestResolve_EmptyMode_DefaultsToAES(t *testing.T) {
	t.Parallel()

	// Legacy rows persisted before migration 011 ran have credential_storage_mode = "".
	// The resolver must treat them as aes so existing deployments keep working.
	aesAuth := makeAuth("legacy")
	kv := &fakeKV{}
	r := newResolverWith(kv, DefaultKVTimeout)

	got, err := r.Resolve(context.Background(), endpoint.Target{
		ID:                    2,
		CredentialStorageMode: "",
		Auth:                  aesAuth,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Token != "legacy" {
		t.Errorf("token = %q; want legacy", got.Token)
	}
	if kv.callCount() != 0 {
		t.Errorf("kv calls = %d; want 0", kv.callCount())
	}
}

func TestResolve_KVMode_HappyPath(t *testing.T) {
	t.Parallel()

	kvAuth := makeAuth("from-kv")
	kv := &fakeKV{values: map[string]string{
		"gateway-target-abc": kvJSON(t, kvAuth),
	}}
	r := newResolverWith(kv, DefaultKVTimeout)

	got, err := r.Resolve(context.Background(), endpoint.Target{
		ID:                    3,
		CredentialStorageMode: endpoint.CredentialModeKV,
		KVSecretName:          "gateway-target-abc",
		// Auth intentionally empty/AuthNone — mode=kv must not fall back to it.
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Token != "from-kv" {
		t.Errorf("token = %q; want from-kv", got.Token)
	}
	if kv.callCount() != 1 {
		t.Errorf("kv calls = %d; want 1", kv.callCount())
	}
}

func TestResolve_BothMode_KVHealthy_ReturnsKV(t *testing.T) {
	t.Parallel()

	// In both mode, when KV responds in time the resolver MUST return the KV
	// value, not the AES cache. Otherwise rotation via KV would never take effect.
	aesAuth := makeAuth("stale-aes")
	kvAuth := makeAuth("fresh-kv")
	kv := &fakeKV{values: map[string]string{
		"gateway-target-xyz": kvJSON(t, kvAuth),
	}}
	r := newResolverWith(kv, DefaultKVTimeout)

	got, err := r.Resolve(context.Background(), endpoint.Target{
		ID:                    4,
		CredentialStorageMode: endpoint.CredentialModeBoth,
		KVSecretName:          "gateway-target-xyz",
		Auth:                  aesAuth,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Token != "fresh-kv" {
		t.Errorf("token = %q; want fresh-kv (KV should win when healthy)", got.Token)
	}
}

// ── Failure & fallback paths ─────────────────────────────────────────────────

func TestResolve_KVMode_Timeout_PropagatesErrKVTimeout(t *testing.T) {
	t.Parallel()

	// fakeKV delays 50 ms; resolver timeout is 5 ms → context deadline expires.
	kv := &fakeKV{
		values: map[string]string{"slow-secret": `{"Type":"bearer_token","Token":"x"}`},
		delay:  50 * time.Millisecond,
	}
	r := newResolverWith(kv, 5*time.Millisecond)

	_, err := r.Resolve(context.Background(), endpoint.Target{
		ID:                    5,
		CredentialStorageMode: endpoint.CredentialModeKV,
		KVSecretName:          "slow-secret",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrKVTimeout) {
		t.Errorf("err = %v; want wrapped ErrKVTimeout", err)
	}
}

func TestResolve_KVMode_GenericError_PropagatesNonTimeout(t *testing.T) {
	t.Parallel()

	kv := &fakeKV{err: errors.New("kv 403 forbidden")}
	r := newResolverWith(kv, DefaultKVTimeout)

	_, err := r.Resolve(context.Background(), endpoint.Target{
		ID:                    6,
		CredentialStorageMode: endpoint.CredentialModeKV,
		KVSecretName:          "any-name",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if errors.Is(err, ErrKVTimeout) {
		t.Errorf("err = %v; should NOT be ErrKVTimeout for non-deadline failures", err)
	}
	if !strings.Contains(err.Error(), "kv 403 forbidden") {
		t.Errorf("err = %v; want underlying message preserved", err)
	}
}

func TestResolve_KVMode_MissingSecretName_Errors(t *testing.T) {
	t.Parallel()

	kv := &fakeKV{}
	r := newResolverWith(kv, DefaultKVTimeout)

	_, err := r.Resolve(context.Background(), endpoint.Target{
		ID:                    7,
		CredentialStorageMode: endpoint.CredentialModeKV,
		// KVSecretName intentionally empty
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if kv.callCount() != 0 {
		t.Errorf("kv calls = %d; want 0 (fail before reaching KV)", kv.callCount())
	}
}

func TestResolve_BothMode_KVTimeout_FallsBackToAES(t *testing.T) {
	t.Parallel()

	aesAuth := makeAuth("cached-aes")
	kv := &fakeKV{
		values: map[string]string{"slow": `{"Type":"bearer_token","Token":"fresh"}`},
		delay:  50 * time.Millisecond,
	}
	r := newResolverWith(kv, 5*time.Millisecond)

	got, err := r.Resolve(context.Background(), endpoint.Target{
		ID:                    8,
		CredentialStorageMode: endpoint.CredentialModeBoth,
		KVSecretName:          "slow",
		Auth:                  aesAuth,
	})
	if err != nil {
		t.Fatalf("expected fallback success, got error: %v", err)
	}
	if got.Token != "cached-aes" {
		t.Errorf("token = %q; want cached-aes (fallback)", got.Token)
	}
}

func TestResolve_BothMode_KVError_FallsBackToAES(t *testing.T) {
	t.Parallel()

	aesAuth := makeAuth("cached-aes")
	kv := &fakeKV{err: errors.New("kv 500")}
	r := newResolverWith(kv, DefaultKVTimeout)

	got, err := r.Resolve(context.Background(), endpoint.Target{
		ID:                    9,
		CredentialStorageMode: endpoint.CredentialModeBoth,
		KVSecretName:          "any",
		Auth:                  aesAuth,
	})
	if err != nil {
		t.Fatalf("expected fallback success, got error: %v", err)
	}
	if got.Token != "cached-aes" {
		t.Errorf("token = %q; want cached-aes", got.Token)
	}
}

func TestResolve_KVMode_InvalidJSON_Errors(t *testing.T) {
	t.Parallel()

	// KV returned non-JSON garbage — resolver must surface a clear error.
	kv := &fakeKV{values: map[string]string{"corrupted": "not json{"}}
	r := newResolverWith(kv, DefaultKVTimeout)

	_, err := r.Resolve(context.Background(), endpoint.Target{
		ID:                    10,
		CredentialStorageMode: endpoint.CredentialModeKV,
		KVSecretName:          "corrupted",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "parsing kv secret") {
		t.Errorf("err = %v; want parse-error message", err)
	}
}

// ── KV not configured (nil SecretGetter) ─────────────────────────────────────

func TestResolve_NilKV_AESMode_Works(t *testing.T) {
	t.Parallel()

	aesAuth := makeAuth("local-only")
	r := newResolverWith(nil, DefaultKVTimeout)

	got, err := r.Resolve(context.Background(), endpoint.Target{
		ID:                    11,
		CredentialStorageMode: endpoint.CredentialModeAES,
		Auth:                  aesAuth,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Token != "local-only" {
		t.Errorf("token = %q; want local-only", got.Token)
	}
}

func TestResolve_NilKV_KVMode_Errors(t *testing.T) {
	t.Parallel()

	r := newResolverWith(nil, DefaultKVTimeout)

	_, err := r.Resolve(context.Background(), endpoint.Target{
		ID:                    12,
		CredentialStorageMode: endpoint.CredentialModeKV,
		KVSecretName:          "any",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "key vault not configured") {
		t.Errorf("err = %v; want configuration-error message", err)
	}
}

func TestResolve_NilKV_BothMode_FallsBackToAES(t *testing.T) {
	t.Parallel()

	aesAuth := makeAuth("graceful-degrade")
	r := newResolverWith(nil, DefaultKVTimeout)

	got, err := r.Resolve(context.Background(), endpoint.Target{
		ID:                    13,
		CredentialStorageMode: endpoint.CredentialModeBoth,
		KVSecretName:          "x",
		Auth:                  aesAuth,
	})
	if err != nil {
		t.Fatalf("expected fallback success, got error: %v", err)
	}
	if got.Token != "graceful-degrade" {
		t.Errorf("token = %q; want graceful-degrade", got.Token)
	}
}

// ── Mode validation ──────────────────────────────────────────────────────────

func TestResolve_InvalidMode_Errors(t *testing.T) {
	t.Parallel()

	r := newResolverWith(&fakeKV{}, DefaultKVTimeout)

	_, err := r.Resolve(context.Background(), endpoint.Target{
		ID:                    14,
		CredentialStorageMode: endpoint.CredentialStorageMode("nonsense"),
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported credential_storage_mode") {
		t.Errorf("err = %v; want unsupported-mode message", err)
	}
}
