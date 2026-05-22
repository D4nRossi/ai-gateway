// Package adminservice implements the application-layer use cases for the admin plane.
// It orchestrates the domain repositories (application, endpoint, admin) and owns all
// business logic that does not belong in handlers or infrastructure:
//
//   - Admin authentication: bcrypt verification, opaque session token generation (ADR-0011)
//   - API key generation: cryptographically random secret, key prefix derivation, SHA-256 hash
//   - Application lifecycle: create (with initial key), update, delete, key rotation
//   - Proxy endpoint lifecycle: create, update, delete, target management, access grants
//   - Admin user management: create, update, deactivate
//
// The package imports only domain types and Go stdlib. It has no knowledge of HTTP,
// SQL, or wire formats, keeping it fully unit-testable without infrastructure (ADR-0015).
//
// References:
//   - ADR-0009 — DB-backed admin plane
//   - ADR-0011 — opaque session token authentication
//   - ADR-0015 — app layer owns business logic; imports domain, not infra
package adminservice

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"
	"unicode"

	"golang.org/x/crypto/bcrypt"

	"github.com/D4nRossi/ai-gateway/internal/domain/admin"
	"github.com/D4nRossi/ai-gateway/internal/domain/application"
	"github.com/D4nRossi/ai-gateway/internal/domain/endpoint"
)

const (
	// bcryptCost is the work factor for hashing admin passwords.
	// Cost 12 takes ~250ms on modern hardware, making offline dictionary attacks expensive.
	bcryptCost = 12

	// defaultSessionTTL is used when no explicit TTL is configured.
	defaultSessionTTL = 8 * time.Hour

	// keyPrefixMaxLen is the maximum number of alphanumeric characters taken from the
	// app name to form the API key prefix (after the "gwk_" literal).
	keyPrefixMaxLen = 10
)

// ErrInvalidCredentials is returned by Login when the username does not exist or the
// password does not match the stored bcrypt hash. The two cases are deliberately
// indistinguishable to prevent username enumeration (ADR-0011).
var ErrInvalidCredentials = errors.New("invalid credentials")

// ErrInvalidProvider is returned by Create/UpdateEndpoint when ProviderKind is
// not in the supported enum (ADR-0016).
var ErrInvalidProvider = errors.New("invalid provider kind")

// Service is the admin application service. It is safe for concurrent use.
type Service struct {
	apps       application.Repository
	endpoints  endpoint.Repository
	admins     admin.Repository
	logger     *slog.Logger
	sessionTTL time.Duration
}

// New constructs a Service. sessionTTL controls how long admin sessions stay valid;
// pass 0 to use the 8-hour default.
//
// References:
//   - ADR-0011 — session TTL is configurable
func New(
	apps application.Repository,
	eps endpoint.Repository,
	admins admin.Repository,
	logger *slog.Logger,
	sessionTTL time.Duration,
) *Service {
	if sessionTTL <= 0 {
		sessionTTL = defaultSessionTTL
	}
	return &Service{
		apps:       apps,
		endpoints:  eps,
		admins:     admins,
		logger:     logger,
		sessionTTL: sessionTTL,
	}
}

// ── Authentication ────────────────────────────────────────────────────────────

// Login verifies admin credentials and, on success, creates a session.
// Returns the raw session token (to be given to the client), the session record,
// and the authenticated user. The raw token is never stored; only its SHA-256
// hash is persisted (ADR-0011).
//
// Returns ErrInvalidCredentials for both unknown username and wrong password
// (prevents username enumeration).
func (s *Service) Login(ctx context.Context, username, password string) (rawToken string, session admin.AdminSession, user admin.AdminUser, err error) {
	user, err = s.admins.GetUserByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, admin.ErrNotFound) {
			// Run bcrypt on a dummy hash to make timing consistent (prevents enumeration).
			_ = bcrypt.CompareHashAndPassword([]byte("$2a$12$dummy"), []byte(password))
			return "", admin.AdminSession{}, admin.AdminUser{}, ErrInvalidCredentials
		}
		return "", admin.AdminSession{}, admin.AdminUser{}, fmt.Errorf("looking up admin user: %w", err)
	}

	if err = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return "", admin.AdminSession{}, admin.AdminUser{}, ErrInvalidCredentials
	}

	rawToken, err = generateRawToken()
	if err != nil {
		return "", admin.AdminSession{}, admin.AdminUser{}, err
	}

	newSession := admin.AdminSession{
		AdminUserID: user.ID,
		TokenHash:   hashToken(rawToken),
		ExpiresAt:   time.Now().UTC().Add(s.sessionTTL),
	}
	newSession, err = s.admins.CreateSession(ctx, newSession)
	if err != nil {
		return "", admin.AdminSession{}, admin.AdminUser{}, fmt.Errorf("creating admin session: %w", err)
	}

	s.logger.Info("admin login",
		"username", user.Username,
		"role", string(user.Role),
		"session_id", newSession.ID,
		"expires_at", newSession.ExpiresAt,
	)
	return rawToken, newSession, user, nil
}

