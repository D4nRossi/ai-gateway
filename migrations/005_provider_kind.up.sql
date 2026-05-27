-- 005_provider_kind.up.sql (T-SQL)
--
-- Portado de migrations/postgres-legacy/005_provider_kind.up.sql (ADR-0016).
-- Tipa cada proxy_endpoint pelo provider conhecido; default 'custom' preserva
-- linhas existentes como passthrough genérico.

IF COL_LENGTH('gogateway.proxy_endpoints', 'provider_kind') IS NULL
BEGIN
    ALTER TABLE gogateway.proxy_endpoints
        ADD provider_kind NVARCHAR(40) NOT NULL
                CONSTRAINT df_proxy_endpoints_provider_kind DEFAULT 'custom'
                CONSTRAINT ck_proxy_endpoints_provider_kind CHECK (provider_kind IN (
                    'azure_openai', 'openai', 'anthropic', 'gemini',
                    'mistral', 'cohere', 'groq', 'together',
                    'ollama', 'vllm', 'custom'
                ));

    -- Filtered index: dashboards filtram por provider; 'custom' tipicamente é
    -- a maioria das linhas — excluí-lo do índice mantém seletividade alta.
    CREATE INDEX idx_proxy_endpoints_provider_kind
        ON gogateway.proxy_endpoints(provider_kind)
        WHERE provider_kind <> 'custom';
END;
