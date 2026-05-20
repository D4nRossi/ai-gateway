// Package postgres provides pgx v5 implementations of the domain repository interfaces.
// Each repository wraps a *pgxpool.Pool and executes parameterized SQL queries.
//
// All methods accept a context.Context as the first argument and propagate cancellation
// to the database driver. No business logic lives here — only persistence.
//
// References:
//   - ADR-0004 — pgx direct (no ORM)
//   - ADR-0009 — DB-backed admin plane
//   - ADR-0015 — infra layer implements domain interfaces
//   - https://pkg.go.dev/github.com/jackc/pgx/v5/pgxpool
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/D4nRossi/ai-gateway/internal/domain/application"
)

// ApplicationRepo is the pgx implementation of application.Repository.
type ApplicationRepo struct {
	pool *pgxpool.Pool
}

// NewApplicationRepo constructs an ApplicationRepo backed by the given pool.
func NewApplicationRepo(pool *pgxpool.Pool) *ApplicationRepo {
	return &ApplicationRepo{pool: pool}
}

// Create inserts a new Application row and returns the persisted entity with
// ID, CreatedAt, and UpdatedAt filled in by the database.
//
// References:
//   - ADR-0009 — application lifecycle
func (r *ApplicationRepo) Create(ctx context.Context, app application.Application) (application.Application, error) {
	const q = `
		INSERT INTO applications
		    (name, tier, allowed_models, streaming_allowed, max_rpm, max_tpm, monthly_budget_brl, active)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, created_at, updated_at`

	row := r.pool.QueryRow(ctx, q,
		app.Name, string(app.Tier), app.AllowedModels,
		app.StreamingAllowed, app.MaxRPM, app.MaxTPM,
		app.MonthlyBudgetBRL, app.Active,
	)
	if err := row.Scan(&app.ID, &app.CreatedAt, &app.UpdatedAt); err != nil {
		return application.Application{}, fmt.Errorf("inserting application %q: %w", app.Name, err)
	}
	return app, nil
}

// Get retrieves an Application by surrogate ID.
func (r *ApplicationRepo) Get(ctx context.Context, id int64) (application.Application, error) {
	const q = `
		SELECT id, name, tier, allowed_models, streaming_allowed,
		       max_rpm, max_tpm, monthly_budget_brl, active, created_at, updated_at
		FROM applications
		WHERE id = $1`

	row := r.pool.QueryRow(ctx, q, id)
	app, err := scanApplication(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return application.Application{}, fmt.Errorf("application id=%d not found: %w", id, ErrNotFound)
		}
		return application.Application{}, fmt.Errorf("getting application id=%d: %w", id, err)
	}
	return app, nil
}

// GetByName retrieves an Application by its unique name. Only active rows are returned.
func (r *ApplicationRepo) GetByName(ctx context.Context, name string) (application.Application, error) {
	const q = `
		SELECT id, name, tier, allowed_models, streaming_allowed,
		       max_rpm, max_tpm, monthly_budget_brl, active, created_at, updated_at
		FROM applications
		WHERE name = $1 AND active = true`

	row := r.pool.QueryRow(ctx, q, name)
	app, err := scanApplication(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return application.Application{}, fmt.Errorf("application %q not found: %w", name, ErrNotFound)
		}
		return application.Application{}, fmt.Errorf("getting application %q: %w", name, err)
	}
	return app, nil
}

// List returns all Applications ordered by name. Includes both active and inactive.
func (r *ApplicationRepo) List(ctx context.Context) ([]application.Application, error) {
	const q = `
		SELECT id, name, tier, allowed_models, streaming_allowed,
		       max_rpm, max_tpm, monthly_budget_brl, active, created_at, updated_at
		FROM applications
		ORDER BY name`

	rows, err := r.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("listing applications: %w", err)
	}
	defer rows.Close()

	var apps []application.Application
	for rows.Next() {
		app, err := scanApplicationFromRows(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning application row: %w", err)
		}
		apps = append(apps, app)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating application rows: %w", err)
	}
	return apps, nil
}

// Update persists changes to an existing Application. ID must be set.
func (r *ApplicationRepo) Update(ctx context.Context, app application.Application) (application.Application, error) {
	const q = `
		UPDATE applications
		SET name = $1, tier = $2, allowed_models = $3, streaming_allowed = $4,
		    max_rpm = $5, max_tpm = $6, monthly_budget_brl = $7, active = $8,
		    updated_at = NOW()
		WHERE id = $9
		RETURNING updated_at`

	row := r.pool.QueryRow(ctx, q,
		app.Name, string(app.Tier), app.AllowedModels,
		app.StreamingAllowed, app.MaxRPM, app.MaxTPM,
		app.MonthlyBudgetBRL, app.Active, app.ID,
	)
	if err := row.Scan(&app.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return application.Application{}, fmt.Errorf("application id=%d not found: %w", app.ID, ErrNotFound)
		}
		return application.Application{}, fmt.Errorf("updating application id=%d: %w", app.ID, err)
	}
	return app, nil
}

