package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/D4nRossi/ai-gateway/internal/domain/endpoint"
	"github.com/D4nRossi/ai-gateway/internal/infra/crypto"
)

// Compile-time assertion: EndpointRepo must satisfy endpoint.Repository.
var _ endpoint.Repository = (*EndpointRepo)(nil)

// EndpointRepo is the pgx implementation of endpoint.Repository.
// It depends on an Encrypter to encrypt and decrypt TargetAuth credentials
// stored in proxy_targets.auth_config_enc (ADR-0012).
type EndpointRepo struct {
	pool      *pgxpool.Pool
	encrypter crypto.Encrypter
}

// NewEndpointRepo constructs an EndpointRepo backed by the given pool and encrypter.
func NewEndpointRepo(pool *pgxpool.Pool, enc crypto.Encrypter) *EndpointRepo {
	return &EndpointRepo{pool: pool, encrypter: enc}
}

// Create inserts a new ProxyEndpoint (without targets) and returns it with ID set.
func (r *EndpointRepo) Create(ctx context.Context, ep endpoint.ProxyEndpoint) (endpoint.ProxyEndpoint, error) {
	const q = `
		INSERT INTO proxy_endpoints (slug, name, provider_kind, lb_strategy, max_rps, max_monthly_requests, active)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, created_at, updated_at`

	row := r.pool.QueryRow(ctx, q,
		ep.Slug, ep.Name, string(ep.ProviderKind), string(ep.LBStrategy),
		ep.MaxRPS, ep.MaxMonthlyRequests, ep.Active,
	)
	if err := row.Scan(&ep.ID, &ep.CreatedAt, &ep.UpdatedAt); err != nil {
		return endpoint.ProxyEndpoint{}, fmt.Errorf("inserting proxy endpoint %q: %w", ep.Slug, err)
	}
	return ep, nil
}

// Get retrieves a ProxyEndpoint by ID including its active targets.
func (r *EndpointRepo) Get(ctx context.Context, id int64) (endpoint.ProxyEndpoint, error) {
	const q = `
		SELECT id, slug, name, provider_kind, lb_strategy, max_rps, max_monthly_requests, active, created_at, updated_at
		FROM proxy_endpoints WHERE id = $1`

	row := r.pool.QueryRow(ctx, q, id)
	ep, err := scanEndpoint(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return endpoint.ProxyEndpoint{}, fmt.Errorf("proxy endpoint id=%d: %w", id, endpoint.ErrNotFound)
		}
		return endpoint.ProxyEndpoint{}, fmt.Errorf("getting proxy endpoint id=%d: %w", id, err)
	}

	ep.Targets, err = r.loadTargets(ctx, ep.ID)
	if err != nil {
		return endpoint.ProxyEndpoint{}, err
	}
	return ep, nil
}

// GetBySlug retrieves an active ProxyEndpoint by slug including its active targets.
func (r *EndpointRepo) GetBySlug(ctx context.Context, slug string) (endpoint.ProxyEndpoint, error) {
	const q = `
		SELECT id, slug, name, provider_kind, lb_strategy, max_rps, max_monthly_requests, active, created_at, updated_at
		FROM proxy_endpoints WHERE slug = $1 AND active = true`

	row := r.pool.QueryRow(ctx, q, slug)
	ep, err := scanEndpoint(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return endpoint.ProxyEndpoint{}, fmt.Errorf("proxy endpoint %q: %w", slug, endpoint.ErrNotFound)
		}
		return endpoint.ProxyEndpoint{}, fmt.Errorf("getting proxy endpoint %q: %w", slug, err)
	}

	ep.Targets, err = r.loadTargets(ctx, ep.ID)
	if err != nil {
		return endpoint.ProxyEndpoint{}, err
	}
	return ep, nil
}

