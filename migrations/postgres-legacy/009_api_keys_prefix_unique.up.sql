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
-- Self-healing on apply
-- ─────────────────────
-- If pre-existing duplicate active prefixes exist (legacy data created under
-- the old keyPrefixMaxLen=10 budget), this migration cleans them up by
-- marking the OLDER duplicate rows as rotated_at=NOW(), keeping only the most
-- recently created key active per prefix. The application that loses its
-- active key here can recover by rotating the key through the admin UI — the
-- new RotateAPIKey path re-derives the prefix with keyPrefixMaxLen=24, which
-- avoids the collision.
--
-- Rationale: the original fail-fast design assumed the operator would clear
-- duplicates manually before restart. In practice, a restart-bricked gateway
-- prevents access to the admin UI, creating a deadlock. Self-heal trades a
-- silent (but logged) state mutation for guaranteed boot recoverability.
-- All steps run inside the implicit migration transaction, so a failure in
-- CREATE UNIQUE INDEX still rolls back the cleanup.
--
-- Postgres partial unique reference:
--   https://www.postgresql.org/docs/17/indexes-partial.html

-- Step 1: detect duplicate active prefixes, mark older rows as rotated, and
-- emit a server NOTICE that appears in the Postgres log so the operator can
-- see exactly what was cleaned. ROW_NUMBER() partitions by key_prefix, ordering
-- by id DESC so the newest (highest id) wins. Anything with rn > 1 is a
-- duplicate older row.
DO $$
DECLARE
    cleaned_count INTEGER := 0;
    cleaned_prefixes TEXT := '(none)';
BEGIN
    WITH ranked AS (
        SELECT id,
               key_prefix,
               ROW_NUMBER() OVER (PARTITION BY key_prefix ORDER BY id DESC) AS rn
        FROM api_keys
        WHERE rotated_at IS NULL
    ),
    to_mark AS (
        SELECT id, key_prefix FROM ranked WHERE rn > 1
    ),
    marked AS (
        UPDATE api_keys
        SET rotated_at = NOW()
        WHERE id IN (SELECT id FROM to_mark)
        RETURNING key_prefix
    )
    SELECT COUNT(*),
           COALESCE(STRING_AGG(DISTINCT key_prefix, ', '), '(none)')
      INTO cleaned_count, cleaned_prefixes
      FROM marked;

    IF cleaned_count > 0 THEN
        RAISE NOTICE 'migration 009: % duplicate api_keys row(s) marked as rotated (prefixes: %). Affected applications must rotate their key in the admin UI to recover.',
            cleaned_count, cleaned_prefixes;
    ELSE
        RAISE NOTICE 'migration 009: no duplicate api_keys rows found, schema was already clean';
    END IF;
END $$;

-- Step 2: now that duplicates are quarantined, enforce uniqueness going forward.
CREATE UNIQUE INDEX idx_api_keys_active_prefix
    ON api_keys (key_prefix)
    WHERE rotated_at IS NULL;

COMMENT ON INDEX idx_api_keys_active_prefix IS
    'Defense in depth: ensure key_prefix is unique among active rows. '
    'Combined with longer deriveKeyPrefix budget (24 chars in service.go), prevents '
    'silent prefix collisions that surface as "token mismatch" 401s on the proxy auth.';
