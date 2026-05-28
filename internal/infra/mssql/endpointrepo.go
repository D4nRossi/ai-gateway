package mssql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/D4nRossi/ai-gateway/internal/domain/endpoint"
	"github.com/D4nRossi/ai-gateway/internal/infra/crypto"
)

// Compile-time assertion: EndpointRepo must satisfy endpoint.Repository.
var _ endpoint.Repository = (*EndpointRepo)(nil)

// EndpointRepo is the SQL Server implementation of endpoint.Repository.
// It depends on an Encrypter to encrypt and decrypt TargetAuth credentials
// stored in proxy_targets.auth_config_enc (ADR-0012).
type EndpointRepo struct {
	db        *sql.DB
	encrypter crypto.Encrypter
}

// NewEndpointRepo constructs an EndpointRepo backed by the given handle and encrypter.
func NewEndpointRepo(db *sql.DB, enc crypto.Encrypter) *EndpointRepo {
	return &EndpointRepo{db: db, encrypter: enc}
}

// Create inserts a new ProxyEndpoint (without targets) and returns it with ID set.
func (r *EndpointRepo) Create(ctx context.Context, ep endpoint.ProxyEndpoint) (endpoint.ProxyEndpoint, error) {
	pcJSON, err := marshalProviderConfig(ep.ProviderConfig)
	if err != nil {
		return endpoint.ProxyEndpoint{}, fmt.Errorf("marshalling provider_config for endpoint %q: %w", ep.Slug, err)
	}

	const q = `
		INSERT INTO gogateway.proxy_endpoints
		    (slug, name, provider_kind, provider_config, lb_strategy, max_rps, max_monthly_requests, active)
		OUTPUT INSERTED.id, INSERTED.created_at, INSERTED.updated_at
		VALUES (@p1, @p2, @p3, @p4, @p5, @p6, @p7, @p8)`

	row := r.db.QueryRowContext(ctx, q,
		ep.Slug, ep.Name, string(ep.ProviderKind), pcJSON, string(ep.LBStrategy),
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
		SELECT id, slug, name, provider_kind, provider_config, lb_strategy, max_rps, max_monthly_requests, active, created_at, updated_at
		FROM gogateway.proxy_endpoints WHERE id = @p1`

	row := r.db.QueryRowContext(ctx, q, id)
	ep, err := scanEndpointRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
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
		SELECT id, slug, name, provider_kind, provider_config, lb_strategy, max_rps, max_monthly_requests, active, created_at, updated_at
		FROM gogateway.proxy_endpoints WHERE slug = @p1 AND active = 1`

	row := r.db.QueryRowContext(ctx, q, slug)
	ep, err := scanEndpointRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
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
		SELECT id, slug, name, provider_kind, provider_config, lb_strategy, max_rps, max_monthly_requests, active, created_at, updated_at
		FROM gogateway.proxy_endpoints ORDER BY slug`

	rows, err := r.db.QueryContext(ctx, q)
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
	pcJSON, err := marshalProviderConfig(ep.ProviderConfig)
	if err != nil {
		return endpoint.ProxyEndpoint{}, fmt.Errorf("marshalling provider_config for endpoint id=%d: %w", ep.ID, err)
	}

	const q = `
		UPDATE gogateway.proxy_endpoints
		SET slug = @p1, name = @p2, provider_kind = @p3, provider_config = @p4, lb_strategy = @p5, max_rps = @p6,
		    max_monthly_requests = @p7, active = @p8, updated_at = SYSUTCDATETIME()
		OUTPUT INSERTED.updated_at
		WHERE id = @p9`

	row := r.db.QueryRowContext(ctx, q,
		ep.Slug, ep.Name, string(ep.ProviderKind), pcJSON, string(ep.LBStrategy),
		ep.MaxRPS, ep.MaxMonthlyRequests, ep.Active, ep.ID,
	)
	if err := row.Scan(&ep.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return endpoint.ProxyEndpoint{}, fmt.Errorf("proxy endpoint id=%d: %w", ep.ID, endpoint.ErrNotFound)
		}
		return endpoint.ProxyEndpoint{}, fmt.Errorf("updating proxy endpoint id=%d: %w", ep.ID, err)
	}
	return ep, nil
}

// Delete soft-deletes a ProxyEndpoint by setting active=0.
func (r *EndpointRepo) Delete(ctx context.Context, id int64) error {
	const q = `UPDATE gogateway.proxy_endpoints SET active = 0, updated_at = SYSUTCDATETIME() WHERE id = @p1`

	result, err := r.db.ExecContext(ctx, q, id)
	if err != nil {
		return fmt.Errorf("deleting proxy endpoint id=%d: %w", id, err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking rows affected for endpoint id=%d: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("proxy endpoint id=%d: %w", id, endpoint.ErrNotFound)
	}
	return nil
}

// AddTarget inserts a new Target, encrypting its auth credentials (ADR-0012).
// Default CredentialStorageMode is CredentialModeAES when not set by the caller
// (preserves pre-ADR-0020 behavior). The repo treats credential_storage_mode
// and kv_secret_name as pure persistence: the service layer is responsible
// for coordinating KV writes ordered with the AES persistence (ADR-0020).
func (r *EndpointRepo) AddTarget(ctx context.Context, t endpoint.Target) (endpoint.Target, error) {
	enc, err := r.encryptAuth(t.Auth)
	if err != nil {
		return endpoint.Target{}, fmt.Errorf("encrypting auth for target in endpoint id=%d: %w", t.EndpointID, err)
	}

	mode := t.CredentialStorageMode
	if mode == "" {
		mode = endpoint.CredentialModeAES
	}

	const q = `
		INSERT INTO gogateway.proxy_targets
		    (endpoint_id, url, weight, auth_type, auth_config_enc, credential_storage_mode, kv_secret_name, active)
		OUTPUT INSERTED.id, INSERTED.created_at
		VALUES (@p1, @p2, @p3, @p4, @p5, @p6, @p7, @p8)`

	row := r.db.QueryRowContext(ctx, q,
		t.EndpointID, t.URL, t.Weight, string(t.Auth.Type), enc,
		string(mode), nullableString(t.KVSecretName), t.Active,
	)
	if err := row.Scan(&t.ID, &t.CreatedAt); err != nil {
		return endpoint.Target{}, fmt.Errorf("inserting proxy target: %w", err)
	}
	t.CredentialStorageMode = mode
	return t, nil
}

// UpdateTarget persists changes to an existing Target. ID must be set.
// Auth credentials are re-encrypted on every update. As in AddTarget, an
// empty CredentialStorageMode defaults to CredentialModeAES.
func (r *EndpointRepo) UpdateTarget(ctx context.Context, t endpoint.Target) (endpoint.Target, error) {
	enc, err := r.encryptAuth(t.Auth)
	if err != nil {
		return endpoint.Target{}, fmt.Errorf("encrypting auth for target id=%d: %w", t.ID, err)
	}

	mode := t.CredentialStorageMode
	if mode == "" {
		mode = endpoint.CredentialModeAES
	}

	const q = `
		UPDATE gogateway.proxy_targets
		SET url = @p1, weight = @p2, auth_type = @p3, auth_config_enc = @p4,
		    credential_storage_mode = @p5, kv_secret_name = @p6, active = @p7
		WHERE id = @p8`

	result, err := r.db.ExecContext(ctx, q,
		t.URL, t.Weight, string(t.Auth.Type), enc,
		string(mode), nullableString(t.KVSecretName), t.Active, t.ID,
	)
	if err != nil {
		return endpoint.Target{}, fmt.Errorf("updating proxy target id=%d: %w", t.ID, err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return endpoint.Target{}, fmt.Errorf("checking rows affected for target id=%d: %w", t.ID, err)
	}
	if n == 0 {
		return endpoint.Target{}, fmt.Errorf("proxy target id=%d: %w", t.ID, endpoint.ErrNotFound)
	}
	t.CredentialStorageMode = mode
	return t, nil
}

// RemoveTarget soft-deletes a Target by setting active=0.
func (r *EndpointRepo) RemoveTarget(ctx context.Context, targetID int64) error {
	const q = `UPDATE gogateway.proxy_targets SET active = 0 WHERE id = @p1`

	result, err := r.db.ExecContext(ctx, q, targetID)
	if err != nil {
		return fmt.Errorf("removing proxy target id=%d: %w", targetID, err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking rows affected for target id=%d: %w", targetID, err)
	}
	if n == 0 {
		return fmt.Errorf("proxy target id=%d: %w", targetID, endpoint.ErrNotFound)
	}
	return nil
}

// Grant inserts an application_endpoint_grants row. Idempotent: the
// IF NOT EXISTS wrapper makes it safe to call when the grant already
// exists (equivalent to PG's ON CONFLICT DO NOTHING).
func (r *EndpointRepo) Grant(ctx context.Context, applicationID, endpointID int64) error {
	const q = `
		IF NOT EXISTS (
		    SELECT 1 FROM gogateway.application_endpoint_grants
		    WHERE application_id = @p1 AND endpoint_id = @p2
		)
		INSERT INTO gogateway.application_endpoint_grants (application_id, endpoint_id)
		VALUES (@p1, @p2)`

	if _, err := r.db.ExecContext(ctx, q, applicationID, endpointID); err != nil {
		return fmt.Errorf("granting app id=%d to endpoint id=%d: %w", applicationID, endpointID, err)
	}
	return nil
}

// Revoke removes an application_endpoint_grants row.
func (r *EndpointRepo) Revoke(ctx context.Context, applicationID, endpointID int64) error {
	const q = `
		DELETE FROM gogateway.application_endpoint_grants
		WHERE application_id = @p1 AND endpoint_id = @p2`

	if _, err := r.db.ExecContext(ctx, q, applicationID, endpointID); err != nil {
		return fmt.Errorf("revoking app id=%d from endpoint id=%d: %w", applicationID, endpointID, err)
	}
	return nil
}

// HasGrant reports whether the application has been granted access to the endpoint.
func (r *EndpointRepo) HasGrant(ctx context.Context, applicationID, endpointID int64) (bool, error) {
	const q = `
		SELECT 1 FROM gogateway.application_endpoint_grants
		WHERE application_id = @p1 AND endpoint_id = @p2`

	row := r.db.QueryRowContext(ctx, q, applicationID, endpointID)
	var one int
	err := row.Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("checking grant app id=%d endpoint id=%d: %w", applicationID, endpointID, err)
	}
	return true, nil
}

// ListGrantedApplicationIDs returns all application IDs granted access to an endpoint.
func (r *EndpointRepo) ListGrantedApplicationIDs(ctx context.Context, endpointID int64) ([]int64, error) {
	const q = `SELECT application_id FROM gogateway.application_endpoint_grants WHERE endpoint_id = @p1`

	rows, err := r.db.QueryContext(ctx, q, endpointID)
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
	const q = `SELECT endpoint_id FROM gogateway.application_endpoint_grants WHERE application_id = @p1`

	rows, err := r.db.QueryContext(ctx, q, applicationID)
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
		SELECT id, endpoint_id, url, weight, auth_type, auth_config_enc,
		       credential_storage_mode, kv_secret_name, active, created_at
		FROM gogateway.proxy_targets
		WHERE endpoint_id = @p1 AND active = 1
		ORDER BY id`

	rows, err := r.db.QueryContext(ctx, q, endpointID)
	if err != nil {
		return nil, fmt.Errorf("loading targets for endpoint id=%d: %w", endpointID, err)
	}
	defer rows.Close()

	var targets []endpoint.Target
	for rows.Next() {
		var t endpoint.Target
		var authType string
		var authEnc []byte
		var mode string
		var kvName sql.NullString

		if err := rows.Scan(
			&t.ID, &t.EndpointID, &t.URL, &t.Weight,
			&authType, &authEnc, &mode, &kvName, &t.Active, &t.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning target row: %w", err)
		}

		// Decrypt the AES copy when present. For CredentialModeKV with NULL
		// auth_config_enc this yields AuthNone, which the resolver detects
		// and routes to Key Vault instead. For CredentialModeBoth the
		// decrypted Auth is the freshness cache used as fallback when KV
		// times out (ADR-0020).
		t.Auth, err = r.decryptAuth(endpoint.AuthType(authType), authEnc)
		if err != nil {
			return nil, fmt.Errorf("decrypting auth for target id=%d: %w", t.ID, err)
		}
		t.CredentialStorageMode = endpoint.CredentialStorageMode(mode)
		if kvName.Valid {
			t.KVSecretName = kvName.String
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

// ── scan helpers ─────────────────────────────────────────────────────────────

func scanEndpoint(s rowScanner) (endpoint.ProxyEndpoint, error) {
	var ep endpoint.ProxyEndpoint
	var lbs, pk string
	// provider_config é NVARCHAR(MAX) JSON; scan como []byte preserva o pipe
	// "armazenado-como-bytes" do helper unmarshalProviderConfig.
	var pcRaw []byte
	err := s.Scan(
		&ep.ID, &ep.Slug, &ep.Name, &pk, &pcRaw, &lbs,
		&ep.MaxRPS, &ep.MaxMonthlyRequests, &ep.Active,
		&ep.CreatedAt, &ep.UpdatedAt,
	)
	if err != nil {
		return endpoint.ProxyEndpoint{}, err
	}
	ep.LBStrategy = endpoint.LBStrategy(lbs)
	ep.ProviderKind = endpoint.ProviderKind(pk)
	ep.ProviderConfig, err = unmarshalProviderConfig(pcRaw)
	if err != nil {
		return endpoint.ProxyEndpoint{}, fmt.Errorf("unmarshalling provider_config: %w", err)
	}
	return ep, nil
}

func scanEndpointRow(row *sql.Row) (endpoint.ProxyEndpoint, error) {
	return scanEndpoint(row)
}

func scanEndpointFromRows(rows *sql.Rows) (endpoint.ProxyEndpoint, error) {
	return scanEndpoint(rows)
}

// marshalProviderConfig serializes a ProviderConfig to a JSON STRING. A
// nil/empty map becomes "{}" so the DB never sees null on a NOT NULL
// NVARCHAR(MAX) column guarded by ISJSON CHECK.
//
// Why string (not []byte): the microsoft/go-mssqldb driver maps []byte
// parameters to VARBINARY, NOT to NVARCHAR. When the SQL Server then
// coerces VARBINARY → NVARCHAR(MAX) implicitly, the resulting value is the
// hex representation of the bytes (e.g. "0x7B7D" for `{}`), which is NOT
// valid JSON — so the ISJSON CHECK constraint on provider_config rejects
// the row. Returning string sidesteps that: driver maps string → NVARCHAR
// directly, and ISJSON sees the literal `{}`/`{"key":...}`. Same applies to
// audit_events.metadata (handled in internal/audit/writer.go).
func marshalProviderConfig(pc endpoint.ProviderConfig) (string, error) {
	if len(pc) == 0 {
		return "{}", nil
	}
	b, err := json.Marshal(pc)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// unmarshalProviderConfig parses NVARCHAR(MAX) JSON bytes into a ProviderConfig.
// Empty input or "{}" yields a non-nil empty map.
func unmarshalProviderConfig(raw []byte) (endpoint.ProviderConfig, error) {
	pc := endpoint.ProviderConfig{}
	if len(raw) == 0 {
		return pc, nil
	}
	if err := json.Unmarshal(raw, &pc); err != nil {
		return nil, err
	}
	return pc, nil
}

// nullableString converts a Go string to sql.NullString so empty values map
// to NULL on the SQL Server side. Used for nullable NVARCHAR columns where
// the difference between "" and NULL matters (e.g. CHECK constraints that
// reject empty strings — see proxy_targets.kv_secret_name in migration 011).
func nullableString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}
