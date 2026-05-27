package mssql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/D4nRossi/ai-gateway/internal/domain/admin"
)

// Compile-time assertion: AdminRepo must satisfy admin.Repository.
var _ admin.Repository = (*AdminRepo)(nil)

// AdminRepo is the SQL Server implementation of admin.Repository.
type AdminRepo struct {
	db *sql.DB
}

// NewAdminRepo constructs an AdminRepo backed by the given *sql.DB handle.
func NewAdminRepo(db *sql.DB) *AdminRepo {
	return &AdminRepo{db: db}
}

// CreateUser inserts a new AdminUser and returns it with ID and timestamps set.
func (r *AdminRepo) CreateUser(ctx context.Context, user admin.AdminUser) (admin.AdminUser, error) {
	const q = `
		INSERT INTO gogateway.admin_users (username, password_hash, role, active)
		OUTPUT INSERTED.id, INSERTED.created_at, INSERTED.updated_at
		VALUES (@p1, @p2, @p3, @p4)`

	row := r.db.QueryRowContext(ctx, q,
		user.Username, user.PasswordHash, string(user.Role), user.Active,
	)
	if err := row.Scan(&user.ID, &user.CreatedAt, &user.UpdatedAt); err != nil {
		return admin.AdminUser{}, fmt.Errorf("inserting admin user %q: %w", user.Username, err)
	}
	return user, nil
}

// GetUser retrieves an AdminUser by surrogate ID.
func (r *AdminRepo) GetUser(ctx context.Context, id int64) (admin.AdminUser, error) {
	const q = `
		SELECT id, username, password_hash, role, active, created_at, updated_at
		FROM gogateway.admin_users WHERE id = @p1`

	row := r.db.QueryRowContext(ctx, q, id)
	user, err := scanAdminUserRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return admin.AdminUser{}, fmt.Errorf("admin user id=%d: %w", id, admin.ErrNotFound)
		}
		return admin.AdminUser{}, fmt.Errorf("getting admin user id=%d: %w", id, err)
	}
	return user, nil
}

// GetUserByUsername retrieves an active AdminUser by username.
func (r *AdminRepo) GetUserByUsername(ctx context.Context, username string) (admin.AdminUser, error) {
	const q = `
		SELECT id, username, password_hash, role, active, created_at, updated_at
		FROM gogateway.admin_users WHERE username = @p1 AND active = 1`

	row := r.db.QueryRowContext(ctx, q, username)
	user, err := scanAdminUserRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return admin.AdminUser{}, fmt.Errorf("admin user %q: %w", username, admin.ErrNotFound)
		}
		return admin.AdminUser{}, fmt.Errorf("getting admin user %q: %w", username, err)
	}
	return user, nil
}

// UpdateUser persists changes to an existing AdminUser. ID must be set.
func (r *AdminRepo) UpdateUser(ctx context.Context, user admin.AdminUser) (admin.AdminUser, error) {
	const q = `
		UPDATE gogateway.admin_users
		SET username = @p1, password_hash = @p2, role = @p3, active = @p4, updated_at = SYSUTCDATETIME()
		OUTPUT INSERTED.updated_at
		WHERE id = @p5`

	row := r.db.QueryRowContext(ctx, q,
		user.Username, user.PasswordHash, string(user.Role), user.Active, user.ID,
	)
	if err := row.Scan(&user.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return admin.AdminUser{}, fmt.Errorf("admin user id=%d: %w", user.ID, admin.ErrNotFound)
		}
		return admin.AdminUser{}, fmt.Errorf("updating admin user id=%d: %w", user.ID, err)
	}
	return user, nil
}

// ListUsers returns all AdminUsers ordered by username. Includes inactive users.
func (r *AdminRepo) ListUsers(ctx context.Context) ([]admin.AdminUser, error) {
	const q = `
		SELECT id, username, password_hash, role, active, created_at, updated_at
		FROM gogateway.admin_users ORDER BY username`

	rows, err := r.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("listing admin users: %w", err)
	}
	defer rows.Close()

	var users []admin.AdminUser
	for rows.Next() {
		user, err := scanAdminUserFromRows(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning admin user row: %w", err)
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating admin user rows: %w", err)
	}
	return users, nil
}

