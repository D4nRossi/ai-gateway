-- 008_usage_events_latency_breakdown.up.sql (T-SQL)
--
-- Portado de migrations/postgres-legacy/008_usage_events_latency_breakdown.up.sql.
-- ADR-0021: 5 colunas null-able para decomposição de latência por bucket.
-- Sem backfill — linhas antigas continuam NULL. Dashboards filtram
-- "WHERE lat_provider_ms IS NOT NULL" pra excluir histórico não-instrumentado.

IF COL_LENGTH('gogateway.usage_events', 'lat_auth_ms') IS NULL
BEGIN
    ALTER TABLE gogateway.usage_events
        ADD lat_auth_ms       INT NULL,
            lat_mask_ms       INT NULL,
            lat_guardrails_ms INT NULL,
            lat_provider_ms   INT NULL,
            lat_encode_ms     INT NULL;
END;