// Logout revokes the session identified by sessionID.
func (s *Service) Logout(ctx context.Context, sessionID int64) error {
	if err := s.admins.RevokeSession(ctx, sessionID); err != nil {
		return fmt.Errorf("revoking session id=%d: %w", sessionID, err)
	}
	return nil
}

// ValidateSession looks up an active session by the raw (client-facing) token.
// Returns the matched session and the owning user, or admin.ErrNotFound if the
// token is invalid, expired, or revoked.
func (s *Service) ValidateSession(ctx context.Context, rawToken string) (admin.AdminSession, admin.AdminUser, error) {
	hash := hashToken(rawToken)

	session, err := s.admins.GetSessionByTokenHash(ctx, hash)
	if err != nil {
		return admin.AdminSession{}, admin.AdminUser{}, fmt.Errorf("validating session: %w", err)
	}

	user, err := s.admins.GetUser(ctx, session.AdminUserID)
	if err != nil {
		return admin.AdminSession{}, admin.AdminUser{}, fmt.Errorf("loading session user: %w", err)
	}
	return session, user, nil
}

// ── Admin user management ─────────────────────────────────────────────────────

// CreateAdminUser creates a new admin user, hashing the plaintext password with bcrypt.
// The caller is responsible for ensuring only admin-role users call this endpoint
// (enforced by the RequireRole middleware in the HTTP layer).
func (s *Service) CreateAdminUser(ctx context.Context, username, password string, role admin.Role) (admin.AdminUser, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return admin.AdminUser{}, fmt.Errorf("hashing password: %w", err)
	}

	user := admin.AdminUser{
		Username:     username,
		PasswordHash: string(hash),
		Role:         role,
		Active:       true,
	}
	user, err = s.admins.CreateUser(ctx, user)
	if err != nil {
		return admin.AdminUser{}, fmt.Errorf("creating admin user %q: %w", username, err)
	}

	s.logger.Info("admin user created", "username", username, "role", string(role))
	return user, nil
}

// ListAdminUsers returns all admin users.
func (s *Service) ListAdminUsers(ctx context.Context) ([]admin.AdminUser, error) {
	users, err := s.admins.ListUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing admin users: %w", err)
	}
	return users, nil
}

// DeactivateAdminUser sets a user as inactive and revokes all their active sessions.
func (s *Service) DeactivateAdminUser(ctx context.Context, userID int64) error {
	user, err := s.admins.GetUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("getting admin user id=%d: %w", userID, err)
	}

	user.Active = false
	if _, err := s.admins.UpdateUser(ctx, user); err != nil {
		return fmt.Errorf("deactivating admin user id=%d: %w", userID, err)
	}

	if err := s.admins.RevokeAllUserSessions(ctx, userID); err != nil {
		// Log but don't fail — the user is already inactive, sessions will expire naturally.
		s.logger.Warn("failed to revoke sessions for deactivated user",
			"user_id", userID, "err", err,
		)
	}
	return nil
}

// ── Application management ────────────────────────────────────────────────────

// CreateApplication creates a new Application with an initial API key.
// Returns the created application, the raw (client-facing) API token, and any error.
// The raw token is returned exactly once and never stored (ADR-0009, ADR-0011).
func (s *Service) CreateApplication(ctx context.Context, app application.Application) (application.Application, string, error) {
	app.Active = true

	rawSecret, err := generateRawToken()
	if err != nil {
		return application.Application{}, "", err
	}

	prefix := deriveKeyPrefix(app.Name)
	fullToken := prefix + "_" + rawSecret

	key := application.APIKey{
		KeyPrefix: prefix,
		KeyHash:   hashToken(fullToken),
	}

	createdApp, _, err := s.apps.CreateWithKey(ctx, app, key)
	if err != nil {
		return application.Application{}, "", fmt.Errorf("creating application %q with key: %w", app.Name, err)
	}

	s.logger.Info("application created",
		"application_name", createdApp.Name,
		"tier", string(createdApp.Tier),
		"key_prefix", prefix,
	)
	return createdApp, fullToken, nil
}

// GetApplication retrieves an Application by ID.
func (s *Service) GetApplication(ctx context.Context, id int64) (application.Application, error) {
	app, err := s.apps.Get(ctx, id)
	if err != nil {
		return application.Application{}, fmt.Errorf("getting application id=%d: %w", id, err)
	}
	return app, nil
}

// ListApplications returns all applications.
func (s *Service) ListApplications(ctx context.Context) ([]application.Application, error) {
	apps, err := s.apps.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing applications: %w", err)
	}
	return apps, nil
}

