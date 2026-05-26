package keyvault

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/security/keyvault/azsecrets"
)

// DefaultCacheTTL is the time a fetched secret stays valid in memory before
// the next Get triggers a refresh from Azure Key Vault. See ADR-0018 for the
// trade-off rationale (latency on hot path vs how quickly rotated secrets
// propagate to the gateway).
const DefaultCacheTTL = 5 * time.Minute

// ErrEmptyValue is returned when a secret exists in Key Vault but its value
// is nil/empty. Treated as fatal — a configuration field referring to an
// empty secret would silently produce broken downstream behavior.
var ErrEmptyValue = errors.New("secret value is empty")

// SecretGetter is the contract consumed by the config resolver and any other
// code that needs to fetch a secret by name. Implemented by *Client in this
// package; defined as an interface so tests can substitute a fake.
type SecretGetter interface {
	Get(ctx context.Context, name string) (string, error)
}

// azClient is the subset of *azsecrets.Client we depend on. Extracting it as
// an interface keeps Client testable without spinning up a real vault.
type azClient interface {
	GetSecret(ctx context.Context, name, version string, opts *azsecrets.GetSecretOptions) (azsecrets.GetSecretResponse, error)
}

// Client resolves Key Vault secrets with a small in-memory cache.
//
// Safe for concurrent use. The cache uses an RWMutex so concurrent reads of
// hot secrets do not block each other; only the actual fetch on a miss takes
// the write lock.
type Client struct {
	az  azClient
	ttl time.Duration

	mu    sync.RWMutex
	cache map[string]entry
}

type entry struct {
	value     string
	expiresAt time.Time
}

// New constructs a Client backed by Azure Key Vault at vaultURL, authenticated
// via DefaultAzureCredential. Returns a meaningful error if the credential
// chain can't be initialized or the URL is malformed.
//
// vaultURL must be the full Vault URI (e.g. https://danieldev.vault.azure.net/).
//
// References:
//   - https://learn.microsoft.com/azure/developer/go/azure-sdk-authentication
func New(vaultURL string) (*Client, error) {
	if vaultURL == "" {
		return nil, errors.New("vaultURL is required")
	}

	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("creating Azure credential: %w", err)
	}

	azc, err := azsecrets.NewClient(vaultURL, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("creating Key Vault client for %q: %w", vaultURL, err)
	}

	return &Client{
		az:    azc,
		ttl:   DefaultCacheTTL,
		cache: make(map[string]entry),
	}, nil
}

// WithTTL overrides the default cache TTL on an existing Client. Intended for
// tests and tuning; not meant for runtime use.
func (c *Client) WithTTL(ttl time.Duration) *Client {
	c.ttl = ttl
	return c
}

// Get returns the secret value for name. Cache-first: hits a fresh entry
// without going to the network. Misses (cold cache or expired entry) fetch
// from Azure and update the cache.
//
// Reasoning: the RLock-then-Lock pattern lets multiple concurrent readers
// share the cache without serializing on misses for different keys. There is
// a small window where two goroutines may both miss the same key and both
// fetch — accepted, since the second fetch overwrites the first and the
// extra Azure call is harmless. Avoiding it would require a per-key
// singleflight, which is overkill for the working set we expect (~5 secrets).
func (c *Client) Get(ctx context.Context, name string) (string, error) {
	now := time.Now()

	c.mu.RLock()
	e, ok := c.cache[name]
	c.mu.RUnlock()
	if ok && now.Before(e.expiresAt) {
		return e.value, nil
	}

	resp, err := c.az.GetSecret(ctx, name, "", nil)
	if err != nil {
		return "", fmt.Errorf("fetching secret %q from key vault: %w", name, err)
	}
	if resp.Value == nil || *resp.Value == "" {
		return "", fmt.Errorf("secret %q: %w", name, ErrEmptyValue)
	}

	value := *resp.Value
	c.mu.Lock()
	c.cache[name] = entry{value: value, expiresAt: now.Add(c.ttl)}
	c.mu.Unlock()

	return value, nil
}

// Compile-time assertion: Client implements SecretGetter.
var _ SecretGetter = (*Client)(nil)
