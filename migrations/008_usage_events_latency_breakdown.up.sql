-- 008_usage_events_latency_breakdown.up.sql
--
-- ADR-0021: instrumentação por bucket no chat handler grava o tempo gasto
-- em cada etapa. Adiciona 5 colunas null-able pra dashboards e queries
-- ad-hoc decomponham o latency_ms total.
--
-- Sem backfill: linhas antigas continuam NULL. Dashboards filtram
-- "WHERE lat_provider_ms IS NOT NULL" pra excluir histórico não-instrumentado.

ALTER TABLE usage_events
    ADD COLUMN lat_auth_ms       INTEGER NULL,
    ADD COLUMN lat_mask_ms       INTEGER NULL,
    ADD COLUMN lat_guardrails_ms INTEGER NULL,
    ADD COLUMN lat_provider_ms   INTEGER NULL,
    ADD COLUMN lat_encode_ms     INTEGER NULL;

COMMENT ON COLUMN usage_events.lat_auth_ms IS
    'ADR-0021 bucket auth: bearer extract + prefix lookup + hash compare + policy fetch.';
COMMENT ON COLUMN usage_events.lat_mask_ms IS
    'ADR-0021 bucket mask: regex local PII + Azure AI Language (Tier 2/3).';
COMMENT ON COLUMN usage_events.lat_guardrails_ms IS
    'ADR-0021 bucket guardrails: local injection + Azure Prompt Shield + Content Safety.';
COMMENT ON COLUMN usage_events.lat_provider_ms IS
    'ADR-0021 bucket provider: chamada ao LLM upstream (Azure OpenAI).';
COMMENT ON COLUMN usage_events.lat_encode_ms IS
    'ADR-0021 bucket encode: JSON marshal + Write da response ao cliente.';
