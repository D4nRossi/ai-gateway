// Package secretsdb implements keyvault.SecretGetter and SecretSetter against
// the `gogateway.secrets` table (ADR-0026). When SQL Server Always Encrypted
// is configured on the `value` column, ciphertext stays at rest in the
// database and the microsoft/go-mssqldb driver transparently decrypts on
// read using the certificate in the Windows LocalMachine\My store.
//
// The package is a drop-in replacement for *keyvault.Client — any consumer
// (config resolver, proxy CredentialResolver from ADR-0020, etc.) sees the
// same interface and behaves identically.
//
// References:
//   - ADR-0026 — Always Encrypted + DPAPI híbrido pra secrets locais
//   - ADR-0018 — Key Vault provider (substituído em deploy Windows pelo db backend)
//   - ADR-0020 — credential storage mode per target (consumer da SecretGetter)
//   - microsoft/go-mssqldb Always Encrypted: https://github.com/microsoft/go-mssqldb
package secretsdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/D4nRossi/ai-gateway/internal/infra/keyvault"
)

// DefaultCacheTTL mirrors keyvault.DefaultCacheTTL so consumers see identical
// hot-path behavior regardless of backend. 5 minutes balances rotation
// propagation (operator updates secret, restart gateway, cache TTL ensures
// new value is read on next miss) against round-trip pressure on the DB.
const DefaultCacheTTL = 5 * time.Minute

// ErrNotFound is returned by Get when the named secret has no row in
// gogateway.secrets. Distinct from keyvault.ErrEmptyValue (which signals a
// row with empty value) so dashboards/alerts can differentiate the cases.
var ErrNotFound = errors.New("secret not found")

// Client caches reads against gogateway.secrets and exposes the same contract
// as keyvault.Client.
//
// Safe for concurrent use. RWMutex on the cache map lets readers of hot
// secrets share without serializing on misses for different keys.
type Client struct {
	db     *sql.DB
	ttl    time.Duration
	logger *slog.Logger

	mu    sync.RWMutex
	cache map[string]entry
}

type entry struct {
	value     string
	expiresAt time.Time
}

// New constructs a Client backed by db with DefaultCacheTTL. logger may be
// nil — falls back to slog.Default() at call time.
func New(db *sql.DB) *Client {
	return &Client{
		db:     db,
		ttl:    DefaultCacheTTL,
		logger: slog.Default(),
		cache:  make(map[string]entry),
	}
}

// WithTTL overrides the cache TTL on an existing Client. Intended for tests
// and tuning; not meant for runtime use.
func (c *Client) WithTTL(ttl time.Duration) *Client {
	c.ttl = ttl
	return c
}

// WithLogger replaces the default slog logger. When unset, the client uses
// slog.Default() which is fine for boot-time.
func (c *Client) WithLogger(logger *slog.Logger) *Client {
	if logger != nil {
		c.logger = logger
	}
	return c
}

// Get returns the secret value for name. Cache-first: hits a fresh entry
// without going to the database. Misses (cold cache or expired entry) issue
// a single-row SELECT and update the cache.
//
// Reasoning: identical RLock-then-Lock pattern as keyvault.Client. Concurrent
// misses on the same key may both query the DB once each — accepted for
// simplicity; the working set is small (~10 secrets) and per-key
// singleflight is overkill.
//
// References:
//   - ADR-0026
//   - keyvault.Client.Get (mirror semantics)
func (c *Client) Get(ctx context.Context, name string) (string, error) {
	now := time.Now()

	c.mu.RLock()
	e, ok := c.cache[name]
	c.mu.RUnlock()
	if ok && now.Before(e.expiresAt) {
		c.log().Debug("secretsdb: cache hit", "secret_name", name)
		return e.value, nil
	}

	const q = `SELECT value FROM gogateway.secrets WHERE name = @p1`
	var value []byte
	err := c.db.QueryRowContext(ctx, q, name).Scan(&value)
	latency := time.Since(now)
	if errors.Is(err, sql.ErrNoRows) {
		c.log().Warn("secretsdb: secret not found",
			"secret_name", name, "latency_ms", latency.Milliseconds(),
		)
		return "", fmt.Errorf("secret %q: %w", name, ErrNotFound)
	}
	if err != nil {
		c.log().Error("secretsdb: fetch failed",
			"secret_name", name, "latency_ms", latency.Milliseconds(), "err", err,
		)
		return "", fmt.Errorf("fetching secret %q from db: %w", name, err)
	}
	if len(value) == 0 {
		c.log().Error("secretsdb: secret exists but value is empty",
			"secret_name", name, "latency_ms", latency.Milliseconds(),
		)
		return "", fmt.Errorf("secret %q: %w", name, keyvault.ErrEmptyValue)
	}

	v := string(value)
	c.mu.Lock()
	c.cache[name] = entry{value: v, expiresAt: now.Add(c.ttl)}
	c.mu.Unlock()

	c.log().Info("secretsdb: secret loaded",
		"event_type", "secret_loaded",
		"secret_name", name,
		"provider", "db",
		"value_length", len(v),
		"latency_ms", latency.Milliseconds(),
	)
	return v, nil
}

