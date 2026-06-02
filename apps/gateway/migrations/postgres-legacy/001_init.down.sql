-- Migration 001 DOWN: drops all tables created in 001_init.up.sql.
-- References: SPEC.md §8.2

DROP INDEX IF EXISTS idx_budget_app;
DROP TABLE IF EXISTS budget_counters;

DROP INDEX IF EXISTS idx_audit_request;
DROP INDEX IF EXISTS idx_audit_event_type;
DROP INDEX IF EXISTS idx_audit_app_created;
DROP TABLE IF EXISTS audit_events;

DROP INDEX IF EXISTS idx_usage_request;
DROP INDEX IF EXISTS idx_usage_app_created;
DROP TABLE IF EXISTS usage_events;
