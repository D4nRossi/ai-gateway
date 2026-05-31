-- 007_api_keys_partial_unique.up.sql (T-SQL)
--
-- Portado de migrations/postgres-legacy/007_api_keys_partial_unique.up.sql.
-- Substitui a UNIQUE constraint sobre application_id (criada em 003) por um
-- filtered unique index WHERE rotated_at IS NULL. Permite múltiplas linhas
-- históricas por application_id (rotações passadas), mas garante uma única
-- chave ATIVA por vez — exatamente o que RotateAPIKey (mark-old-then-insert-new
-- na mesma transação) precisa.
--
-- Self-healing: o DROP CONSTRAINT é resolvido por sys.key_constraints porque
-- o nome do constraint pode variar entre instâncias se 003 foi aplicada em
-- contextos diferentes.

DECLARE @sql NVARCHAR(MAX);

SELECT @sql = COALESCE(@sql + N';', N'') + N'ALTER TABLE gogateway.api_keys DROP CONSTRAINT ' + QUOTENAME(kc.name)
  FROM sys.key_constraints kc
  JOIN sys.indexes ix
    ON ix.object_id = kc.parent_object_id
   AND ix.index_id  = kc.unique_index_id
  JOIN sys.index_columns ic
    ON ic.object_id = ix.object_id
   AND ic.index_id  = ix.index_id
  JOIN sys.columns c
    ON c.object_id = ic.object_id
   AND c.column_id = ic.column_id
 WHERE kc.parent_object_id = OBJECT_ID('gogateway.api_keys')
   AND kc.type = 'UQ'
   AND c.name = 'application_id'
   AND ic.index_column_id = 1
   AND NOT EXISTS (
       SELECT 1 FROM sys.index_columns ic2
        WHERE ic2.object_id = ix.object_id
          AND ic2.index_id  = ix.index_id
          AND ic2.index_column_id > 1
   );
IF @sql IS NOT NULL EXEC sp_executesql @sql;

IF NOT EXISTS (
    SELECT 1 FROM sys.indexes
     WHERE name = 'idx_api_keys_active_per_app'
       AND object_id = OBJECT_ID('gogateway.api_keys')
)
BEGIN
    CREATE UNIQUE INDEX idx_api_keys_active_per_app
        ON gogateway.api_keys(application_id)
        WHERE rotated_at IS NULL;
END;
