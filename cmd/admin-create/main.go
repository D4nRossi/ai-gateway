// Command admin-create inserts the first admin user into the
// gogateway.admin_users table on SQL Server (ADR-0022).
//
// Usage:
//
//	# senha lida interativamente (eco desligado — seguro para histórico do shell)
//	go run ./cmd/admin-create -username daniel -role admin
//
//	# senha via stdin (útil em scripts):
//	echo "minhaSenha" | go run ./cmd/admin-create -username daniel -role admin -stdin
//
//	# DATABASE_URL precisa estar no ambiente — formato sqlserver://user:pass@host?database=DB&encrypt=true
//
// Reasoning: o gateway intencionalmente não popula nenhum admin no migration —
// cada deploy escolhe sua própria credencial inicial. Esta CLI é o caminho
// suportado para criar o primeiro usuário (e usuários subsequentes podem ser
// criados pela UI uma vez que um admin esteja logado).
//
// Segurança:
//   - bcrypt cost=12 (mesma constante do adminservice).
//   - Senha NUNCA aparece em flags por padrão; leitura é via stdin com eco
//     desligado (golang.org/x/term).
//   - Em caso de erro, mensagens NÃO incluem a senha digitada.
//
// References:
//   - ADR-0011 — admin auth via bcrypt
//   - ADR-0022 — troca PG → SQL Server
//   - SPEC.md §15 — bootstrap and first-user provisioning
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	mssql "github.com/microsoft/go-mssqldb"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/term"
)

const (
	bcryptCost = 12

	// errDuplicateKey is the SQL Server error number for PRIMARY KEY / UNIQUE
	// violations (analogous to PG SQLSTATE 23505). Matches mssql.ErrNumberDuplicateKey
	// in internal/infra/mssql/errors.go — copied here so admin-create stays
	// importable without the infra package.
	errDuplicateKey int32 = 2627
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "admin-create: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	username := flag.String("username", "", "admin username (required)")
	role := flag.String("role", "admin", "role: admin | operator | viewer")
	stdin := flag.Bool("stdin", false, "read password from stdin instead of prompting (for scripts)")
	flag.Parse()

	if *username == "" {
		flag.Usage()
		return errors.New("--username is required")
	}
	if !validRole(*role) {
		return fmt.Errorf("invalid role %q — must be admin|operator|viewer", *role)
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return errors.New("DATABASE_URL environment variable is required (e.g. sqlserver://user:pass@host?database=AzureAI_Gateway_hom)")
	}

	password, err := readPassword(*stdin)
	if err != nil {
		return err
	}
	if len(password) < 8 {
		return errors.New("password must have at least 8 characters")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return fmt.Errorf("hashing password: %w", err)
	}

	ctx := context.Background()
	conn, err := sql.Open("sqlserver", dbURL)
	if err != nil {
		return fmt.Errorf("opening sqlserver connection: %w", err)
	}
	defer conn.Close()

	if err := conn.PingContext(ctx); err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}

	const q = `
		INSERT INTO gogateway.admin_users (username, password_hash, role, active)
		OUTPUT INSERTED.id
		VALUES (@p1, @p2, @p3, 1)`

	var id int64
	row := conn.QueryRowContext(ctx, q, *username, string(hash), *role)
	if err := row.Scan(&id); err != nil {
		var mssqlErr mssql.Error
		if errors.As(err, &mssqlErr) && mssqlErr.Number == errDuplicateKey {
			return fmt.Errorf("username %q already exists", *username)
		}
		return fmt.Errorf("inserting admin user: %w", err)
	}

	fmt.Printf("✓ admin user created\n")
	fmt.Printf("  id:       %d\n", id)
	fmt.Printf("  username: %s\n", *username)
	fmt.Printf("  role:     %s\n", *role)
	return nil
}

// readPassword prompts the user without echoing, or reads the first line of
// stdin when -stdin is set (for scripted use).
func readPassword(useStdin bool) (string, error) {
	if useStdin {
		var line string
		if _, err := fmt.Scanln(&line); err != nil {
			return "", fmt.Errorf("reading password from stdin: %w", err)
		}
		return strings.TrimRight(line, "\r\n"), nil
	}

	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", errors.New("stdin is not a terminal — pass -stdin to read from a pipe")
	}

	fmt.Print("Senha: ")
	p1, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return "", fmt.Errorf("reading password: %w", err)
	}
	fmt.Print("Confirmar senha: ")
	p2, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return "", fmt.Errorf("reading confirmation: %w", err)
	}
	if string(p1) != string(p2) {
		return "", errors.New("passwords do not match")
	}
	return string(p1), nil
}

func validRole(r string) bool {
	switch r {
	case "admin", "operator", "viewer":
		return true
	default:
		return false
	}
}
