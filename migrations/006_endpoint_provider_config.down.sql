IF COL_LENGTH('gogateway.proxy_endpoints', 'provider_config') IS NOT NULL
BEGIN
    DECLARE @sql NVARCHAR(MAX);

    SELECT @sql = COALESCE(@sql + N';', N'') + N'ALTER TABLE gogateway.proxy_endpoints DROP CONSTRAINT ' + QUOTENAME(cc.name)
      FROM sys.check_constraints cc
     WHERE cc.parent_object_id = OBJECT_ID('gogateway.proxy_endpoints')
       AND cc.name = 'ck_proxy_endpoints_provider_config';
    IF @sql IS NOT NULL EXEC sp_executesql @sql;
    SET @sql = NULL;

    SELECT @sql = COALESCE(@sql + N';', N'') + N'ALTER TABLE gogateway.proxy_endpoints DROP CONSTRAINT ' + QUOTENAME(dc.name)
      FROM sys.default_constraints dc
     WHERE dc.parent_object_id = OBJECT_ID('gogateway.proxy_endpoints')
       AND dc.name = 'df_proxy_endpoints_provider_config';
    IF @sql IS NOT NULL EXEC sp_executesql @sql;

    ALTER TABLE gogateway.proxy_endpoints DROP COLUMN provider_config;
END;
