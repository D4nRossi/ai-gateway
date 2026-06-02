-- 008_usage_events_latency_breakdown.down.sql
--
-- Reverter ADR-0021: remove as 5 colunas de breakdown de latência.
-- Dados nas colunas são perdidos — não há campo backup.

ALTER TABLE usage_events
    DROP COLUMN lat_auth_ms,
    DROP COLUMN lat_mask_ms,
    DROP COLUMN lat_guardrails_ms,
    DROP COLUMN lat_provider_ms,
    DROP COLUMN lat_encode_ms;
