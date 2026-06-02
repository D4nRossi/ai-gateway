IF EXISTS (SELECT 1 FROM sys.indexes WHERE name = 'idx_api_keys_active_prefix' AND object_id = OBJECT_ID('gogateway.api_keys'))
    DROP INDEX idx_api_keys_active_prefix ON gogateway.api_keys;
-- A reversão NÃO restaura os rotated_at marcados durante 009.up — esse cleanup
-- é intencional e irreversível por design.