// Delete soft-deletes an Application by setting active=false.
func (r *ApplicationRepo) Delete(ctx context.Context, id int64) error {
	const q = `UPDATE applications SET active = false, updated_at = NOW() WHERE id = $1`

	tag, err := r.pool.Exec(ctx, q, id)
	if err != nil {
		return fmt.Errorf("deleting application id=%d: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("application id=%d not found: %w", id, ErrNotFound)
	}
	return nil
}

// CreateAPIKey inserts a new APIKey and returns it with ID set.
//
// Reasoning: any previous key for the same application_id must be handled by RotateAPIKey
// (which sets rotated_at on the old key in the same transaction). This method does not
// enforce uniqueness by itself — the DB UNIQUE constraint on application_id does.
func (r *ApplicationRepo) CreateAPIKey(ctx context.Context, key application.APIKey) (application.APIKey, error) {
	const q = `
		INSERT INTO api_keys (application_id, key_prefix, key_hash)
		VALUES ($1, $2, $3)
		RETURNING id, created_at`

	row := r.pool.QueryRow(ctx, q, key.ApplicationID, key.KeyPrefix, key.KeyHash)
	if err := row.Scan(&key.ID, &key.CreatedAt); err != nil {
		return application.APIKey{}, fmt.Errorf("inserting api key for app id=%d: %w", key.ApplicationID, err)
	}
	return key, nil
}

// GetAPIKeyByPrefix retrieves the APIKey for the given prefix.
// Used by the auth middleware; only returns keys where rotated_at IS NULL (active key).
func (r *ApplicationRepo) GetAPIKeyByPrefix(ctx context.Context, prefix string) (application.APIKey, error) {
	const q = `
		SELECT id, application_id, key_prefix, key_hash, created_at, rotated_at
		FROM api_keys
		WHERE key_prefix = $1 AND rotated_at IS NULL`

	row := r.pool.QueryRow(ctx, q, prefix)
	key, err := scanAPIKey(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return application.APIKey{}, fmt.Errorf("api key prefix %q not found: %w", prefix, ErrNotFound)
		}
		return application.APIKey{}, fmt.Errorf("getting api key for prefix %q: %w", prefix, err)
	}
	return key, nil
}

// RotateAPIKey replaces the active key for an application in a single transaction:
// sets rotated_at on the existing key and inserts newKey.
//
// Reasoning: atomic swap ensures zero-downtime key rotation — there is no window
// where the application has no valid key (ADR-0009 response C).
func (r *ApplicationRepo) RotateAPIKey(ctx context.Context, applicationID int64, newKey application.APIKey) (application.APIKey, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return application.APIKey{}, fmt.Errorf("beginning rotation transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	now := time.Now().UTC()

	const markOld = `
		UPDATE api_keys SET rotated_at = $1
		WHERE application_id = $2 AND rotated_at IS NULL`
	if _, err = tx.Exec(ctx, markOld, now, applicationID); err != nil {
		return application.APIKey{}, fmt.Errorf("marking old api key as rotated: %w", err)
	}

	const insertNew = `
		INSERT INTO api_keys (application_id, key_prefix, key_hash)
		VALUES ($1, $2, $3)
		RETURNING id, created_at`
	newKey.ApplicationID = applicationID
	row := tx.QueryRow(ctx, insertNew, applicationID, newKey.KeyPrefix, newKey.KeyHash)
	if err = row.Scan(&newKey.ID, &newKey.CreatedAt); err != nil {
		return application.APIKey{}, fmt.Errorf("inserting rotated api key: %w", err)
	}

	if err = tx.Commit(ctx); err != nil {
		return application.APIKey{}, fmt.Errorf("committing key rotation: %w", err)
	}
	return newKey, nil
}

// ── scan helpers ─────────────────────────────────────────────────────────────

func scanApplication(row pgx.Row) (application.Application, error) {
	var app application.Application
	var tier string
	err := row.Scan(
		&app.ID, &app.Name, &tier, &app.AllowedModels,
		&app.StreamingAllowed, &app.MaxRPM, &app.MaxTPM,
		&app.MonthlyBudgetBRL, &app.Active, &app.CreatedAt, &app.UpdatedAt,
	)
	if err != nil {
		return application.Application{}, err
	}
	app.Tier = application.TierLevel(tier)
	return app, nil
}

func scanApplicationFromRows(rows pgx.Rows) (application.Application, error) {
	var app application.Application
	var tier string
	err := rows.Scan(
		&app.ID, &app.Name, &tier, &app.AllowedModels,
		&app.StreamingAllowed, &app.MaxRPM, &app.MaxTPM,
		&app.MonthlyBudgetBRL, &app.Active, &app.CreatedAt, &app.UpdatedAt,
	)
	if err != nil {
		return application.Application{}, err
	}
	app.Tier = application.TierLevel(tier)
	return app, nil
}

func scanAPIKey(row pgx.Row) (application.APIKey, error) {
	var key application.APIKey
	err := row.Scan(
		&key.ID, &key.ApplicationID, &key.KeyPrefix,
		&key.KeyHash, &key.CreatedAt, &key.RotatedAt,
	)
	return key, err
}