// CreateSession inserts a new AdminSession and returns it with ID set.
func (r *AdminRepo) CreateSession(ctx context.Context, session admin.AdminSession) (admin.AdminSession, error) {
	const q = `
		INSERT INTO gogateway.admin_sessions (admin_user_id, token_hash, expires_at)
		OUTPUT INSERTED.id, INSERTED.created_at
		VALUES (@p1, @p2, @p3)`

	row := r.db.QueryRowContext(ctx, q,
		session.AdminUserID, session.TokenHash, session.ExpiresAt,
	)
	if err := row.Scan(&session.ID, &session.CreatedAt); err != nil {
		return admin.AdminSession{}, fmt.Errorf("inserting admin session: %w", err)
	}
	return session, nil
}

// GetSessionByTokenHash retrieves an active (not revoked, not expired) session by token hash.
func (r *AdminRepo) GetSessionByTokenHash(ctx context.Context, tokenHash string) (admin.AdminSession, error) {
	const q = `
		SELECT id, admin_user_id, token_hash, expires_at, created_at, revoked_at
		FROM gogateway.admin_sessions
		WHERE token_hash = @p1
		  AND revoked_at IS NULL
		  AND expires_at > SYSUTCDATETIME()`

	row := r.db.QueryRowContext(ctx, q, tokenHash)
	session, err := scanAdminSessionRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return admin.AdminSession{}, fmt.Errorf("session not found or expired: %w", admin.ErrNotFound)
		}
		return admin.AdminSession{}, fmt.Errorf("getting admin session: %w", err)
	}
	return session, nil
}

// RevokeSession sets revoked_at on the session identified by ID.
func (r *AdminRepo) RevokeSession(ctx context.Context, id int64) error {
	const q = `UPDATE gogateway.admin_sessions SET revoked_at = SYSUTCDATETIME() WHERE id = @p1 AND revoked_at IS NULL`

	result, err := r.db.ExecContext(ctx, q, id)
	if err != nil {
		return fmt.Errorf("revoking session id=%d: %w", id, err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking rows affected for session id=%d: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("session id=%d: %w", id, admin.ErrNotFound)
	}
	return nil
}

// RevokeAllUserSessions revokes every active session for a given user.
func (r *AdminRepo) RevokeAllUserSessions(ctx context.Context, userID int64) error {
	const q = `
		UPDATE gogateway.admin_sessions SET revoked_at = SYSUTCDATETIME()
		WHERE admin_user_id = @p1 AND revoked_at IS NULL`

	if _, err := r.db.ExecContext(ctx, q, userID); err != nil {
		return fmt.Errorf("revoking all sessions for user id=%d: %w", userID, err)
	}
	return nil
}

// DeleteExpiredSessions removes rows that are expired or revoked.
func (r *AdminRepo) DeleteExpiredSessions(ctx context.Context) error {
	const q = `DELETE FROM gogateway.admin_sessions WHERE expires_at < SYSUTCDATETIME() OR revoked_at IS NOT NULL`

	if _, err := r.db.ExecContext(ctx, q); err != nil {
		return fmt.Errorf("deleting expired admin sessions: %w", err)
	}
	return nil
}

// ── scan helpers ─────────────────────────────────────────────────────────────

// rowScanner is the common interface between *sql.Row and *sql.Rows (both
// expose Scan(...)). Lets scan helpers serve both single-row and iterating
// callers without duplication.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanAdminUser(s rowScanner) (admin.AdminUser, error) {
	var user admin.AdminUser
	var role string
	err := s.Scan(
		&user.ID, &user.Username, &user.PasswordHash,
		&role, &user.Active, &user.CreatedAt, &user.UpdatedAt,
	)
	if err != nil {
		return admin.AdminUser{}, err
	}
	user.Role = admin.Role(role)
	return user, nil
}

func scanAdminUserRow(row *sql.Row) (admin.AdminUser, error)        { return scanAdminUser(row) }
func scanAdminUserFromRows(rows *sql.Rows) (admin.AdminUser, error) { return scanAdminUser(rows) }

func scanAdminSession(s rowScanner) (admin.AdminSession, error) {
	var sess admin.AdminSession
	var revokedAt sql.NullTime
	err := s.Scan(
		&sess.ID, &sess.AdminUserID, &sess.TokenHash,
		&sess.ExpiresAt, &sess.CreatedAt, &revokedAt,
	)
	if err != nil {
		return admin.AdminSession{}, err
	}
	if revokedAt.Valid {
		t := revokedAt.Time
		sess.RevokedAt = &t
	}
	return sess, nil
}

func scanAdminSessionRow(row *sql.Row) (admin.AdminSession, error) {
	return scanAdminSession(row)
}
