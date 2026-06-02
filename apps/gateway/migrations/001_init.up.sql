-- 001_init.up.sql (T-SQL — SQL Server, schema gogateway)
--
-- Portado de migrations/postgres-legacy/001_init.up.sql como parte da ADR-0022
-- (troca PostgreSQL → SQL Server). Cria o schema `gogateway` (isolado de outras
-- aplicações que vivem no mesmo banco AzureAI_Gateway_hom) e as 3 tabelas core.
--
-- Tipos PG → T-SQL aplicados aqui:
--   BIGSERIAL                  → BIGINT IDENTITY(1,1)
--   TEXT                       → NVARCHAR(...)  (Unicode-safe)
--   INTEGER                    → INT
--   NUMERIC(p,s)               → DECIMAL(p,s)
--   TIMESTAMPTZ                → DATETIMEOFFSET
--   NOW()                      → SYSUTCDATETIME()
--   JSONB                      → NVARCHAR(MAX) + ISJSON CHECK
--
-- References: SPEC.md §8.1, ADR-0022, CLAUDE.md §9.

IF NOT EXISTS (SELECT 1 FROM sys.schemas WHERE name = N'gogateway')
    EXEC(N'CREATE SCHEMA gogateway');

IF OBJECT_ID('gogateway.usage_events', 'U') IS NULL
BEGIN
    CREATE TABLE gogateway.usage_events (
        id                  BIGINT          IDENTITY(1,1) PRIMARY KEY,
        request_id          NVARCHAR(64)    NOT NULL,
        application_name    NVARCHAR(128)   NOT NULL,
        tier                NVARCHAR(20)    NOT NULL,
        model               NVARCHAR(64)    NOT NULL,
        provider            NVARCHAR(40)    NOT NULL,
        input_tokens        INT             NULL,
        output_tokens       INT             NULL,
        total_tokens        INT             NULL,
        latency_ms          INT             NOT NULL,
        status_code         INT             NOT NULL,
        estimated_cost_brl  DECIMAL(12, 6)  NULL,
        created_at          DATETIMEOFFSET  NOT NULL DEFAULT SYSUTCDATETIME()
    );
    CREATE INDEX idx_usage_app_created ON gogateway.usage_events(application_name, created_at DESC);
    CREATE INDEX idx_usage_request     ON gogateway.usage_events(request_id);
END;

IF OBJECT_ID('gogateway.audit_events', 'U') IS NULL
BEGIN
    CREATE TABLE gogateway.audit_events (
        id                  BIGINT          IDENTITY(1,1) PRIMARY KEY,
        request_id          NVARCHAR(64)    NOT NULL,
        application_name    NVARCHAR(128)   NOT NULL,
        event_type          NVARCHAR(64)    NOT NULL,
        severity            NVARCHAR(16)    NOT NULL,
        metadata            NVARCHAR(MAX)   NULL,
        created_at          DATETIMEOFFSET  NOT NULL DEFAULT SYSUTCDATETIME()
    );
    CREATE INDEX idx_audit_app_created ON gogateway.audit_events(application_name, created_at DESC);
    CREATE INDEX idx_audit_event_type  ON gogateway.audit_events(event_type);
    CREATE INDEX idx_audit_request     ON gogateway.audit_events(request_id);
END;

IF OBJECT_ID('gogateway.budget_counters', 'U') IS NULL
BEGIN
    CREATE TABLE gogateway.budget_counters (
        id                  BIGINT          IDENTITY(1,1) PRIMARY KEY,
        application_name    NVARCHAR(128)   NOT NULL,
        period_yyyymm       NVARCHAR(6)     NOT NULL,
        total_requests      BIGINT          NOT NULL DEFAULT 0,
        total_tokens        BIGINT          NOT NULL DEFAULT 0,
        estimated_cost_brl  DECIMAL(14, 6)  NOT NULL DEFAULT 0,
        updated_at          DATETIMEOFFSET  NOT NULL DEFAULT SYSUTCDATETIME(),
        CONSTRAINT uq_budget_counters_app_period UNIQUE (application_name, period_yyyymm)
    );
    CREATE INDEX idx_budget_app ON gogateway.budget_counters(application_name);
END;
