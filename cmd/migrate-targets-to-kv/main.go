// Command migrate-targets-to-kv promotes a proxy target's credential storage
// from AES (status quo) to Key Vault, switching the credential_storage_mode
// to "kv" or "both" (ADR-0020).
//
// Usage:
//
//	# Move target 42 to mode=both (KV authoritative + AES cache)
//	go run ./cmd/migrate-targets-to-kv -target-id 42 -mode both
//
//	# Move target 42 to mode=kv (KV only; auth_config_enc cleared)
//	go run ./cmd/migrate-targets-to-kv -target-id 42 -mode kv
//
//	# Custom KV secret name (default is "gateway-target-{uuid_v7}")
//	go run ./cmd/migrate-targets-to-kv -target-id 42 -mode both -secret-name my-custom-secret
//
// Environment:
//
//	KEYVAULT_URI — required, e.g. https://danieldev.vault.azure.net/
//	az login        — DefaultAzureCredential must be able to reach the vault
//
// Config: reads configs/gateway.yaml by default (override with -config) so the
// same database connection and AES encryption key the gateway uses are picked
// up automatically.
//
// Idempotency: refuses to run against a target whose credential_storage_mode
// is not "aes" — re-running against an already-migrated target is a no-op
// with a clear message. To rotate a KV-backed credential, use the admin API,
// not this CLI.
//
// References:
//   - ADR-0020 — credential storage mode per target
//   - ADR-0012 — AES-256-GCM target credentials at rest
//   - ADR-0018 — Key Vault provider with cached resolver
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"regexp"

	"github.com/google/uuid"

	"github.com/D4nRossi/ai-gateway/internal/config"
	"github.com/D4nRossi/ai-gateway/internal/db"
	"github.com/D4nRossi/ai-gateway/internal/domain/endpoint"
	"github.com/D4nRossi/ai-gateway/internal/infra/crypto"
	"github.com/D4nRossi/ai-gateway/internal/infra/keyvault"
)

// kvSecretNamePattern mirrors the Azure Key Vault constraint for secret
// names: 1-127 chars from [A-Za-z0-9-]. Hyphens are allowed; underscores,
// dots and other punctuation are NOT. Enforcing this locally avoids round-
// tripping a 400 from the vault.
var kvSecretNamePattern = regexp.MustCompile(`^[A-Za-z0-9-]{1,127}$`)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "migrate-targets-to-kv: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	targetID := flag.Int64("target-id", 0, "ID of the proxy target to migrate (required)")
	modeRaw := flag.String("mode", "", `target mode after migration: "kv" or "both" (required)`)
	secretName := flag.String("secret-name", "", `optional Key Vault secret name; default is "gateway-target-{uuid_v7}"`)
	cfgPath := flag.String("config", "configs/gateway.yaml", "path to gateway.yaml")
	flag.Parse()

	if *targetID <= 0 {
		flag.Usage()
		return errors.New("-target-id is required and must be positive")
	}
	mode := endpoint.CredentialStorageMode(*modeRaw)
	if mode != endpoint.CredentialModeKV && mode != endpoint.CredentialModeBoth {
		return fmt.Errorf(`-mode must be %q or %q`, endpoint.CredentialModeKV, endpoint.CredentialModeBoth)
	}

	ctx := context.Background()

	// ── Bootstrap: KV client + config + DB + AES encrypter ────────────────────
	vaultURL := os.Getenv("KEYVAULT_URI")
	if vaultURL == "" {
		return errors.New("KEYVAULT_URI environment variable is required")
	}
	kvClient, err := keyvault.New(vaultURL)
	if err != nil {
		return fmt.Errorf("initializing key vault client: %w", err)
	}

	cfg, err := config.Load(ctx, *cfgPath, kvClient)
	if err != nil {
		return fmt.Errorf("loading config %q: %w", *cfgPath, err)
	}

	dbHandle, err := db.NewMSSQL(ctx, cfg.Database)
	if err != nil {
		return fmt.Errorf("connecting to sqlserver: %w", err)
	}
	defer dbHandle.Close()

	encrypter, err := crypto.NewAESGCMEncrypter(cfg.Database.EncryptionKeyHex)
	if err != nil {
		return fmt.Errorf("initializing AES-GCM encrypter: %w", err)
	}

	// ── 1. Load target + idempotency guard ────────────────────────────────────
	currentMode, authType, authEnc, err := loadTarget(ctx, dbHandle, *targetID)
	if err != nil {
		return err
	}
	if currentMode != "" && currentMode != endpoint.CredentialModeAES {
		fmt.Printf("target id=%d is already in mode %q — nothing to do (idempotent no-op)\n", *targetID, currentMode)
		return nil
	}
	if authType == string(endpoint.AuthNone) || len(authEnc) == 0 {
		return fmt.Errorf("target id=%d has auth_type=%q with no encrypted credential — nothing to migrate", *targetID, authType)
	}

	// ── 2. Decrypt the AES copy in memory ─────────────────────────────────────
	plain, err := encrypter.Decrypt(authEnc)
	if err != nil {
		return fmt.Errorf("decrypting auth_config_enc for target id=%d: %w", *targetID, err)
	}
	var auth endpoint.TargetAuth
	if err := json.Unmarshal(plain, &auth); err != nil {
		return fmt.Errorf("parsing decrypted auth for target id=%d: %w", *targetID, err)
	}

	// ── 3. Resolve / generate the KV secret name ──────────────────────────────
	name := *secretName
	if name == "" {
		u, err := uuid.NewV7()
		if err != nil {
			return fmt.Errorf("generating UUID v7: %w", err)
		}
		name = "gateway-target-" + u.String()
	}
	if !kvSecretNamePattern.MatchString(name) {
		return fmt.Errorf("kv secret name %q is invalid (must match %s)", name, kvSecretNamePattern.String())
	}

	// ── 4. Marshal credential and POST to Key Vault ───────────────────────────
	payload, err := json.Marshal(auth)
	if err != nil {
		return fmt.Errorf("marshalling target auth: %w", err)
	}
	if err := kvClient.Set(ctx, name, string(payload)); err != nil {
		return fmt.Errorf("writing credential to key vault: %w", err)
	}

	// ── 5. Persist mode + secret name (and clear AES copy when mode=kv) ───────
	// Done in a single UPDATE so the row is consistent regardless of when the
	// next request observes it. For mode=both, auth_config_enc keeps its
	// current value (KV-decoded payload would be a cache anyway; existing AES
	// cipher is already the same plaintext).
	if err := updateTargetMode(ctx, dbHandle, *targetID, mode, name); err != nil {
		return err
	}

	fmt.Printf("✓ target id=%d migrated\n", *targetID)
	fmt.Printf("  mode:           %s\n", mode)
	fmt.Printf("  kv_secret_name: %s\n", name)
	if mode == endpoint.CredentialModeKV {
		fmt.Printf("  auth_config_enc: cleared (NULL)\n")
	} else {
		fmt.Printf("  auth_config_enc: preserved as AES cache\n")
	}

	// Boot-style observability: emit a structured log to stdout for ingestion
	// when this CLI runs from automation. event_type matches the resolver's
	// vocabulary so dashboards can filter both paths uniformly.
	slog.Info("target credential migrated",
		"event_type", "target_credential_migrated",
		"target_id", *targetID,
		"mode_before", string(endpoint.CredentialModeAES),
		"mode_after", string(mode),
		"kv_secret_name", name,
	)
	return nil
}

