package mssql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/D4nRossi/ai-gateway/internal/domain/application"
)

// Compile-time assertion: ApplicationRepo must satisfy application.Repository.
var _ application.Repository = (*ApplicationRepo)(nil)

// ApplicationRepo is the SQL Server implementation of application.Repository.
type ApplicationRepo struct {
	db *sql.DB
}

// NewApplicationRepo constructs an ApplicationRepo backed by the given handle.
func NewApplicationRepo(db *sql.DB) *ApplicationRepo {
	return &ApplicationRepo{db: db}
}

// Create inserts a new Application row and returns the entity with ID,
// CreatedAt, and UpdatedAt filled in by the database.
func (r *ApplicationRepo) Create(ctx context.Context, app application.Application) (application.Application, error) {
	allowed, err := marshalStringArray(app.AllowedModels)
	if err != nil {
		return application.Application{}, fmt.Errorf("marshalling allowed_models for app %q: %w", app.Name, err)
	}

	const q = `
		INSERT INTO gogateway.applications
		    (name, tier, allowed_models, streaming_allowed, max_rpm, max_tpm, monthly_budget_brl, active)
		OUTPUT INSERTED.id, INSERTED.created_at, INSERTED.updated_at
		VALUES (@p1, @p2, @p3, @p4, @p5, @p6, @p7, @p8)`

	row := r.db.QueryRowContext(ctx, q,
		app.Name, string(app.Tier), allowed,
		app.StreamingAllowed, app.MaxRPM, app.MaxTPM,
		app.MonthlyBudgetBRL, app.Active,
	)
	if err := row.Scan(&app.ID, &app.CreatedAt, &app.UpdatedAt); err != nil {
		return application.Application{}, fmt.Errorf("inserting application %q: %w", app.Name, err)
	}
	return app, nil
}

