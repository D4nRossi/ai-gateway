-- 006_endpoint_provider_config.up.sql (T-SQL)
--
-- Portado de migrations/postgres-legacy/006_endpoint_provider_config.up.sql (ADR-0017).
-- Adiciona provider_config NVARCHAR(MAX) (era JSONB no PG). Default '{}'
-- preserva linhas existentes; validação semântica por provider_kind vive em
-- adminservice (não no DB).

IF COL_LENGTH('gogateway.proxy_endpoints', 'provider_config') IS NULL
BEGIN
    ALTER TABLE gogateway.proxy_endpoints
        ADD provider_config NVARCHAR(MAX) NOT NULL
                CONSTRAINT df_proxy_endpoints_provider_config DEFAULT N'{}'
                CONSTRAINT ck_proxy_endpoints_provider_config CHECK (ISJSON(provider_config) = 1);
END;