// UpdateApplication persists changes to an existing application.
func (s *Service) UpdateApplication(ctx context.Context, app application.Application) (application.Application, error) {
	updated, err := s.apps.Update(ctx, app)
	if err != nil {
		return application.Application{}, fmt.Errorf("updating application id=%d: %w", app.ID, err)
	}
	return updated, nil
}

// DeleteApplication soft-deletes an application.
func (s *Service) DeleteApplication(ctx context.Context, id int64) error {
	if err := s.apps.Delete(ctx, id); err != nil {
		return fmt.Errorf("deleting application id=%d: %w", id, err)
	}
	return nil
}

// RotateAPIKey atomically generates a new API key for an application and returns the
// raw (client-facing) token. The previous key stops being valid immediately (ADR-0009).
func (s *Service) RotateAPIKey(ctx context.Context, applicationID int64) (string, error) {
	app, err := s.apps.Get(ctx, applicationID)
	if err != nil {
		return "", fmt.Errorf("getting application id=%d for key rotation: %w", applicationID, err)
	}

	rawSecret, err := generateRawToken()
	if err != nil {
		return "", err
	}

	prefix := deriveKeyPrefix(app.Name)
	fullToken := prefix + "_" + rawSecret

	newKey := application.APIKey{
		KeyPrefix: prefix,
		KeyHash:   hashToken(fullToken),
	}

	if _, err := s.apps.RotateAPIKey(ctx, applicationID, newKey); err != nil {
		return "", fmt.Errorf("rotating api key for app id=%d: %w", applicationID, err)
	}

	s.logger.Info("api key rotated",
		"application_name", app.Name,
		"key_prefix", prefix,
	)
	return fullToken, nil
}

// ── Endpoint management ───────────────────────────────────────────────────────

// CreateEndpoint creates a new proxy endpoint.
//
// Defaults applied here:
//   - LBStrategy: round_robin
//   - ProviderKind: custom (passthrough genérico) — ADR-0016
//
// Validation: ProviderKind must be a value enumerated in domain/endpoint.
// Returns ErrInvalidProvider when unknown.
func (s *Service) CreateEndpoint(ctx context.Context, ep endpoint.ProxyEndpoint) (endpoint.ProxyEndpoint, error) {
	ep.Active = true
	if ep.LBStrategy == "" {
		ep.LBStrategy = endpoint.LBRoundRobin
	}
	if ep.ProviderKind == "" {
		ep.ProviderKind = endpoint.ProviderCustom
	}
	if !ep.ProviderKind.Valid() {
		return endpoint.ProxyEndpoint{}, fmt.Errorf("provider %q: %w", ep.ProviderKind, ErrInvalidProvider)
	}

	created, err := s.endpoints.Create(ctx, ep)
	if err != nil {
		return endpoint.ProxyEndpoint{}, fmt.Errorf("creating endpoint %q: %w", ep.Slug, err)
	}
	return created, nil
}

// GetEndpoint retrieves a proxy endpoint by ID, including its active targets.
func (s *Service) GetEndpoint(ctx context.Context, id int64) (endpoint.ProxyEndpoint, error) {
	ep, err := s.endpoints.Get(ctx, id)
	if err != nil {
		return endpoint.ProxyEndpoint{}, fmt.Errorf("getting endpoint id=%d: %w", id, err)
	}
	return ep, nil
}

// ListEndpoints returns all proxy endpoints without their target lists.
func (s *Service) ListEndpoints(ctx context.Context) ([]endpoint.ProxyEndpoint, error) {
	eps, err := s.endpoints.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing endpoints: %w", err)
	}
	return eps, nil
}

// UpdateEndpoint persists changes to an existing endpoint.
// Same provider_kind validation as CreateEndpoint (ADR-0016).
func (s *Service) UpdateEndpoint(ctx context.Context, ep endpoint.ProxyEndpoint) (endpoint.ProxyEndpoint, error) {
	if ep.ProviderKind == "" {
		ep.ProviderKind = endpoint.ProviderCustom
	}
	if !ep.ProviderKind.Valid() {
		return endpoint.ProxyEndpoint{}, fmt.Errorf("provider %q: %w", ep.ProviderKind, ErrInvalidProvider)
	}

	updated, err := s.endpoints.Update(ctx, ep)
	if err != nil {
		return endpoint.ProxyEndpoint{}, fmt.Errorf("updating endpoint id=%d: %w", ep.ID, err)
	}
	return updated, nil
}

// DeleteEndpoint soft-deletes a proxy endpoint.
func (s *Service) DeleteEndpoint(ctx context.Context, id int64) error {
	if err := s.endpoints.Delete(ctx, id); err != nil {
		return fmt.Errorf("deleting endpoint id=%d: %w", id, err)
	}
	return nil
}

