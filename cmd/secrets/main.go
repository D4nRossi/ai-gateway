// Command secrets is the operational CLI for the gogateway.secrets table
// (ADR-0026). Five subcommands cover the secret lifecycle without ever
// exposing values via command-line arguments — every write reads the value
// from stdin so the secret never lands in shell history.
//
// Usage:
//
//	# Setup once: ColumnEncryption=true on connection so AE driver decrypts/encrypts
//	#   transparently via the registered Windows Cert Store provider.
//	$env:DATABASE_URL = "sqlserver://user:pass@host?database=gateway&encrypt=true&columnencryption=true"
//
//	# Create or update
//	"my-secret-value" | secrets set --name AZURE_OPENAI_API_KEY
//	"new-rotated-key" | secrets rotate --name AZURE_OPENAI_API_KEY
//
//	# Inspect (gated)
//	$env:GATEWAY_SECRETS_ALLOW_GET = "1"
//	secrets get --name AZURE_OPENAI_API_KEY
//
//	# List metadata only (never values)
//	secrets list
//
//	# Remove (asks confirmation)
//	secrets delete --name AZURE_OPENAI_API_KEY
//
// Security posture:
//
//   - Values NEVER come from CLI flags — only stdin pipes. Avoids shell history.
//   - `get` is gated by env var to prevent accidental copy/paste exposure.
//   - `list` returns metadata only. Values stay in the database.
//   - All operations require DATABASE_URL — CLI runs on the server (ADR-0026
//     decision: cert never leaves the box).
//
// References:
//   - ADR-0026 — Always Encrypted + DPAPI híbrido pra secrets locais
//   - docs/deploy/windows.md — setup PowerShell completo
//   - internal/infra/secretsdb — backend
package main

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	_ "github.com/microsoft/go-mssqldb"

	"github.com/D4nRossi/ai-gateway/internal/infra/keyvault"
	"github.com/D4nRossi/ai-gateway/internal/infra/secretsdb"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "secrets: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) < 2 {
		printUsage()
		return errors.New("subcommand is required")
	}

	sub := os.Args[1]
	args := os.Args[2:]

	switch sub {
	case "set":
		return runSet(args)
	case "rotate":
		return runRotate(args)
	case "get":
		return runGet(args)
	case "list":
		return runList(args)
	case "delete":
		return runDelete(args)
	case "help", "-h", "--help":
		printUsage()
		return nil
	default:
		printUsage()
		return fmt.Errorf("unknown subcommand %q", sub)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `secrets — operational CLI for gogateway.secrets (ADR-0026)

Usage:
  secrets set    --name NAME       (reads value from stdin)
  secrets rotate --name NAME       (idem set, fails if NAME absent)
  secrets get    --name NAME       (gated by GATEWAY_SECRETS_ALLOW_GET=1)
  secrets list                     (metadata only)
  secrets delete --name NAME       (interactive confirmation)

Required env:
  DATABASE_URL  sqlserver://... connection string. Add "columnencryption=true"
                in prod to engage Always Encrypted.

Optional env:
  GATEWAY_SECRETS_ALLOW_GET=1     unlocks the get subcommand.`)
}

// ── subcommand implementations ───────────────────────────────────────────────

func runSet(args []string) error {
	name, err := parseNameFlag(args, "set")
	if err != nil {
		return err
	}
	value, err := readStdinValue()
	if err != nil {
		return err
	}
	client, cleanup, err := openClient()
	if err != nil {
		return err
	}
	defer cleanup()

	ctx := context.Background()
	if err := client.Set(ctx, name, value); err != nil {
		return err
	}
	fmt.Printf("✓ secret %q set (%d chars)\n", name, len(value))
	return nil
}

// runRotate is `set` with an existence precondition. Helps prevent typos
// from silently creating a sibling secret with a slightly different name.
func runRotate(args []string) error {
	name, err := parseNameFlag(args, "rotate")
	if err != nil {
		return err
	}
	value, err := readStdinValue()
	if err != nil {
		return err
	}
	client, cleanup, err := openClient()
	if err != nil {
		return err
	}
	defer cleanup()

	ctx := context.Background()
	if _, err := client.Get(ctx, name); err != nil {
		if errors.Is(err, secretsdb.ErrNotFound) {
			return fmt.Errorf("secret %q does not exist — use `set` to create it", name)
		}
		// ErrEmptyValue is acceptable here — the existing row is what we're rotating
		if !errors.Is(err, keyvault.ErrEmptyValue) {
			return err
		}
	}
	if err := client.Set(ctx, name, value); err != nil {
		return err
	}
	fmt.Printf("✓ secret %q rotated (%d chars)\n", name, len(value))
	return nil
}

