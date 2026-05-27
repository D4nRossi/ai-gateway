-- 003_applications_db.up.sql (T-SQL)
--
-- Portado de migrations/postgres-legacy/003_applications_db.up.sql (ADR-0022).
-- Tabelas applications + api_keys (ADR-0009).
--
-- Nota sobre TEXT[]: PostgreSQL guardava allowed_models como TEXT[] nativo.
-- SQL Server não tem array tipado; armazenamos como NVARCHAR(MAX) contendo
-- JSON array (e.g. N'["gpt-4.1-nano","gpt-4.1-mini"]'). CHECK ISJSON garante
-- conteúdo válido; o código Go faz json.Marshal/Unmarshal.
--
-- Nota sobre UNIQUE(application_id) em api_keys: criada aqui igual ao PG
-- original para preservar a coreografia das migrations. Migration 007 troca
-- por filtered unique (WHERE rotated_at IS NULL) — desbloqueia rotação atômica.

IF OBJECT_ID('gogateway.applications', 'U') IS NULL
BEGIN
    CREATE TABLE gogateway.applications (
        id                  BIGINT          IDENTITY(1,1) PRIMARY KEY,
        name                NVARCHAR(64)    NOT NULL,
        tier                NVARCHAR(20)    NOT NULL
                                CONSTRAINT ck_applications_tier CHECK (tier IN ('tier_1', 'tier_2', 'tier_3')),
        allowed_models      NVARCHAR(MAX)   NOT NULL
                                CONSTRAINT df_applications_allowed_models DEFAULT N'[]'
                                CONSTRAINT ck_applications_allowed_models CHECK (ISJSON(allowed_models) = 1),
        streaming_allowed   BIT             NOT NULL DEFAULT 0,
        max_rpm             INT             NOT NULL DEFAULT 0,
        max_tpm             INT             NOT NULL DEFAULT 0,
        monthly_budget_brl  DECIMAL(12, 2)  NOT NULL DEFAULT 0,
        active              BIT             NOT NULL DEFAULT 1,
        created_at          DATETIMEOFFSET  NOT NULL DEFAULT SYSUTCDATETIME(),
        updated_at          DATETIMEOFFSET  NOT NULL DEFAULT SYSUTCDATETIME(),
        CONSTRAINT uq_applications_name UNIQUE (name)
    );

    -- Filtered index para o hot path de auth (lookup por name onde active = 1).
    CREATE INDEX idx_applications_name_active
        ON gogateway.applications(name)
        WHERE active = 1;
END;

IF OBJECT_ID('gogateway.api_keys', 'U') IS NULL
BEGIN
    CREATE TABLE gogateway.api_keys (
        id              BIGINT          IDENTITY(1,1) PRIMARY KEY,
        application_id  BIGINT          NOT NULL,
        -- gwk_{prefix}_ — usado para lookup O(1) antes da comparação de hash.
        key_prefix      NVARCHAR(30)    NOT NULL,
        -- SHA-256 hex digest do token bruto completo (constant-time compare).
        key_hash        NVARCHAR(64)    NOT NULL,
        created_at      DATETIMEOFFSET  NOT NULL DEFAULT SYSUTCDATETIME(),
        rotated_at      DATETIMEOFFSET  NULL,
        CONSTRAINT fk_api_keys_application FOREIGN KEY (application_id) REFERENCES gogateway.applications(id) ON DELETE CASCADE,
        -- UNIQUE total — substituída pelo filtered unique de 007 para suportar rotação atômica.
        CONSTRAINT uq_api_keys_application UNIQUE (application_id)
    );

    -- Índice para o lookup por prefix no hot path de auth.
    CREATE INDEX idx_api_keys_prefix ON gogateway.api_keys(key_prefix);
END;
