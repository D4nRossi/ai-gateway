-- 007_api_keys_partial_unique.up.sql
--
-- Fix: o schema original (migration 003) declarou api_keys.application_id
-- como UNIQUE, mas o adminservice.RotateAPIKey faz "mark old as rotated +
-- INSERT new" dentro de uma transação. Como UNIQUE não filtra por
-- rotated_at, o INSERT da nova chave sempre violava a constraint e a
-- rotação falhava silenciosamente com 500.
--
-- Solução: trocar UNIQUE total por índice UNIQUE *parcial*
-- WHERE rotated_at IS NULL. Permite múltiplas linhas com mesma
-- application_id (histórico de rotações), mas garante que só uma esteja
-- ativa por vez — exatamente o que o RotateAPIKey assumia.
--
-- Postgres parcial unique reference:
--   https://www.postgresql.org/docs/17/indexes-partial.html
--
-- Para descobrir o nome real da constraint UNIQUE (varia entre installs
-- que rodaram migration 003 em momentos diferentes), descobrimos via
-- pg_constraint e dropamos por nome.

DO $$
DECLARE
    cname text;
BEGIN
    SELECT conname INTO cname
    FROM pg_constraint
    WHERE conrelid = 'api_keys'::regclass
      AND contype  = 'u'
      AND array_length(conkey, 1) = 1
      AND conkey[1] = (
          SELECT attnum FROM pg_attribute
          WHERE attrelid = 'api_keys'::regclass AND attname = 'application_id'
      )
    LIMIT 1;

    IF cname IS NOT NULL THEN
        EXECUTE format('ALTER TABLE api_keys DROP CONSTRAINT %I', cname);
    END IF;
END $$;

CREATE UNIQUE INDEX idx_api_keys_active_per_app
    ON api_keys (application_id)
    WHERE rotated_at IS NULL;

COMMENT ON INDEX idx_api_keys_active_per_app IS
    'Garante uma única chave ativa (rotated_at IS NULL) por aplicação. '
    'Linhas com rotated_at preenchido são histórico e ficam fora do índice — '
    'permite RotateAPIKey fazer mark-old + insert-new na mesma transação.';
