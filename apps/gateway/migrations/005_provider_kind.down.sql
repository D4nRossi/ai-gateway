-- 005_provider_kind.down.sql
--
-- SQL Server exige que CHECK e DEFAULT constraints sejam dropadas explicitamente
-- antes do DROP COLUMN. Usamos sys.check_constraints/sys.default_constraints
-- pra descobrir os nomes (que podem variar de instalação para instalação se
-- a migration foi aplicada por outro caminho) e dropá-las.

IF EXISTS (SELECT 1 FROM sys.indexes WHERE name = 'idx_proxy_endpoints_provider_kind' AND object_id = OBJECT_ID('gogateway.proxy_endpoints'))
    DROP INDEX idx_proxy_endpoints_provider_kind ON gogateway.proxy_endpoints;

IF COL_LENGTH('gogateway.proxy_endpoints', 'provider_kind') IS NOT NULL
BEGIN
    DECLARE @sql NVARCHAR(MAX);

    SELECT @sql = COALESCE(@sql + N';', N'') + N'ALTER TABLE gogateway.proxy_endpoints DROP CONSTRAINT ' + QUOTENAME(cc.name)
      FROM sys.check_constraints cc
     WHERE cc.parent_object_id = OBJECT_ID('gogateway.proxy_endpoints')
       AND cc.name = 'ck_proxy_endpoints_provider_kind';
    IF @sql IS NOT NULL EXEC sp_executesql @sql;
    SET @sql = NULL;

    SELECT @sql = COALESCE(@sql + N';', N'') + N'ALTER TABLE gogateway.proxy_endpoints DROP CONSTRAINT ' + QUOTENAME(dc.name)
      FROM sys.default_constraints dc
     WHERE dc.parent_object_id = OBJECT_ID('gogateway.proxy_endpoints')
       AND dc.name = 'df_proxy_endpoints_provider_kind';
    IF @sql IS NOT NULL EXEC sp_executesql @sql;

    ALTER TABLE gogateway.proxy_endpoints DROP COLUMN provider_kind;
END;
