// Package admin defines the core domain types for admin users and their sessions.
// These are pure value objects with no dependency on infrastructure.
//
// Admin users authenticate via bcrypt-verified passwords and receive opaque session tokens
// (ADR-0011). The Role field controls which Admin API operations are permitted.
//
// References:
//   - ADR-0011 — opaque session token authentication
//   - ADR-0015 — domain/app/infra layering
//   - docs/v2-alignment.md — response A (admin auth) and role definitions
package admin

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound is returned by Repository methods when the requested AdminUser or
// AdminSession does not exist. Callers use errors.Is(err, admin.ErrNotFound).
var ErrNotFound = errors.New("admin entity not found")

// Role names the permission level of an admin user.
type Role string

const (
	// RoleAdmin may manage other admin users and perform any operation.
	RoleAdmin Role = "admin"

	// RoleOperator may create and edit applications and endpoints but cannot
	// manage other admin users or change roles.
	RoleOperator Role = "operator"

	// RoleViewer has read-only access: logs, usage, audit, and budget data.
	RoleViewer Role = "viewer"
)

// AdminUser is the domain entity for a human operator who manages the gateway
// via the Admin API and web UI.
type AdminUser struct {
	// ID is the database-assigned surrogate key.
	ID int64

	// Username is the unique login identifier.
	Username string

	// PasswordHash is the bcrypt digest (cost=12) of the user's password.
	// The plaintext password is never stored or logged (ADR-0011).
	PasswordHash string

	// Role controls which Admin API operations this user may perform.
	Role Role

	// Active controls whether this user may log in. Soft-delete semantics.
	Active bool

	CreatedAt time.Time
	UpdatedAt time.Time
}

// AdminSession represents one authenticated admin session.
//
// Lifecycle:
//  1. User POSTs to /admin/v1/auth/login with credentials.
//  2. On success, the gateway generates 32 random bytes, returns them raw to the client
//     (the "raw token"), and stores SHA-256(raw token) as TokenHash.
//  3. Every subsequent admin request carries the raw token; the middleware hashes it
//     and looks up the session in constant time (ADR-0011).
//  4. On logout or revocation, RevokedAt is set to the current timestamp.
type AdminSession struct {
	// ID is the database-assigned surrogate key.
	ID int64

	// AdminUserID is the FK referencing the owning AdminUser.
	AdminUserID int64

	// TokenHash is the SHA-256 hex digest (64 chars) of the raw session token.
	// The raw token is never stored (ADR-0011).
	TokenHash string

	// ExpiresAt is the wall-clock time after which this session is invalid.
	ExpiresAt time.Time

	CreatedAt time.Time

	// RevokedAt is non-nil when the session was explicitly invalidated (logout or ban).
	RevokedAt *time.Time
}

// Repository defines the persistence contract for AdminUser and AdminSession entities.
// All methods accept a context.Context as first argument (CLAUDE.md §5.5).
// The implementation lives in internal/infra/postgres/adminrepo.go.
//
// References:
//   - ADR-0011 — opaque session token authentication
//   - ADR-0015 — repository interfaces belong in the domain package
type Repository interface {
	// CreateUser persists a new AdminUser and returns it with ID and timestamps set.
	CreateUser(ctx context.Context, user AdminUser) (AdminUser, error)

	// GetUser retrieves an AdminUser by its surrogate ID.
	// Returns ErrNotFound if no row matches.
	GetUser(ctx context.Context, id int64) (AdminUser, error)

	// GetUserByUsername retrieves an active AdminUser by username.
	// Returns ErrNotFound if no active row matches.
	GetUserByUsername(ctx context.Context, username string) (AdminUser, error)

	// UpdateUser persists changes to an existing AdminUser. ID must be set.
	// Returns ErrNotFound if no row matches.
	UpdateUser(ctx context.Context, user AdminUser) (AdminUser, error)

	// ListUsers returns all AdminUsers (active and inactive), ordered by username.
	ListUsers(ctx context.Context) ([]AdminUser, error)

	// CreateSession persists a new AdminSession and returns it with ID set.
	CreateSession(ctx context.Context, session AdminSession) (AdminSession, error)

	// GetSessionByTokenHash retrieves an active (non-revoked, non-expired) session
	// matching the given SHA-256 hex token hash. Used by the admin auth middleware.
	// Returns ErrNotFound if no matching active session exists.
	GetSessionByTokenHash(ctx context.Context, tokenHash string) (AdminSession, error)

	// RevokeSession sets revoked_at on the session identified by ID.
	// Returns ErrNotFound if no active session with that ID exists.
	RevokeSession(ctx context.Context, id int64) error

	// RevokeAllUserSessions revokes all active sessions for a given user.
	RevokeAllUserSessions(ctx context.Context, userID int64) error

	// DeleteExpiredSessions removes rows where expires_at < NOW() or revoked_at IS NOT NULL.
	// Safe to run at boot and periodically to bound table growth.
	DeleteExpiredSessions(ctx context.Context) error
}