// List returns all ProxyEndpoints without targets, ordered by slug.
func (r *EndpointRepo) List(ctx context.Context) ([]endpoint.ProxyEndpoint, error) {
	const q = `
		SELECT id, slug, name, provider_kind, lb_strategy, max_rps, max_monthly_requests, active, created_at, updated_at
		FROM proxy_endpoints ORDER BY slug`

	rows, err := r.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("listing proxy endpoints: %w", err)
	}
	defer rows.Close()

	var eps []endpoint.ProxyEndpoint
	for rows.Next() {
		ep, err := scanEndpointFromRows(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning proxy endpoint row: %w", err)
		}
		eps = append(eps, ep)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating proxy endpoint rows: %w", err)
	}
	return eps, nil
}

// Update persists changes to an existing ProxyEndpoint. ID must be set.
func (r *EndpointRepo) Update(ctx context.Context, ep endpoint.ProxyEndpoint) (endpoint.ProxyEndpoint, error) {
	const q = `
		UPDATE proxy_endpoints
		SET slug = $1, name = $2, provider_kind = $3, lb_strategy = $4, max_rps = $5,
		    max_monthly_requests = $6, active = $7, updated_at = NOW()
		WHERE id = $8
		RETURNING updated_at`

	row := r.pool.QueryRow(ctx, q,
		ep.Slug, ep.Name, string(ep.ProviderKind), string(ep.LBStrategy),
		ep.MaxRPS, ep.MaxMonthlyRequests, ep.Active, ep.ID,
	)
	if err := row.Scan(&ep.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return endpoint.ProxyEndpoint{}, fmt.Errorf("proxy endpoint id=%d: %w", ep.ID, endpoint.ErrNotFound)
		}
		return endpoint.ProxyEndpoint{}, fmt.Errorf("updating proxy endpoint id=%d: %w", ep.ID, err)
	}
	return ep, nil
}

