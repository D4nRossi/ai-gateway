-- 011_proxy_targets_kv_credential_mode.up.sql (T-SQL)
--
-- ADR-0020: armazenamento opcional de credenciais de proxy_targets no Azure
-- Key Vault, com modo por target.
--
-- Modes (credential_storage_mode):
--   'aes'  (default) — credencial em auth_config_enc apenas. Status quo.
--   'kv'             — credencial em KV apenas (kv_secret_name preenchido).
--   'both'           — KV é fonte de verdade; auth_config_enc é cache cifrado
--                      de fallback (timeout 200ms no read pro KV).
--
-- Targets existentes ficam todos em 'aes' por causa do DEFAULT — zero
-- migração compulsória. CLI cmd/migrate-targets-to-kv promove target a
-- target quando o KV estiver pronto.
--
-- References:
--   - ADR-0020 — modo de armazenamento de credenciais por target
--   - ADR-0012 — AES-256-GCM para credenciais at-rest (mantida)
--   - ADR-0018 — KV resolver com cache 5min (reusado em runtime)

IF COL_LENGTH('gogateway.proxy_targets', 'credential_storage_mode') IS NULL
BEGIN
    ALTER TABLE gogateway.proxy_targets
        ADD credential_storage_mode NVARCHAR(10) NOT NULL
                CONSTRAINT df_proxy_targets_credential_storage_mode DEFAULT N'aes'
                CONSTRAINT ck_proxy_targets_credential_storage_mode
                    CHECK (credential_storage_mode IN (N'aes', N'kv', N'both')),
            kv_secret_name NVARCHAR(127) NULL
                CONSTRAINT ck_proxy_targets_kv_secret_name_format
                    CHECK (kv_secret_name IS NULL OR LEN(kv_secret_name) BETWEEN 1 AND 127);
END;

-- Index parcial: lookup operacional ("quais targets usam o secret X?").
-- Filtered porque a maioria dos rows tem kv_secret_name NULL (default 'aes').
IF NOT EXISTS (
    SELECT 1 FROM sys.indexes
     WHERE name = 'idx_proxy_targets_kv_secret_name'
       AND object_id = OBJECT_ID('gogateway.proxy_targets')
)
BEGIN
    CREATE INDEX idx_proxy_targets_kv_secret_name
        ON gogateway.proxy_targets(kv_secret_name)
        WHERE kv_secret_name IS NOT NULL;
END;
