package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/D4nRossi/ai-gateway/internal/infra/dpapi"
)

// loadDPAPIEnvFile decrypts a DPAPI-protected KEY=VALUE file (ADR-0026) and
// exposes its contents as process environment variables. Lines starting with
// '#' and blank lines are ignored. Values may be wrapped in double quotes
// (stripped on load), which lets operators include `;` or `=` in secrets.
//
// On non-Windows the underlying DPAPI call returns ErrUnsupportedOS — the
// caller already gated this function behind a runtime check, so it is safe
// to fail loudly here.
//
// References:
//   - ADR-0026 §Bootstrap (DPAPI path)
//   - internal/infra/dpapi — wrapper
func loadDPAPIEnvFile(path string) error {
	ciphertext, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading dpapi env file: %w", err)
	}
	if len(ciphertext) == 0 {
		return fmt.Errorf("dpapi env file %q is empty", path)
	}

	plaintext, err := dpapi.Unprotect(ciphertext)
	if err != nil {
		return fmt.Errorf("unprotecting dpapi env file: %w", err)
	}

	scanner := bufio.NewScanner(strings.NewReader(string(plaintext)))
	// Generous buffer pra suportar valores longos (e.g. hex AES-256 = 64 chars).
	scanner.Buffer(make([]byte, 1<<10), 1<<16)

	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 1 {
			return fmt.Errorf("dpapi env file %q: malformed line %d (expected KEY=VALUE)", path, lineNo)
		}
		key := strings.TrimSpace(line[:eq])
		value := strings.TrimSpace(line[eq+1:])
		// Strip optional surrounding quotes so operadores podem incluir
		// `=` / `;` / `#` dentro de valores (e.g. JSON).
		if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
			value = value[1 : len(value)-1]
		}
		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("dpapi env file %q line %d: setenv %q: %w", path, lineNo, key, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scanning dpapi env file: %w", err)
	}
	return nil
}

// noopSecretResolverDB is wired as config.SecretResolver when
// SECRET_PROVIDER=db. Any ${kv:NAME} reference in the YAML triggers a clear
// error guiding the operator to migrate to ${VAR_NAME} (DPAPI env) instead.
//
// Rationale: in db mode the boot path resolves secrets through env vars
// populated by DPAPI; ${kv:NAME} is meaningless because there's no Azure
// Key Vault. Returning a typed error early surfaces config drift on the
// next deploy instead of silently using a placeholder string.
type noopSecretResolverDB struct{}

func (noopSecretResolverDB) Get(_ context.Context, name string) (string, error) {
	return "", fmt.Errorf(
		"SECRET_PROVIDER=db: ${kv:%s} not supported in this mode — "+
			"use ${%s} sourced from the DPAPI env file (ADR-0026)",
		name, strings.ReplaceAll(strings.ToUpper(name), "-", "_"),
	)
}
