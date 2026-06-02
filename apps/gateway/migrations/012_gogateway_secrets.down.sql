-- Reverte 012: drop da tabela gogateway.secrets.
--
-- Atenção: este DROP é irreversível. Em ambiente com Always Encrypted
-- configurado, a CEK/CMK no banco e o cert no Windows Store NÃO são
-- removidos por esta migration — operador limpa via PowerShell se desejar.
--
-- Cenários de uso legítimo:
--   - Rollback durante o desenvolvimento da Onda
--   - Recriar a tabela com novo schema (migration nova)
-- NÃO usar em prod com secrets vivos sem backup prévio.

IF OBJECT_ID('gogateway.secrets', 'U') IS NOT NULL
BEGIN
    DROP TABLE gogateway.secrets;
END;