// Delete soft-deletes a ProxyEndpoint by setting active=false.
func (r *EndpointRepo) Delete(ctx context.Context, id int64) error {
	const q = `UPDATE proxy_endpoints SET active = false, updated_at = NOW() WHERE id = $1`

	tag, err := r.pool.Exec(ctx, q, id)
	if err != nil {
		return fmt.Errorf("deleting proxy endpoint id=%d: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("proxy endpoint id=%d: %w", id, endpoint.ErrNotFound)
	}
	return nil
}

// AddTarget inserts a new Target, encrypting its auth credentials (ADR-0012).
func (r *EndpointRepo) AddTarget(ctx context.Context, t endpoint.Target) (endpoint.Target, error) {
	enc, err := r.encryptAuth(t.Auth)
	if err != nil {
		return endpoint.Target{}, fmt.Errorf("encrypting auth for target in endpoint id=%d: %w", t.EndpointID, err)
	}

	const q = `
		INSERT INTO proxy_targets (endpoint_id, url, weight, auth_type, auth_config_enc, active)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at`

	row := r.pool.QueryRow(ctx, q,
		t.EndpointID, t.URL, t.Weight, string(t.Auth.Type), enc, t.Active,
	)
	if err := row.Scan(&t.ID, &t.CreatedAt); err != nil {
		return endpoint.Target{}, fmt.Errorf("inserting proxy target: %w", err)
	}
	return t, nil
}

// UpdateTarget persists changes to an existing Target. ID must be set.
// Auth credentials are re-encrypted on every update.
func (r *EndpointRepo) UpdateTarget(ctx context.Context, t endpoint.Target) (endpoint.Target, error) {
	enc, err := r.encryptAuth(t.Auth)
	if err != nil {
		return endpoint.Target{}, fmt.Errorf("encrypting auth for target id=%d: %w", t.ID, err)
	}

	const q = `
		UPDATE proxy_targets
		SET url = $1, weight = $2, auth_type = $3, auth_config_enc = $4, active = $5
		WHERE id = $6`

	tag, err := r.pool.Exec(ctx, q, t.URL, t.Weight, string(t.Auth.Type), enc, t.Active, t.ID)
	if err != nil {
		return endpoint.Target{}, fmt.Errorf("updating proxy target id=%d: %w", t.ID, err)
	}
	if tag.RowsAffected() == 0 {
		return endpoint.Target{}, fmt.Errorf("proxy target id=%d: %w", t.ID, endpoint.ErrNotFound)
	}
	return t, nil
}

// RemoveTarget soft-deletes a Target by setting active=false.
func (r *EndpointRepo) RemoveTarget(ctx context.Context, targetID int64) error {
	const q = `UPDATE proxy_targets SET active = false WHERE id = $1`

	tag, err := r.pool.Exec(ctx, q, targetID)
	if err != nil {
		return fmt.Errorf("removing proxy target id=%d: %w", targetID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("proxy target id=%d: %w", targetID, endpoint.ErrNotFound)
	}
	return nil
}

// Grant inserts an application_endpoint_grants row. Idempotent (ON CONFLICT DO NOTHING).
func (r *EndpointRepo) Grant(ctx context.Context, applicationID, endpointID int64) error {
	const q = `
		INSERT INTO application_endpoint_grants (application_id, endpoint_id)
		VALUES ($1, $2)
		ON CONFLICT DO NOTHING`

	if _, err := r.pool.Exec(ctx, q, applicationID, endpointID); err != nil {
		return fmt.Errorf("granting app id=%d to endpoint id=%d: %w", applicationID, endpointID, err)
	}
	return nil
}

// Revoke removes an application_endpoint_grants row.
func (r *EndpointRepo) Revoke(ctx context.Context, applicationID, endpointID int64) error {
	const q = `
		DELETE FROM application_endpoint_grants
		WHERE application_id = $1 AND endpoint_id = $2`

	if _, err := r.pool.Exec(ctx, q, applicationID, endpointID); err != nil {
		return fmt.Errorf("revoking app id=%d from endpoint id=%d: %w", applicationID, endpointID, err)
	}
	return nil
}

// HasGrant reports whether the application has been granted access to the endpoint.
func (r *EndpointRepo) HasGrant(ctx context.Context, applicationID, endpointID int64) (bool, error) {
	const q = `
		SELECT 1 FROM application_endpoint_grants
		WHERE application_id = $1 AND endpoint_id = $2`

	row := r.pool.QueryRow(ctx, q, applicationID, endpointID)
	var one int
	err := row.Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("checking grant app id=%d endpoint id=%d: %w", applicationID, endpointID, err)
	}
	return true, nil
}

// ListGrantedApplicationIDs returns all application IDs granted access to an endpoint.
func (r *EndpointRepo) ListGrantedApplicationIDs(ctx context.Context, endpointID int64) ([]int64, error) {
	const q = `SELECT application_id FROM application_endpoint_grants WHERE endpoint_id = $1`

	rows, err := r.pool.Query(ctx, q, endpointID)
	if err != nil {
		return nil, fmt.Errorf("listing grants for endpoint id=%d: %w", endpointID, err)
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scanning grant row: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating grant rows: %w", err)
	}
	return ids, nil
}

// ListGrantedEndpointIDs returns all endpoint IDs an application has been granted.
func (r *EndpointRepo) ListGrantedEndpointIDs(ctx context.Context, applicationID int64) ([]int64, error) {
	const q = `SELECT endpoint_id FROM application_endpoint_grants WHERE application_id = $1`

	rows, err := r.pool.Query(ctx, q, applicationID)
	if err != nil {
		return nil, fmt.Errorf("listing grants for app id=%d: %w", applicationID, err)
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scanning grant row: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating grant rows: %w", err)
	}
	return ids, nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

func (r *EndpointRepo) loadTargets(ctx context.Context, endpointID int64) ([]endpoint.Target, error) {
	const q = `
		SELECT id, endpoint_id, url, weight, auth_type, auth_config_enc, active, created_at
		FROM proxy_targets
		WHERE endpoint_id = $1 AND active = true
		ORDER BY id`

	rows, err := r.pool.Query(ctx, q, endpointID)
	if err != nil {
		return nil, fmt.Errorf("loading targets for endpoint id=%d: %w", endpointID, err)
	}
	defer rows.Close()

	var targets []endpoint.Target
	for rows.Next() {
		var t endpoint.Target
		var authType string
		var authEnc []byte

		if err := rows.Scan(
			&t.ID, &t.EndpointID, &t.URL, &t.Weight,
			&authType, &authEnc, &t.Active, &t.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning target row: %w", err)
		}

		t.Auth, err = r.decryptAuth(endpoint.AuthType(authType), authEnc)
		if err != nil {
			return nil, fmt.Errorf("decrypting auth for target id=%d: %w", t.ID, err)
		}
		targets = append(targets, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating target rows: %w", err)
	}
	return targets, nil
}

// encryptAuth serializes TargetAuth to JSON and encrypts it (ADR-0012).
// Returns nil when auth.Type == AuthNone.
func (r *EndpointRepo) encryptAuth(auth endpoint.TargetAuth) ([]byte, error) {
	if auth.Type == endpoint.AuthNone {
		return nil, nil
	}
	plain, err := json.Marshal(auth)
	if err != nil {
		return nil, fmt.Errorf("marshalling target auth: %w", err)
	}
	enc, err := r.encrypter.Encrypt(plain)
	if err != nil {
		return nil, fmt.Errorf("encrypting target auth: %w", err)
	}
	return enc, nil
}

// decryptAuth decrypts and deserializes TargetAuth from stored ciphertext.
// Returns AuthNone when authEnc is nil.
func (r *EndpointRepo) decryptAuth(authType endpoint.AuthType, authEnc []byte) (endpoint.TargetAuth, error) {
	if authType == endpoint.AuthNone || len(authEnc) == 0 {
		return endpoint.TargetAuth{Type: endpoint.AuthNone}, nil
	}
	plain, err := r.encrypter.Decrypt(authEnc)
	if err != nil {
		return endpoint.TargetAuth{}, fmt.Errorf("decrypting target auth: %w", err)
	}
	var auth endpoint.TargetAuth
	if err := json.Unmarshal(plain, &auth); err != nil {
		return endpoint.TargetAuth{}, fmt.Errorf("unmarshalling target auth: %w", err)
	}
	return auth, nil
}

func scanEndpoint(row pgx.Row) (endpoint.ProxyEndpoint, error) {
	var ep endpoint.ProxyEndpoint
	var lbs, pk string
	err := row.Scan(
		&ep.ID, &ep.Slug, &ep.Name, &pk, &lbs,
		&ep.MaxRPS, &ep.MaxMonthlyRequests, &ep.Active,
		&ep.CreatedAt, &ep.UpdatedAt,
	)
	if err != nil {
		return endpoint.ProxyEndpoint{}, err
	}
	ep.LBStrategy = endpoint.LBStrategy(lbs)
	ep.ProviderKind = endpoint.ProviderKind(pk)
	return ep, nil
}

func scanEndpointFromRows(rows pgx.Rows) (endpoint.ProxyEndpoint, error) {
	var ep endpoint.ProxyEndpoint
	var lbs, pk string
	err := rows.Scan(
		&ep.ID, &ep.Slug, &ep.Name, &pk, &lbs,
		&ep.MaxRPS, &ep.MaxMonthlyRequests, &ep.Active,
		&ep.CreatedAt, &ep.UpdatedAt,
	)
	if err != nil {
		return endpoint.ProxyEndpoint{}, err
	}
	ep.LBStrategy = endpoint.LBStrategy(lbs)
	ep.ProviderKind = endpoint.ProviderKind(pk)
	return ep, nil
}
