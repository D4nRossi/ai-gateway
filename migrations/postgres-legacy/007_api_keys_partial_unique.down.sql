-- 007_api_keys_partial_unique.down.sql
--
-- Reverter para o esquema original da migration 003 (UNIQUE total em
-- application_id). Atenção: este rollback FALHA se existirem linhas
-- rotacionadas no histórico (rotated_at IS NOT NULL) para a mesma
-- application_id — porque a UNIQUE total não admite duplicatas.
--
-- Para rollback seguro em ambiente com histórico, limpe primeiro:
--   DELETE FROM api_keys WHERE rotated_at IS NOT NULL;

DROP INDEX IF EXISTS idx_api_keys_active_per_app;

ALTER TABLE api_keys
    ADD CONSTRAINT api_keys_application_id_key UNIQUE (application_id);
