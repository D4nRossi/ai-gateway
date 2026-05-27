// Package mssql provides microsoft/go-mssqldb implementations of the domain
// repository interfaces (ADR-0022). Each repository wraps a *sql.DB handle
// and executes parameterized T-SQL queries qualified with the gogateway schema.
// No business logic lives here — only persistence.
//
// Conventions enforced across this package:
//   - All tables referenced as gogateway.NomeTabela (never bare names)
//   - Parameter binding via @p1, @p2, ... (positional named)
//   - errors.Is(err, sql.ErrNoRows) for "row not found" detection
//   - INSERTs that need the new row's ID use OUTPUT INSERTED.id
//   - Idempotent INSERTs use "IF NOT EXISTS (SELECT 1 ...) INSERT ..."
//   - UPSERTs use MERGE
//
// Domain-specific sentinel errors (ErrNotFound) are defined in their
// respective domain packages so the service layer can check for them without
// importing infra (ADR-0015).
//
// References:
//   - ADR-0009 — DB-backed admin plane
//   - ADR-0015 — infra layer implements domain interfaces
//   - ADR-0022 — troca PostgreSQL → SQL Server
//   - CLAUDE.md §9 — convenções T-SQL
//   - https://pkg.go.dev/github.com/microsoft/go-mssqldb
package mssql

import (
	"encoding/json"
	"errors"
	"strings"

	mssql "github.com/microsoft/go-mssqldb"
)

// ── JSON array helpers (TEXT[] replacement) ──────────────────────────────────
//
// PostgreSQL had TEXT[] arrays for fields like applications.allowed_models.
// SQL Server has no native array type — we store JSON arrays in NVARCHAR(MAX)
// columns (CHECK ISJSON(...) enforces validity at the DB layer).

// marshalStringArray serializes a []string to a JSON array. Returns "[]" for
// nil or empty slices so the destination NVARCHAR column never sees null or
// invalid JSON (the ISJSON CHECK constraint would reject "").
func marshalStringArray(vs []string) (string, error) {
	if len(vs) == 0 {
		return "[]", nil
	}
	b, err := json.Marshal(vs)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// unmarshalStringArray parses a JSON array string (or NULL) into []string.
// Empty input or "null" yields a non-nil empty slice so callers can range
// without nil checks.
func unmarshalStringArray(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" {
		return []string{}, nil
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = []string{}
	}
	return out, nil
}

// ── Error inspection ─────────────────────────────────────────────────────────

// MSSQLErrorNumber returns the SQL Server error number when err wraps an
// mssql.Error, or 0 otherwise. Used by the admin handlers' pgerrors translation
// layer (mantém nome do arquivo apesar do swap PG → SQL Server pra reduzir
// blast radius — translatePgError continua sendo o único ponto de tradução).
//
// References:
//   - https://learn.microsoft.com/en-us/sql/relational-databases/errors-events/database-engine-events-and-errors
func MSSQLErrorNumber(err error) int32 {
	var mssqlErr mssql.Error
	if !errors.As(err, &mssqlErr) {
		return 0
	}
	return mssqlErr.Number
}

// MSSQLErrorMessage returns the human-readable message from a wrapped
// mssql.Error, or empty when err does not wrap one. Used in admin handlers
// to expose constraint names in details for operator triage.
func MSSQLErrorMessage(err error) string {
	var mssqlErr mssql.Error
	if !errors.As(err, &mssqlErr) {
		return ""
	}
	return mssqlErr.Message
}

// Common SQL Server error numbers consumed by the admin handlers.
const (
	// ErrNumberDuplicateKey is raised for PRIMARY KEY / UNIQUE constraint
	// violations on insert (analogous to PG SQLSTATE 23505).
	ErrNumberDuplicateKey int32 = 2627

	// ErrNumberUniqueIndexViolation is raised for filtered or non-clustered
	// UNIQUE index violations (e.g. idx_api_keys_active_prefix).
	ErrNumberUniqueIndexViolation int32 = 2601

	// ErrNumberConstraintViolation covers CHECK and FOREIGN KEY violations.
	// SQL Server uses the same generic number for both — the wrapped message
	// names the specific constraint.
	ErrNumberConstraintViolation int32 = 547
)

// IsDuplicateKey reports whether err is a SQL Server unique violation
// (2627 primary key or 2601 unique index).
func IsDuplicateKey(err error) bool {
	n := MSSQLErrorNumber(err)
	return n == ErrNumberDuplicateKey || n == ErrNumberUniqueIndexViolation
}

// IsConstraintViolation reports whether err is a CHECK or FK violation (547).
func IsConstraintViolation(err error) bool {
	return MSSQLErrorNumber(err) == ErrNumberConstraintViolation
}
