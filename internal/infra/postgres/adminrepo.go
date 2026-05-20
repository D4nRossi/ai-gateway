package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/D4nRossi/ai-gateway/internal/domain/admin"
)

// AdminRepo is the pgx implementation of admin.Repository.
type AdminRepo struct {
	pool *pgxpool.Pool
}

// NewAdminRepo constructs an AdminRepo backed by the given pool.
func NewAdminRepo(pool *pgxpool.Pool) *AdminRepo {
	return &AdminRepo{pool: pool}
}

// CreateUser inserts a new AdminUser and returns it with ID and timestamps set.
func (r *AdminRepo) CreateUser(ctx context.Context, user admin.AdminUser) (admin.AdminUser, error) {
	const q = `
		INSERT INTO admin_users (username, password_hash, role, active)
		VALUES ($1, $2, $3, $4)
		RETURNING id, created_at, updated_at`

	row := r.pool.QueryRow(ctx, q,
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
		FROM admin_users WHERE id = $1`

	row := r.pool.QueryRow(ctx, q, id)
	user, err := scanAdminUser(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return admin.AdminUser{}, fmt.Errorf("admin user id=%d not found: %w", id, ErrNotFound)
		}
		return admin.AdminUser{}, fmt.Errorf("getting admin user id=%d: %w", id, err)
	}
	return user, nil
}

// GetUserByUsername retrieves an active AdminUser by username.
// Used during login to locate the user record before bcrypt verification.
func (r *AdminRepo) GetUserByUsername(ctx context.Context, username string) (admin.AdminUser, error) {
	const q = `
		SELECT id, username, password_hash, role, active, created_at, updated_at
		FROM admin_users WHERE username = $1 AND active = true`

	row := r.pool.QueryRow(ctx, q, username)
	user, err := scanAdminUser(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return admin.AdminUser{}, fmt.Errorf("admin user %q not found: %w", username, ErrNotFound)
		}
		return admin.AdminUser{}, fmt.Errorf("getting admin user %q: %w", username, err)
	}
	return user, nil
}

// UpdateUser persists changes to an existing AdminUser. ID must be set.
func (r *AdminRepo) UpdateUser(ctx context.Context, user admin.AdminUser) (admin.AdminUser, error) {
	const q = `
		UPDATE admin_users
		SET username = $1, password_hash = $2, role = $3, active = $4, updated_at = NOW()
		WHERE id = $5
		RETURNING updated_at`

	row := r.pool.QueryRow(ctx, q,
		user.Username, user.PasswordHash, string(user.Role), user.Active, user.ID,
	)
	if err := row.Scan(&user.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return admin.AdminUser{}, fmt.Errorf("admin user id=%d not found: %w", user.ID, ErrNotFound)
		}
		return admin.AdminUser{}, fmt.Errorf("updating admin user id=%d: %w", user.ID, err)
	}
	return user, nil
}

// ListUsers returns all AdminUsers ordered by username. Includes inactive users.
func (r *AdminRepo) ListUsers(ctx context.Context) ([]admin.AdminUser, error) {
	const q = `
		SELECT id, username, password_hash, role, active, created_at, updated_at
		FROM admin_users ORDER BY username`

	rows, err := r.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("listing admin users: %w", err)
	}
	defer rows.Close()

	var users []admin.AdminUser
	for rows.Next() {
		var user admin.AdminUser
		var role string
		if err := rows.Scan(
			&user.ID, &user.Username, &user.PasswordHash,
			&role, &user.Active, &user.CreatedAt, &user.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning admin user row: %w", err)
		}
		user.Role = admin.Role(role)
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
		INSERT INTO admin_sessions (admin_user_id, token_hash, expires_at)
		VALUES ($1, $2, $3)
		RETURNING id, created_at`

	row := r.pool.QueryRow(ctx, q,
		session.AdminUserID, session.TokenHash, session.ExpiresAt,
	)
	if err := row.Scan(&session.ID, &session.CreatedAt); err != nil {
		return admin.AdminSession{}, fmt.Errorf("inserting admin session: %w", err)
	}
	return session, nil
}

// GetSessionByTokenHash retrieves an active (not revoked, not expired) session by token hash.
// This is on the critical path of every admin request.
func (r *AdminRepo) GetSessionByTokenHash(ctx context.Context, tokenHash string) (admin.AdminSession, error) {
	const q = `
		SELECT id, admin_user_id, token_hash, expires_at, created_at, revoked_at
		FROM admin_sessions
		WHERE token_hash = $1
		  AND revoked_at IS NULL
		  AND expires_at > NOW()`

	row := r.pool.QueryRow(ctx, q, tokenHash)
	session, err := scanAdminSession(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return admin.AdminSession{}, fmt.Errorf("session not found or expired: %w", ErrNotFound)
		}
		return admin.AdminSession{}, fmt.Errorf("getting admin session: %w", err)
	}
	return session, nil
}

// RevokeSession sets revoked_at = NOW() on the session identified by ID.
func (r *AdminRepo) RevokeSession(ctx context.Context, id int64) error {
	const q = `UPDATE admin_sessions SET revoked_at = NOW() WHERE id = $1 AND revoked_at IS NULL`

	tag, err := r.pool.Exec(ctx, q, id)
	if err != nil {
		return fmt.Errorf("revoking session id=%d: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("session id=%d not found or already revoked: %w", id, ErrNotFound)
	}
	return nil
}

// RevokeAllUserSessions revokes every active session for a given user.
// Called when deactivating a user or forcing a password reset.
func (r *AdminRepo) RevokeAllUserSessions(ctx context.Context, userID int64) error {
	const q = `
		UPDATE admin_sessions SET revoked_at = NOW()
		WHERE admin_user_id = $1 AND revoked_at IS NULL`

	if _, err := r.pool.Exec(ctx, q, userID); err != nil {
		return fmt.Errorf("revoking all sessions for user id=%d: %w", userID, err)
	}
	return nil
}

// DeleteExpiredSessions removes rows that are expired or revoked to keep the table bounded.
// Safe to run at boot and periodically via a background goroutine.
func (r *AdminRepo) DeleteExpiredSessions(ctx context.Context) error {
	const q = `DELETE FROM admin_sessions WHERE expires_at < NOW() OR revoked_at IS NOT NULL`

	if _, err := r.pool.Exec(ctx, q); err != nil {
		return fmt.Errorf("deleting expired admin sessions: %w", err)
	}
	return nil
}

// ── scan helpers ─────────────────────────────────────────────────────────────

func scanAdminUser(row pgx.Row) (admin.AdminUser, error) {
	var user admin.AdminUser
	var role string
	err := row.Scan(
		&user.ID, &user.Username, &user.PasswordHash,
		&role, &user.Active, &user.CreatedAt, &user.UpdatedAt,
	)
	if err != nil {
		return admin.AdminUser{}, err
	}
	user.Role = admin.Role(role)
	return user, nil
}

func scanAdminSession(row pgx.Row) (admin.AdminSession, error) {
	var s admin.AdminSession
	err := row.Scan(
		&s.ID, &s.AdminUserID, &s.TokenHash,
		&s.ExpiresAt, &s.CreatedAt, &s.RevokedAt,
	)
	return s, err
}
