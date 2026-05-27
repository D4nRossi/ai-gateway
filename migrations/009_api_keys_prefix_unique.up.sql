-- 009_api_keys_prefix_unique.up.sql
--
-- Bug: deriveKeyPrefix (adminservice/service.go) truncated at 10 ASCII chars,
-- so applications with names sharing the first ~10 letters produced the
-- same key_prefix. Combined with the absence of UNIQUE on api_keys.key_prefix,
-- GetAPIKeyByPrefix returned a non-deterministic row when two active keys
-- shared a prefix, surfacing as intermittent "token mismatch" 401s.
--
-- Migration 007 only enforced one ACTIVE key per application_id (partial UNIQUE).
-- This migration adds the missing partial UNIQUE on key_prefix as defense in
-- depth, working together with keyPrefixMaxLen=24 in adminservice/service.go.
--
-- IMPORTANT: this migration assumes any pre-existing duplicate active prefixes
-- have already been cleared by the operator (deleting the colliding
-- applications via the admin UI before applying). If duplicates remain, the
-- CREATE UNIQUE INDEX below will fail with SQLSTATE 23505 and the gateway will
-- refuse to start — that is the intended fail-fast behavior, surfacing the
-- collision rather than hiding it.
--
-- Postgres partial unique reference:
--   https://www.postgresql.org/docs/17/indexes-partial.html

CREATE UNIQUE INDEX idx_api_keys_active_prefix
    ON api_keys (key_prefix)
    WHERE rotated_at IS NULL;

COMMENT ON INDEX idx_api_keys_active_prefix IS
    'Defense in depth: ensure key_prefix is unique among active rows. '
    'Combined with longer deriveKeyPrefix budget (24 chars in service.go), prevents '
    'silent prefix collisions that surface as "token mismatch" 401s on the proxy auth.';
