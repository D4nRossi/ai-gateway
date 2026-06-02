-- 009_api_keys_prefix_unique.up.sql (T-SQL)
--
-- Portado de migrations/postgres-legacy/009_api_keys_prefix_unique.up.sql.
-- ADR-0022 + correção do bug "token mismatch" em apps com nome similar:
-- adiciona filtered UNIQUE em key_prefix, com self-heal de duplicatas
-- pré-existentes (marcadas como rotated_at).
--
-- A apuração das duplicatas usa ROW_NUMBER() OVER (PARTITION BY key_prefix
-- ORDER BY id DESC) — mantém a key mais recente ativa e invalida as anteriores.
-- A application que perde sua key precisa rotacionar via UI; com o novo
-- keyPrefixMaxLen=24 em adminservice/service.go, a rotação produz um prefix
-- que não colide.
--
-- RAISERROR WITH NOWAIT emite a mensagem direto no log do SQL Server (visível
-- via SQL Server Management Studio "Messages" ou no log do gateway via
-- pgx/mssql driver — substituindo o RAISE NOTICE do PL/pgSQL).

;WITH ranked AS (
    SELECT id,
           key_prefix,
           ROW_NUMBER() OVER (PARTITION BY key_prefix ORDER BY id DESC) AS rn
      FROM gogateway.api_keys
     WHERE rotated_at IS NULL
)
UPDATE gogateway.api_keys
   SET rotated_at = SYSUTCDATETIME()
 WHERE id IN (SELECT id FROM ranked WHERE rn > 1);

DECLARE @cleaned INT = @@ROWCOUNT;
IF @cleaned > 0
    RAISERROR(N'migration 009: %d duplicate api_keys row(s) marked as rotated. Affected applications must rotate their key in the admin UI.', 0, 1, @cleaned) WITH NOWAIT;
ELSE
    RAISERROR(N'migration 009: no duplicate api_keys rows found, schema was already clean', 0, 1) WITH NOWAIT;

IF NOT EXISTS (
    SELECT 1 FROM sys.indexes
     WHERE name = 'idx_api_keys_active_prefix'
       AND object_id = OBJECT_ID('gogateway.api_keys')
)
BEGIN
    CREATE UNIQUE INDEX idx_api_keys_active_prefix
        ON gogateway.api_keys(key_prefix)
        WHERE rotated_at IS NULL;
END;
