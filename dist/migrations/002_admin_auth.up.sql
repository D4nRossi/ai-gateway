-- 002_admin_auth.up.sql (T-SQL)
--
-- Portado de migrations/postgres-legacy/002_admin_auth.up.sql (ADR-0022).
-- Tabelas admin_users + admin_sessions para autenticação opaca (ADR-0011).

IF OBJECT_ID('gogateway.admin_users', 'U') IS NULL
BEGIN
    CREATE TABLE gogateway.admin_users (
        id            BIGINT         IDENTITY(1,1) PRIMARY KEY,
        username      NVARCHAR(64)   NOT NULL,
        -- bcrypt hash de senha (cost=12); nunca plaintext
        password_hash NVARCHAR(72)   NOT NULL,
        role          NVARCHAR(20)   NOT NULL
                          CONSTRAINT ck_admin_users_role CHECK (role IN ('admin', 'operator', 'viewer')),
        active        BIT            NOT NULL DEFAULT 1,
        created_at    DATETIMEOFFSET NOT NULL DEFAULT SYSUTCDATETIME(),
        updated_at    DATETIMEOFFSET NOT NULL DEFAULT SYSUTCDATETIME(),
        CONSTRAINT uq_admin_users_username UNIQUE (username)
    );
END;

IF OBJECT_ID('gogateway.admin_sessions', 'U') IS NULL
BEGIN
    CREATE TABLE gogateway.admin_sessions (
        id            BIGINT         IDENTITY(1,1) PRIMARY KEY,
        admin_user_id BIGINT         NOT NULL,
        -- SHA-256 hex digest do raw token (nunca plaintext)
        token_hash    NVARCHAR(64)   NOT NULL,
        expires_at    DATETIMEOFFSET NOT NULL,
        created_at    DATETIMEOFFSET NOT NULL DEFAULT SYSUTCDATETIME(),
        -- NULL = sessão ativa; NOT NULL = revogada
        revoked_at    DATETIMEOFFSET NULL,
        CONSTRAINT fk_admin_sessions_user
            FOREIGN KEY (admin_user_id) REFERENCES gogateway.admin_users(id) ON DELETE CASCADE,
        CONSTRAINT uq_admin_sessions_token UNIQUE (token_hash)
    );

    -- Filtered index pro hot path de auth (lookup por hash, só sessões ativas).
    CREATE INDEX idx_admin_sessions_token
        ON gogateway.admin_sessions(token_hash)
        WHERE revoked_at IS NULL;

    CREATE INDEX idx_admin_sessions_user
        ON gogateway.admin_sessions(admin_user_id);
END;
