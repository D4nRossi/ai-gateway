-- Reverter 007: drop o filtered index, restaurar UNIQUE total em application_id.

IF EXISTS (SELECT 1 FROM sys.indexes WHERE name = 'idx_api_keys_active_per_app' AND object_id = OBJECT_ID('gogateway.api_keys'))
    DROP INDEX idx_api_keys_active_per_app ON gogateway.api_keys;

IF NOT EXISTS (
    SELECT 1 FROM sys.key_constraints
     WHERE parent_object_id = OBJECT_ID('gogateway.api_keys')
       AND name = 'uq_api_keys_application'
)
BEGIN
    ALTER TABLE gogateway.api_keys
        ADD CONSTRAINT uq_api_keys_application UNIQUE (application_id);
END;
