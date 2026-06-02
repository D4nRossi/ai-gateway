-- Migration 001 UP: create usage_events, audit_events, budget_counters tables.
-- References: SPEC.md §8.1

CREATE TABLE usage_events (
    id                  BIGSERIAL PRIMARY KEY,
    request_id          TEXT        NOT NULL,
    application_name    TEXT        NOT NULL,
    tier                TEXT        NOT NULL,
    model               TEXT        NOT NULL,
    provider            TEXT        NOT NULL,
    input_tokens        INTEGER,
    output_tokens       INTEGER,
    total_tokens        INTEGER,
    latency_ms          INTEGER     NOT NULL,
    status_code         INTEGER     NOT NULL,
    estimated_cost_brl  NUMERIC(12, 6),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_usage_app_created ON usage_events(application_name, created_at DESC);
CREATE INDEX idx_usage_request     ON usage_events(request_id);

CREATE TABLE audit_events (
    id                  BIGSERIAL PRIMARY KEY,
    request_id          TEXT        NOT NULL,
    application_name    TEXT        NOT NULL,
    event_type          TEXT        NOT NULL,
    severity            TEXT        NOT NULL,
    metadata            JSONB,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_audit_app_created ON audit_events(application_name, created_at DESC);
CREATE INDEX idx_audit_event_type  ON audit_events(event_type);
CREATE INDEX idx_audit_request     ON audit_events(request_id);

CREATE TABLE budget_counters (
    id                  BIGSERIAL PRIMARY KEY,
    application_name    TEXT           NOT NULL,
    period_yyyymm       TEXT           NOT NULL,
    total_requests      BIGINT         NOT NULL DEFAULT 0,
    total_tokens        BIGINT         NOT NULL DEFAULT 0,
    estimated_cost_brl  NUMERIC(14, 6) NOT NULL DEFAULT 0,
    updated_at          TIMESTAMPTZ    NOT NULL DEFAULT NOW(),
    UNIQUE (application_name, period_yyyymm)
);
CREATE INDEX idx_budget_app ON budget_counters(application_name);
