IF COL_LENGTH('gogateway.usage_events', 'lat_auth_ms') IS NOT NULL
BEGIN
    ALTER TABLE gogateway.usage_events
        DROP COLUMN lat_auth_ms, lat_mask_ms, lat_guardrails_ms, lat_provider_ms, lat_encode_ms;
END;
