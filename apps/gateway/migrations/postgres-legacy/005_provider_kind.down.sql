-- 005_provider_kind.down.sql — desfaz 005_provider_kind.up.sql

DROP INDEX IF EXISTS idx_proxy_endpoints_provider_kind;
ALTER TABLE proxy_endpoints DROP COLUMN IF EXISTS provider_kind;
