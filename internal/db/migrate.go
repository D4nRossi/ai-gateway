package db

import (
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres" // registers postgres:// scheme
	_ "github.com/golang-migrate/migrate/v4/source/file"       // registers file:// scheme
)

// RunMigrations applies all pending UP migrations from the migrations/ directory.
// It is idempotent: migrate.ErrNoChange is treated as success.
//
// Reasoning: running migrations at every boot guarantees the schema is always
// up-to-date without a separate migration step in CI or deployment scripts.
//
// References:
//   - SPEC.md §16 step 5
//   - https://github.com/golang-migrate/migrate/blob/master/MIGRATIONS.md
func RunMigrations(databaseURL, migrationsPath string) error {
	sourceURL := "file://" + migrationsPath

	m, err := migrate.New(sourceURL, databaseURL)
	if err != nil {
		return fmt.Errorf("creating migrate instance: %w", err)
	}
	defer func() {
		_, _ = m.Close() // best-effort cleanup; errors here are not actionable
	}()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("running migrations: %w", err)
	}

	return nil
}