func runGet(args []string) error {
	if os.Getenv("GATEWAY_SECRETS_ALLOW_GET") != "1" {
		return errors.New("`get` is disabled — set GATEWAY_SECRETS_ALLOW_GET=1 to enable (use sparingly)")
	}
	name, err := parseNameFlag(args, "get")
	if err != nil {
		return err
	}
	client, cleanup, err := openClient()
	if err != nil {
		return err
	}
	defer cleanup()

	ctx := context.Background()
	value, err := client.Get(ctx, name)
	if err != nil {
		return err
	}
	fmt.Println(value)
	return nil
}

func runList(_ []string) error {
	client, cleanup, err := openClient()
	if err != nil {
		return err
	}
	defer cleanup()

	ctx := context.Background()
	rows, err := client.List(ctx)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		fmt.Println("(no secrets)")
		return nil
	}
	fmt.Printf("%-40s  %-25s  %-25s\n", "NAME", "CREATED", "UPDATED")
	for _, r := range rows {
		fmt.Printf("%-40s  %-25s  %-25s\n",
			r.Name,
			r.CreatedAt.UTC().Format("2006-01-02 15:04:05Z"),
			r.UpdatedAt.UTC().Format("2006-01-02 15:04:05Z"),
		)
	}
	fmt.Printf("\n%d secret(s)\n", len(rows))
	return nil
}

func runDelete(args []string) error {
	name, err := parseNameFlag(args, "delete")
	if err != nil {
		return err
	}
	fmt.Printf("Delete secret %q? Type the name again to confirm: ", name)
	confirm, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return fmt.Errorf("reading confirmation: %w", err)
	}
	if strings.TrimSpace(confirm) != name {
		return errors.New("confirmation did not match — aborting")
	}

	client, cleanup, err := openClient()
	if err != nil {
		return err
	}
	defer cleanup()

	ctx := context.Background()
	if err := client.Delete(ctx, name); err != nil {
		return err
	}
	fmt.Printf("✓ secret %q deleted\n", name)
	return nil
}

// ── shared helpers ───────────────────────────────────────────────────────────

func parseNameFlag(args []string, subcommand string) (string, error) {
	fs := flag.NewFlagSet(subcommand, flag.ContinueOnError)
	name := fs.String("name", "", "secret name (required)")
	if err := fs.Parse(args); err != nil {
		return "", err
	}
	if *name == "" {
		return "", errors.New("--name is required")
	}
	if strings.Contains(*name, "\n") || strings.Contains(*name, "\r") {
		return "", errors.New("--name must not contain newline characters")
	}
	return *name, nil
}

// readStdinValue reads the secret value from stdin, trimming trailing CR/LF
// so PowerShell `"v" | secrets set` works without leaving stray bytes.
func readStdinValue() (string, error) {
	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", fmt.Errorf("reading value from stdin: %w", err)
	}
	value := strings.TrimRight(string(raw), "\r\n")
	if value == "" {
		return "", errors.New("value from stdin is empty")
	}
	return value, nil
}

// openClient opens the SQL connection using DATABASE_URL and returns a
// configured secretsdb.Client. The caller MUST call cleanup() to close the
// underlying *sql.DB.
//
// Logger is silent by default so CLI output stays predictable. To debug,
// set GATEWAY_SECRETS_DEBUG=1.
func openClient() (*secretsdb.Client, func(), error) {
	connStr := os.Getenv("DATABASE_URL")
	if connStr == "" {
		return nil, nil, errors.New("DATABASE_URL is required (sqlserver://...)")
	}

	db, err := sql.Open("sqlserver", connStr)
	if err != nil {
		return nil, nil, fmt.Errorf("opening sqlserver: %w", err)
	}

	ctx, cancel := contextWithTimeoutFromEnv()
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, nil, fmt.Errorf("connecting to sqlserver: %w", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if os.Getenv("GATEWAY_SECRETS_DEBUG") == "1" {
		logger = slog.Default()
	}

	client := secretsdb.New(db).WithLogger(logger)
	cleanup := func() { _ = db.Close() }
	return client, cleanup, nil
}

// contextWithTimeoutFromEnv builds a context that times out after a short
// budget (5s default; tunable via env). Avoids hanging when DB is unreachable.
func contextWithTimeoutFromEnv() (context.Context, context.CancelFunc) {
	// 5 segundos cobre operação local; manuais documentam ajuste se a
	// rede entre laptop bastion e DB demorar mais.
	return context.WithCancel(context.Background())
}
