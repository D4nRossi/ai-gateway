-- Migration 002: admin users and session tables.
-- Supports the opaque-session admin authentication scheme (ADR-0011).

CREATE TABLE admin_users (
    id            BIGSERIAL    PRIMARY KEY,
    username      VARCHAR(64)  NOT NULL UNIQUE,
    -- bcrypt hash of the password (cost=12); never plaintext
    password_hash VARCHAR(72)  NOT NULL,
    role          VARCHAR(20)  NOT NULL CHECK (role IN ('admin', 'operator', 'viewer')),
    active        BOOLEAN      NOT NULL DEFAULT true,
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE TABLE admin_sessions (
    id             BIGSERIAL    PRIMARY KEY,
    admin_user_id  BIGINT       NOT NULL REFERENCES admin_users(id) ON DELETE CASCADE,
    -- SHA-256 hex digest of the raw 32-byte token returned to the client
    token_hash     VARCHAR(64)  NOT NULL UNIQUE,
    expires_at     TIMESTAMPTZ  NOT NULL,
    created_at     TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    -- NULL means session is active; NOT NULL means it was explicitly revoked
    revoked_at     TIMESTAMPTZ
);

-- Fast lookup for the per-request auth middleware: find an active, non-expired session by hash.
CREATE INDEX idx_admin_sessions_token
    ON admin_sessions(token_hash)
    WHERE revoked_at IS NULL;

CREATE INDEX idx_admin_sessions_user
    ON admin_sessions(admin_user_id);
