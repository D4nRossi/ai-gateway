-- Reverte 011: remove credential_storage_mode + kv_secret_name + index parcial.
--
-- Drop ordenado: index → constraints → colunas. SQL Server exige drop das
-- constraints (CHECK + DEFAULT) antes da coluna; DROP CONSTRAINT IF EXISTS
-- é SQL Server 2016+ (compatível com o ambiente corporativo — ADR-0022).
--
-- Targets em modo 'kv' ou 'both' perdem o link com o secret no KV — o
-- secret em si permanece intacto no vault, só a referência no DB some.

IF EXISTS (
    SELECT 1 FROM sys.indexes
     WHERE name = 'idx_proxy_targets_kv_secret_name'
       AND object_id = OBJECT_ID('gogateway.proxy_targets')
)
BEGIN
    DROP INDEX idx_proxy_targets_kv_secret_name ON gogateway.proxy_targets;
END;

ALTER TABLE gogateway.proxy_targets DROP CONSTRAINT IF EXISTS ck_proxy_targets_kv_secret_name_format;
ALTER TABLE gogateway.proxy_targets DROP CONSTRAINT IF EXISTS ck_proxy_targets_credential_storage_mode;
ALTER TABLE gogateway.proxy_targets DROP CONSTRAINT IF EXISTS df_proxy_targets_credential_storage_mode;

IF COL_LENGTH('gogateway.proxy_targets', 'kv_secret_name') IS NOT NULL
BEGIN
    ALTER TABLE gogateway.proxy_targets DROP COLUMN kv_secret_name;
END;

IF COL_LENGTH('gogateway.proxy_targets', 'credential_storage_mode') IS NOT NULL
BEGIN
    ALTER TABLE gogateway.proxy_targets DROP COLUMN credential_storage_mode;
END;
