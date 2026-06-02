// Package keyvault wraps the Azure Key Vault Secrets API with a small in-memory
// cache so the gateway can resolve ${kv:NAME} references from the YAML config
// without paying 100-300ms per lookup on every read.
//
// Authentication uses azidentity.DefaultAzureCredential, which in development
// picks up `az login` credentials transparently and in production falls back
// to Managed Identity — no client secret lives in the gateway container.
//
// The cache is intentionally lazy and TTL-based (default 5 min):
//
//   - First Get(name) call hits the vault.
//   - Subsequent calls within the TTL are served from memory.
//   - After expiry, the next call refreshes — there is no proactive prefetch
//     goroutine because the working set is tiny (~5 secrets per boot).
//
// All errors from Get are wrapped with the secret name as context. Callers
// (config resolver, future on-demand lookups) MUST treat a Get failure as
// fatal at boot — see ADR-0018.
//
// References:
//   - ADR-0018 — Azure Key Vault como provider de segredos
//   - https://pkg.go.dev/github.com/Azure/azure-sdk-for-go/sdk/security/keyvault/azsecrets
//   - https://pkg.go.dev/github.com/Azure/azure-sdk-for-go/sdk/azidentity
package keyvault
