-- 006_endpoint_provider_config.down.sql
--
-- Reverter ADR-0017: remove a coluna provider_config. Após rollback, o
-- proxy plane volta ao passthrough total (path translation desativada).
-- Dados em provider_config são perdidos — não há campo backup.

ALTER TABLE proxy_endpoints
    DROP COLUMN provider_config;