// CreateWithKey creates an Application and its initial APIKey in a single
// transaction, ensuring the app is never accessible without a key (ADR-0009).
func (r *ApplicationRepo) CreateWithKey(ctx context.Context, app application.Application, key application.APIKey) (application.Application, application.APIKey, error) {
	allowed, err := marshalStringArray(app.AllowedModels)
	if err != nil {
		return application.Application{}, application.APIKey{}, fmt.Errorf("marshalling allowed_models for app %q: %w", app.Name, err)
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return application.Application{}, application.APIKey{}, fmt.Errorf("beginning create-with-key transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	const qApp = `
		INSERT INTO gogateway.applications
		    (name, tier, allowed_models, streaming_allowed, max_rpm, max_tpm, monthly_budget_brl, active)
		OUTPUT INSERTED.id, INSERTED.created_at, INSERTED.updated_at
		VALUES (@p1, @p2, @p3, @p4, @p5, @p6, @p7, @p8)`

	rowApp := tx.QueryRowContext(ctx, qApp,
		app.Name, string(app.Tier), allowed,
		app.StreamingAllowed, app.MaxRPM, app.MaxTPM,
		app.MonthlyBudgetBRL, app.Active,
	)
	if err = rowApp.Scan(&app.ID, &app.CreatedAt, &app.UpdatedAt); err != nil {
		return application.Application{}, application.APIKey{}, fmt.Errorf("inserting application %q: %w", app.Name, err)
	}

	key.ApplicationID = app.ID
	const qKey = `
		INSERT INTO gogateway.api_keys (application_id, key_prefix, key_hash)
		OUTPUT INSERTED.id, INSERTED.created_at
		VALUES (@p1, @p2, @p3)`

	rowKey := tx.QueryRowContext(ctx, qKey, key.ApplicationID, key.KeyPrefix, key.KeyHash)
	if err = rowKey.Scan(&key.ID, &key.CreatedAt); err != nil {
		return application.Application{}, application.APIKey{}, fmt.Errorf("inserting api key for app %q: %w", app.Name, err)
	}

	if err = tx.Commit(); err != nil {
		return application.Application{}, application.APIKey{}, fmt.Errorf("committing create-with-key: %w", err)
	}
	return app, key, nil
}

// Get retrieves an Application by surrogate ID.
func (r *ApplicationRepo) Get(ctx context.Context, id int64) (application.Application, error) {
	const q = `
		SELECT id, name, tier, allowed_models, streaming_allowed,
		       max_rpm, max_tpm, monthly_budget_brl, active, created_at, updated_at
		FROM gogateway.applications
		WHERE id = @p1`

	row := r.db.QueryRowContext(ctx, q, id)
	app, err := scanApplicationRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return application.Application{}, fmt.Errorf("application id=%d: %w", id, application.ErrNotFound)
		}
		return application.Application{}, fmt.Errorf("getting application id=%d: %w", id, err)
	}
	return app, nil
}

// GetByName retrieves an active Application by its unique name.
func (r *ApplicationRepo) GetByName(ctx context.Context, name string) (application.Application, error) {
	const q = `
		SELECT id, name, tier, allowed_models, streaming_allowed,
		       max_rpm, max_tpm, monthly_budget_brl, active, created_at, updated_at
		FROM gogateway.applications
		WHERE name = @p1 AND active = 1`

	row := r.db.QueryRowContext(ctx, q, name)
	app, err := scanApplicationRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return application.Application{}, fmt.Errorf("application %q: %w", name, application.ErrNotFound)
		}
		return application.Application{}, fmt.Errorf("getting application %q: %w", name, err)
	}
	return app, nil
}

// List returns all Applications ordered by name, including inactive ones.
func (r *ApplicationRepo) List(ctx context.Context) ([]application.Application, error) {
	const q = `
		SELECT id, name, tier, allowed_models, streaming_allowed,
		       max_rpm, max_tpm, monthly_budget_brl, active, created_at, updated_at
		FROM gogateway.applications
		ORDER BY name`

	rows, err := r.db.QueryContext(ctx, q)
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
	allowed, err := marshalStringArray(app.AllowedModels)
	if err != nil {
		return application.Application{}, fmt.Errorf("marshalling allowed_models for app id=%d: %w", app.ID, err)
	}

	const q = `
		UPDATE gogateway.applications
		SET name = @p1, tier = @p2, allowed_models = @p3, streaming_allowed = @p4,
		    max_rpm = @p5, max_tpm = @p6, monthly_budget_brl = @p7, active = @p8,
		    updated_at = SYSUTCDATETIME()
		OUTPUT INSERTED.updated_at
		WHERE id = @p9`

	row := r.db.QueryRowContext(ctx, q,
		app.Name, string(app.Tier), allowed,
		app.StreamingAllowed, app.MaxRPM, app.MaxTPM,
		app.MonthlyBudgetBRL, app.Active, app.ID,
	)
	if err := row.Scan(&app.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return application.Application{}, fmt.Errorf("application id=%d: %w", app.ID, application.ErrNotFound)
		}
		return application.Application{}, fmt.Errorf("updating application id=%d: %w", app.ID, err)
	}
	return app, nil
}

// Delete soft-deletes an Application by setting active=0.
func (r *ApplicationRepo) Delete(ctx context.Context, id int64) error {
	const q = `UPDATE gogateway.applications SET active = 0, updated_at = SYSUTCDATETIME() WHERE id = @p1`

	result, err := r.db.ExecContext(ctx, q, id)
	if err != nil {
		return fmt.Errorf("deleting application id=%d: %w", id, err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking rows affected for app id=%d: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("application id=%d: %w", id, application.ErrNotFound)
	}
	return nil
}

// CreateAPIKey inserts a new APIKey and returns it with ID set.
func (r *ApplicationRepo) CreateAPIKey(ctx context.Context, key application.APIKey) (application.APIKey, error) {
	const q = `
		INSERT INTO gogateway.api_keys (application_id, key_prefix, key_hash)
		OUTPUT INSERTED.id, INSERTED.created_at
		VALUES (@p1, @p2, @p3)`

	row := r.db.QueryRowContext(ctx, q, key.ApplicationID, key.KeyPrefix, key.KeyHash)
	if err := row.Scan(&key.ID, &key.CreatedAt); err != nil {
		return application.APIKey{}, fmt.Errorf("inserting api key for app id=%d: %w", key.ApplicationID, err)
	}
	return key, nil
}

// GetAPIKeyByPrefix retrieves the active (non-rotated) APIKey for the given prefix.
func (r *ApplicationRepo) GetAPIKeyByPrefix(ctx context.Context, prefix string) (application.APIKey, error) {
	const q = `
		SELECT id, application_id, key_prefix, key_hash, created_at, rotated_at
		FROM gogateway.api_keys
		WHERE key_prefix = @p1 AND rotated_at IS NULL`

	row := r.db.QueryRowContext(ctx, q, prefix)
	key, err := scanAPIKeyRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return application.APIKey{}, fmt.Errorf("api key prefix %q: %w", prefix, application.ErrNotFound)
		}
		return application.APIKey{}, fmt.Errorf("getting api key for prefix %q: %w", prefix, err)
	}
	return key, nil
}

// RotateAPIKey atomically marks the current key as rotated and inserts newKey.
// Zero-downtime: the new key is active before the old is invalidated, within
// one transaction.
func (r *ApplicationRepo) RotateAPIKey(ctx context.Context, applicationID int64, newKey application.APIKey) (application.APIKey, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return application.APIKey{}, fmt.Errorf("beginning rotation transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	now := time.Now().UTC()

	const markOld = `
		UPDATE gogateway.api_keys SET rotated_at = @p1
		WHERE application_id = @p2 AND rotated_at IS NULL`
	if _, err = tx.ExecContext(ctx, markOld, now, applicationID); err != nil {
		return application.APIKey{}, fmt.Errorf("marking old api key as rotated: %w", err)
	}

	const insertNew = `
		INSERT INTO gogateway.api_keys (application_id, key_prefix, key_hash)
		OUTPUT INSERTED.id, INSERTED.created_at
		VALUES (@p1, @p2, @p3)`
	newKey.ApplicationID = applicationID
	row := tx.QueryRowContext(ctx, insertNew, applicationID, newKey.KeyPrefix, newKey.KeyHash)
	if err = row.Scan(&newKey.ID, &newKey.CreatedAt); err != nil {
		return application.APIKey{}, fmt.Errorf("inserting rotated api key: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return application.APIKey{}, fmt.Errorf("committing key rotation: %w", err)
	}
	return newKey, nil
}

// ── scan helpers ─────────────────────────────────────────────────────────────

func scanApplication(s rowScanner) (application.Application, error) {
	var app application.Application
	var tier string
	var allowedRaw string
	err := s.Scan(
		&app.ID, &app.Name, &tier, &allowedRaw,
		&app.StreamingAllowed, &app.MaxRPM, &app.MaxTPM,
		&app.MonthlyBudgetBRL, &app.Active, &app.CreatedAt, &app.UpdatedAt,
	)
	if err != nil {
		return application.Application{}, err
	}
	app.Tier = application.TierLevel(tier)
	app.AllowedModels, err = unmarshalStringArray(allowedRaw)
	if err != nil {
		return application.Application{}, fmt.Errorf("unmarshalling allowed_models for app id=%d: %w", app.ID, err)
	}
	return app, nil
}

func scanApplicationRow(row *sql.Row) (application.Application, error) {
	return scanApplication(row)
}

func scanApplicationFromRows(rows *sql.Rows) (application.Application, error) {
	return scanApplication(rows)
}

func scanAPIKey(s rowScanner) (application.APIKey, error) {
	var key application.APIKey
	var rotatedAt sql.NullTime
	err := s.Scan(
		&key.ID, &key.ApplicationID, &key.KeyPrefix,
		&key.KeyHash, &key.CreatedAt, &rotatedAt,
	)
	if err != nil {
		return application.APIKey{}, err
	}
	if rotatedAt.Valid {
		t := rotatedAt.Time
		key.RotatedAt = &t
	}
	return key, nil
}

func scanAPIKeyRow(row *sql.Row) (application.APIKey, error) { return scanAPIKey(row) }
