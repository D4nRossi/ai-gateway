-- Migration 004: generic HTTP proxy engine tables.
-- Stores proxy endpoints, their upstream targets, and per-app access grants (ADR-0010, ADR-0013).

CREATE TABLE proxy_endpoints (
    id                    BIGSERIAL    PRIMARY KEY,
    -- URL-safe identifier used in /v1/proxy/{slug}
    slug                  VARCHAR(64)  NOT NULL UNIQUE,
    name                  VARCHAR(128) NOT NULL,
    lb_strategy           VARCHAR(30)  NOT NULL DEFAULT 'round_robin'
                              CHECK (lb_strategy IN (
                                  'round_robin', 'weighted_round_robin',
                                  'random', 'least_connections', 'ip_hash'
                              )),
    -- 0 = unlimited
    max_rps               INTEGER      NOT NULL DEFAULT 0,
    max_monthly_requests  BIGINT       NOT NULL DEFAULT 0,
    active                BOOLEAN      NOT NULL DEFAULT true,
    created_at            TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_proxy_endpoints_slug
    ON proxy_endpoints(slug)
    WHERE active = true;

CREATE TABLE proxy_targets (
    id               BIGSERIAL    PRIMARY KEY,
    endpoint_id      BIGINT       NOT NULL REFERENCES proxy_endpoints(id) ON DELETE CASCADE,
    url              TEXT         NOT NULL,
    -- used by weighted_round_robin; must be > 0
    weight           INTEGER      NOT NULL DEFAULT 1 CHECK (weight > 0),
    auth_type        VARCHAR(20)  NOT NULL DEFAULT 'none'
                         CHECK (auth_type IN ('none', 'bearer_token', 'api_key_header', 'basic_auth')),
    -- AES-256-GCM encrypted JSON of TargetAuth credentials (ADR-0012).
    -- NULL when auth_type = 'none'.
    -- Format: 12-byte nonce || GCM ciphertext (BYTEA).
    auth_config_enc  BYTEA,
    active           BOOLEAN      NOT NULL DEFAULT true,
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_proxy_targets_endpoint
    ON proxy_targets(endpoint_id)
    WHERE active = true;

-- Controls which consumer applications may call which proxy endpoints.
-- An application without a grant receives 403 on /v1/proxy/{slug}.
CREATE TABLE application_endpoint_grants (
    application_id  BIGINT       NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    endpoint_id     BIGINT       NOT NULL REFERENCES proxy_endpoints(id) ON DELETE CASCADE,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    PRIMARY KEY (application_id, endpoint_id)
);

CREATE INDEX idx_grants_endpoint
    ON application_endpoint_grants(endpoint_id);
