package db

import (
	"errors"
	"fmt"

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