// Set persists value for name. Creates the row when absent, updates when
// present (idempotent for callers). Always Encrypted column receives plaintext
// from this side — the driver encrypts using the registered CMK provider.
//
// Reasoning: empty value rejected upfront (consistent with keyvault.Client.Set).
// MERGE pattern keeps the operation atomic and idempotent without requiring
// the caller to know whether the secret exists.
//
// References:
//   - ADR-0026 §Implementation
func (c *Client) Set(ctx context.Context, name, value string) error {
	if name == "" {
		return errors.New("secret name is required")
	}
	if value == "" {
		return fmt.Errorf("secret %q: %w", name, keyvault.ErrEmptyValue)
	}

	const q = `
		MERGE gogateway.secrets WITH (HOLDLOCK) AS target
		USING (SELECT @p1 AS name) AS source
		ON target.name = source.name
		WHEN MATCHED THEN
		    UPDATE SET value = @p2, updated_at = SYSUTCDATETIME()
		WHEN NOT MATCHED THEN
		    INSERT (name, value) VALUES (@p1, @p2);`

	start := time.Now()
	_, err := c.db.ExecContext(ctx, q, name, []byte(value))
	latency := time.Since(start)
	if err != nil {
		c.log().Error("secretsdb: set failed",
			"secret_name", name, "latency_ms", latency.Milliseconds(), "err", err,
		)
		return fmt.Errorf("setting secret %q in db: %w", name, err)
	}

	// Invalidate cache so the next Get returns the new value rather than
	// serving a stale read from the previous version.
	c.mu.Lock()
	delete(c.cache, name)
	c.mu.Unlock()

	c.log().Info("secretsdb: secret set",
		"event_type", "secret_set",
		"secret_name", name,
		"value_length", len(value),
		"latency_ms", latency.Milliseconds(),
	)
	return nil
}

// Delete removes the secret named name from gogateway.secrets. Returns nil
// when the row doesn't exist (idempotent — paridade com behavior comum
// pra delete em CLI de operação).
func (c *Client) Delete(ctx context.Context, name string) error {
	if name == "" {
		return errors.New("secret name is required")
	}

	const q = `DELETE FROM gogateway.secrets WHERE name = @p1`
	res, err := c.db.ExecContext(ctx, q, name)
	if err != nil {
		return fmt.Errorf("deleting secret %q from db: %w", name, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking rows affected for delete of secret %q: %w", name, err)
	}

	c.mu.Lock()
	delete(c.cache, name)
	c.mu.Unlock()

	c.log().Info("secretsdb: secret deleted",
		"event_type", "secret_deleted",
		"secret_name", name,
		"rows_affected", n,
	)
	return nil
}

// SecretMeta is the metadata view exposed by List — never includes value.
type SecretMeta struct {
	Name      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// List returns metadata for all secrets in the table, ordered by name.
// Values are never returned — paridade com a regra "list shows names only"
// do CLI (cmd/secrets list).
func (c *Client) List(ctx context.Context) ([]SecretMeta, error) {
	const q = `SELECT name, created_at, updated_at FROM gogateway.secrets ORDER BY name`
	rows, err := c.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("listing secrets: %w", err)
	}
	defer rows.Close()

	var out []SecretMeta
	for rows.Next() {
		var m SecretMeta
		if err := rows.Scan(&m.Name, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning secret meta row: %w", err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating secret meta rows: %w", err)
	}
	return out, nil
}

// log returns the logger, falling back to slog.Default() when c.logger is nil.
// Defends against test fixtures that build Client literally without going
// through New().
func (c *Client) log() *slog.Logger {
	if c.logger != nil {
		return c.logger
	}
	return slog.Default()
}

// Compile-time assertions: Client implements both keyvault contracts.
var (
	_ keyvault.SecretGetter = (*Client)(nil)
	_ keyvault.SecretSetter = (*Client)(nil)
)
