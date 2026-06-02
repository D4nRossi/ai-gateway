-- 005_provider_kind.up.sql (T-SQL)
--
-- Portado de migrations/postgres-legacy/005_provider_kind.up.sql (ADR-0016).
-- Tipa cada proxy_endpoint pelo provider conhecido; default 'custom' preserva
-- linhas existentes como passthrough genérico.
--
-- T-SQL gotcha (2026-05-27): scripts diretos NÃO têm deferred name resolution
-- — o batch inteiro é parseado/name-resolved ANTES de executar. Combinar
-- `ALTER TABLE ADD column` com um `CREATE INDEX` que referencia essa coluna
-- no mesmo batch causa "Invalid column name". A fix é envolver o `CREATE INDEX`
-- em `EXEC(N'...')`: a string SQL só é parseada quando o EXEC roda, momento em
-- que a coluna já existe.

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
END;

-- Filtered index: dashboards filtram por provider; 'custom' tipicamente é
-- a maioria das linhas — excluí-lo do índice mantém seletividade alta.
-- EXEC() force-deferes a name resolution pra DEPOIS do ALTER TABLE acima.
IF NOT EXISTS (
    SELECT 1 FROM sys.indexes
     WHERE name = 'idx_proxy_endpoints_provider_kind'
       AND object_id = OBJECT_ID('gogateway.proxy_endpoints')
)
BEGIN
    EXEC(N'CREATE INDEX idx_proxy_endpoints_provider_kind
        ON gogateway.proxy_endpoints(provider_kind)
        WHERE provider_kind <> ''custom''');
END;