// AddTarget adds a new upstream target to a proxy endpoint. The target's auth
// credentials are encrypted by the repository layer (ADR-0012).
func (s *Service) AddTarget(ctx context.Context, t endpoint.Target) (endpoint.Target, error) {
	t.Active = true
	if t.Weight <= 0 {
		t.Weight = 1
	}
	created, err := s.endpoints.AddTarget(ctx, t)
	if err != nil {
		return endpoint.Target{}, fmt.Errorf("adding target to endpoint id=%d: %w", t.EndpointID, err)
	}
	return created, nil
}

// UpdateTarget persists changes to a target (including re-encrypting its credentials).
func (s *Service) UpdateTarget(ctx context.Context, t endpoint.Target) (endpoint.Target, error) {
	updated, err := s.endpoints.UpdateTarget(ctx, t)
	if err != nil {
		return endpoint.Target{}, fmt.Errorf("updating target id=%d: %w", t.ID, err)
	}
	return updated, nil
}

// RemoveTarget soft-deletes a target.
func (s *Service) RemoveTarget(ctx context.Context, targetID int64) error {
	if err := s.endpoints.RemoveTarget(ctx, targetID); err != nil {
		return fmt.Errorf("removing target id=%d: %w", targetID, err)
	}
	return nil
}

// ListEndpointGrants returns every proxy endpoint an application has been
// granted access to. Used by the admin UI access matrix.
func (s *Service) ListEndpointGrants(ctx context.Context, applicationID int64) ([]endpoint.ProxyEndpoint, error) {
	ids, err := s.endpoints.ListGrantedEndpointIDs(ctx, applicationID)
	if err != nil {
		return nil, fmt.Errorf("listing endpoint grants for app id=%d: %w", applicationID, err)
	}
	if len(ids) == 0 {
		return nil, nil
	}
	// N+1 is acceptable here — admin pages are low-traffic and the grant count
	// per application is typically < 20. If this becomes a hotspot, switch to
	// a JOIN in a dedicated repository method.
	out := make([]endpoint.ProxyEndpoint, 0, len(ids))
	for _, id := range ids {
		ep, err := s.endpoints.Get(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("loading granted endpoint id=%d: %w", id, err)
		}
		out = append(out, ep)
	}
	return out, nil
}

// GrantAccess allows an application to call a proxy endpoint.
func (s *Service) GrantAccess(ctx context.Context, applicationID, endpointID int64) error {
	if err := s.endpoints.Grant(ctx, applicationID, endpointID); err != nil {
		return fmt.Errorf("granting access app id=%d endpoint id=%d: %w", applicationID, endpointID, err)
	}
	return nil
}

// RevokeAccess removes an application's access to a proxy endpoint.
func (s *Service) RevokeAccess(ctx context.Context, applicationID, endpointID int64) error {
	if err := s.endpoints.Revoke(ctx, applicationID, endpointID); err != nil {
		return fmt.Errorf("revoking access app id=%d endpoint id=%d: %w", applicationID, endpointID, err)
	}
	return nil
}

// ── Maintenance ───────────────────────────────────────────────────────────────

// PurgeExpiredSessions deletes expired or revoked session rows. Safe to call at boot
// and periodically (e.g., daily) to keep admin_sessions bounded (ADR-0011).
func (s *Service) PurgeExpiredSessions(ctx context.Context) error {
	if err := s.admins.DeleteExpiredSessions(ctx); err != nil {
		return fmt.Errorf("purging expired sessions: %w", err)
	}
	return nil
}

// ── Private helpers ───────────────────────────────────────────────────────────

// generateRawToken returns 32 cryptographically random bytes encoded as 64 lowercase
// hex characters. This is both the secret portion of API keys and the opaque
// session token (before prefixing for API keys).
func generateRawToken() (string, error) {
	b := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", fmt.Errorf("generating random token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// hashToken returns the SHA-256 hex digest of a raw token string.
// Used for both API key hashes and session token hashes (ADR-0009, ADR-0011).
func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// deriveKeyPrefix builds the "gwk_{name}" prefix for an API key from an application name.
// Takes up to keyPrefixMaxLen alphanumeric characters from the name (lowercased),
// which is what the auth middleware stores and queries for O(1) candidate lookup.
//
// Examples: "AppDemo" → "gwk_appdemo", "My-Service-v2" → "gwk_myservicev"
func deriveKeyPrefix(name string) string {
	var b strings.Builder
	b.WriteString("gwk_")
	for _, c := range strings.ToLower(name) {
		if unicode.IsLetter(c) || unicode.IsDigit(c) {
			b.WriteRune(c)
			if b.Len() >= 4+keyPrefixMaxLen { // 4 = len("gwk_")
				break
			}
		}
	}
	return b.String()
}