// loadTarget fetches the columns this CLI needs from a single row. Returns
// endpoint.ErrNotFound when the target doesn't exist or is inactive.
func loadTarget(ctx context.Context, dbHandle *sql.DB, targetID int64) (endpoint.CredentialStorageMode, string, []byte, error) {
	const q = `
		SELECT credential_storage_mode, auth_type, auth_config_enc
		FROM gogateway.proxy_targets
		WHERE id = @p1 AND active = 1`

	var (
		mode     string
		authType string
		authEnc  []byte
	)
	row := dbHandle.QueryRowContext(ctx, q, targetID)
	err := row.Scan(&mode, &authType, &authEnc)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", nil, fmt.Errorf("proxy target id=%d not found or inactive", targetID)
	}
	if err != nil {
		return "", "", nil, fmt.Errorf("loading target id=%d: %w", targetID, err)
	}
	return endpoint.CredentialStorageMode(mode), authType, authEnc, nil
}

// updateTargetMode flips credential_storage_mode + kv_secret_name on the row,
// and clears auth_config_enc when mode = kv (no AES cache wanted).
func updateTargetMode(ctx context.Context, dbHandle *sql.DB, targetID int64, mode endpoint.CredentialStorageMode, secretName string) error {
	var q string
	if mode == endpoint.CredentialModeKV {
		q = `
			UPDATE gogateway.proxy_targets
			SET credential_storage_mode = @p1, kv_secret_name = @p2, auth_config_enc = NULL
			WHERE id = @p3`
	} else {
		q = `
			UPDATE gogateway.proxy_targets
			SET credential_storage_mode = @p1, kv_secret_name = @p2
			WHERE id = @p3`
	}

	result, err := dbHandle.ExecContext(ctx, q, string(mode), secretName, targetID)
	if err != nil {
		return fmt.Errorf("updating target id=%d mode: %w", targetID, err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking rows affected for target id=%d: %w", targetID, err)
	}
	if n == 0 {
		return fmt.Errorf("proxy target id=%d disappeared mid-migration (no rows updated)", targetID)
	}
	return nil
}
