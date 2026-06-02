-- Migration 003: applications and api_keys tables.
-- Moves per-application configuration from YAML config to PostgreSQL (ADR-0009).
-- Phase 1 YAML-backed PolicyStore remains functional on the main branch; this migration
-- activates the DB-backed store for the V2 admin plane.

CREATE TABLE applications (
    id                   BIGSERIAL     PRIMARY KEY,
    name                 VARCHAR(64)   NOT NULL UNIQUE,
    tier                 VARCHAR(20)   NOT NULL CHECK (tier IN ('tier_1', 'tier_2', 'tier_3')),
    -- comma-separated model public names; stored as TEXT[] for easy containment queries
    allowed_models       TEXT[]        NOT NULL DEFAULT '{}',
    streaming_allowed    BOOLEAN       NOT NULL DEFAULT false,
    -- 0 means no limit enforced at the gateway level
    max_rpm              INTEGER       NOT NULL DEFAULT 0,
    max_tpm              INTEGER       NOT NULL DEFAULT 0,
    monthly_budget_brl   NUMERIC(12,2) NOT NULL DEFAULT 0,
    active               BOOLEAN       NOT NULL DEFAULT true,
    created_at           TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ   NOT NULL DEFAULT NOW()
);

-- Lookup by name on every authenticated request (hot path after cache miss).
CREATE INDEX idx_applications_name_active
    ON applications(name)
    WHERE active = true;

-- One API key per application at any time (UNIQUE enforced by constraint).
-- When rotating, the old row is soft-deleted (rotated_at set) and a new row is inserted.
-- The UNIQUE constraint on application_id ensures atomicity of the swap via transaction.
CREATE TABLE api_keys (
    id              BIGSERIAL    PRIMARY KEY,
    application_id  BIGINT       NOT NULL UNIQUE REFERENCES applications(id) ON DELETE CASCADE,
    -- gwk_{prefix}_ portion — used for O(1) lookup before hash comparison
    key_prefix      VARCHAR(30)  NOT NULL,
    -- SHA-256 hex digest of the full raw bearer token; compared in constant time
    key_hash        VARCHAR(64)  NOT NULL,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    -- NULL until the key is rotated; then set to the rotation timestamp
    rotated_at      TIMESTAMPTZ
);

-- The auth middleware first looks up by prefix to narrow candidates before hash comparison.
CREATE INDEX idx_api_keys_prefix ON api_keys(key_prefix);
