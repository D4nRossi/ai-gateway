package db

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"

	"github.com/golang-migrate/migrate/v4"
	// Registers the "sqlserver" database scheme with golang-migrate.
	_ "github.com/golang-migrate/migrate/v4/database/sqlserver"
	// Registers the "file" source scheme with golang-migrate.
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

// RunMigrations applies all pending UP migrations from the migrations/
// directory. It is idempotent: migrate.ErrNoChange is treated as success.
//
// connStr must be a sqlserver://... URL acceptable to both the runtime driver
// and golang-migrate's sqlserver driver (they share the same scheme and
// underlying parser). Build it via db.ConnString(cfg).
//
// The schema_migrations bookkeeping table is created by golang-migrate in the
// user's default schema (typically dbo on SQL Server). The gateway's own
// tables live in the `gogateway` schema (created by migration 001). The
// operator running this needs CREATE TABLE permission in both schemas — see
// ADR-0022 §Mitigations.
//
// References:
//   - SPEC.md §16 step 5
//   - ADR-0022 — golang-migrate driver sqlserver
//   - ADR-0025 — auto-apply vs manual migration policy
//   - https://github.com/golang-migrate/migrate/tree/master/database/sqlserver
//   - https://github.com/golang-migrate/migrate/blob/master/MIGRATIONS.md
func RunMigrations(connStr, migrationsPath string) error {
	sourceURL := "file://" + migrationsPath

	m, err := migrate.New(sourceURL, connStr)
	if err != nil {
		return fmt.Errorf("creating migrate instance: %w", err)
	}
	defer func() {
		// Close drains the resources held by the migrate instance. Errors
		// here are not actionable (the upstream driver may have already
		// released the connection); we ignore them deliberately.
		_, _ = m.Close()
	}()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("running migrations: %w", err)
	}

	return nil
}

// upMigrationFilename matches files like "011_proxy_targets_kv_credential_mode.up.sql"
// and captures the numeric prefix as group 1.
var upMigrationFilename = regexp.MustCompile(`^(\d+)_.+\.up\.sql$`)

// LatestExpectedVersion scans migrationsPath for *.up.sql files and returns
// the highest numeric prefix found. Used by AssertSchemaUpToDate to check
// what the deployed binary expects vs what the database currently has.
//
// Returns 0 when the directory has no .up.sql files (fresh repo without
// migrations — invalid state for production but handled gracefully).
func LatestExpectedVersion(migrationsPath string) (uint, error) {
	entries, err := os.ReadDir(migrationsPath)
	if err != nil {
		return 0, fmt.Errorf("reading migrations directory %q: %w", migrationsPath, err)
	}

	var latest uint
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		matches := upMigrationFilename.FindStringSubmatch(e.Name())
		if matches == nil {
			continue
		}
		v, err := strconv.ParseUint(matches[1], 10, 32)
		if err != nil {
			// Files matching the regex with non-numeric prefix shouldn't
			// happen — the regex requires digits — but defensive guard.
			continue
		}
		if uint(v) > latest {
			latest = uint(v)
		}
	}
	return latest, nil
}

// ErrSchemaOutOfDate is returned by AssertSchemaUpToDate when the binary
// expects a higher migration version than what's recorded in the database.
// Operator must apply pending migrations manually (e.g. `migrate -database
// "..." -path migrations up`) before this gateway can start.
var ErrSchemaOutOfDate = errors.New("schema is out of date")

// ErrSchemaAhead is returned when the database is at a higher migration
// version than what the binary knows. Typically means a rollback to an older
// binary — operator must align the two ends (downgrade DB or upgrade binary)
// before the gateway can start safely.
var ErrSchemaAhead = errors.New("schema is ahead of the running binary")

// ErrSchemaDirty is returned when schema_migrations.dirty = 1, indicating a
// previous migration failed mid-flight. Operator must inspect the partial
// state, clean it up, and reset dirty=0 manually before retrying.
var ErrSchemaDirty = errors.New("schema_migrations.dirty = 1; previous migration failed")

// AssertSchemaUpToDate is the no-op counterpart of RunMigrations: it inspects
// the current schema_migrations row, compares with the highest version found
// in migrationsPath, and returns nil only when they match exactly and the
// dirty flag is clear.
//
// Intended for production deployments where the DBA owns the migration
// window (ADR-0025). Operator runs `migrate up` ahead of restarting the
// gateway; the gateway boots only after the schema is provably in sync.
//
// Returns:
//   - nil               — versions match, dirty=0
//   - ErrSchemaOutOfDate — DB version < expected (operator needs to run migrate up)
//   - ErrSchemaAhead    — DB version > expected (operator downgraded the binary)
//   - ErrSchemaDirty    — partial migration in flight, manual cleanup required
//
// References:
//   - ADR-0025 — auto-apply vs manual migration policy
func AssertSchemaUpToDate(connStr, migrationsPath string) error {
	expected, err := LatestExpectedVersion(migrationsPath)
	if err != nil {
		return fmt.Errorf("scanning migrations directory: %w", err)
	}
	if expected == 0 {
		// No migrations on disk — nothing to assert. Unusual but not an error.
		return nil
	}

	sourceURL := "file://" + migrationsPath
	m, err := migrate.New(sourceURL, connStr)
	if err != nil {
		return fmt.Errorf("creating migrate instance: %w", err)
	}
	defer func() { _, _ = m.Close() }()

	current, dirty, err := m.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		// Nothing applied yet — DB needs the full set.
		return fmt.Errorf(
			"%w: expected version %d, database has no migrations applied",
			ErrSchemaOutOfDate, expected,
		)
	}
	if err != nil {
		return fmt.Errorf("reading schema_migrations: %w", err)
	}
	if dirty {
		return fmt.Errorf(
			"%w: version %d is marked dirty; resolve the partial state before retrying",
			ErrSchemaDirty, current,
		)
	}
	if current < expected {
		return fmt.Errorf(
			"%w: database at version %d, binary expects %d (apply pending migrations: `migrate -database \"...\" -path migrations up`)",
			ErrSchemaOutOfDate, current, expected,
		)
	}
	if current > expected {
		return fmt.Errorf(
			"%w: database at version %d, binary expects %d (rollback detected — align binary with DB)",
			ErrSchemaAhead, current, expected,
		)
	}
	return nil
}
