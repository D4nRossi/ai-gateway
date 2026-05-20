// Package config loads, expands, and validates the gateway YAML configuration.
//
// Structural settings (apps, models, tiers, timeouts) live in configs/gateway.yaml.
// Secrets (API keys, DB password) are supplied via environment variables referenced
// in the YAML as ${VAR_NAME} placeholders; they are expanded by os.ExpandEnv at load time.
//
// The entry point for callers is [Load], which returns a validated [Config] or an
// error listing all validation failures. On any validation failure the caller should
// log the error and exit.
//
// References:
//   - SPEC.md §4 — configuration schema
//   - SPEC.md §4.1 — required validations
//   - CLAUDE.md §10 — configuration and secrets policy
package config
