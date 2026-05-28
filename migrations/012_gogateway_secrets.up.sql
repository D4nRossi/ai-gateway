-- 012_gogateway_secrets.up.sql (T-SQL)
--
-- ADR-0026: tabela pra secrets locais quando Azure Key Vault não está
-- disponível (deploy Windows corporativo air-gap-parcial).
--
-- Esta migration cria APENAS a estrutura básica da tabela com `value` em
-- VARBINARY(MAX) PLAINTEXT. O passo de Always Encrypted (CREATE COLUMN
-- MASTER KEY + CREATE COLUMN ENCRYPTION KEY + ALTER COLUMN com
-- `ENCRYPTED WITH (...)`) é MANUAL e executado pelo operador via
-- PowerShell no setup do servidor, porque depende do thumbprint do
-- certificado importado em LocalMachine\My — esse thumbprint varia por
-- servidor e o golang-migrate não suporta SQL parametrizado.
--
-- O procedimento completo está em `docs/deploy/windows.md` (seção AE setup).
-- Em dev/homolog sem AE configurado, a tabela funciona como armazenamento
-- plaintext — adequado pra testes funcionais, NUNCA pra produção.
--
-- References:
--   - ADR-0026 — Always Encrypted + DPAPI híbrido pra secrets locais
--   - ADR-0018 — Key Vault provider (alternativa)
--   - ADR-0022 — golang-migrate driver sqlserver

IF OBJECT_ID('gogateway.secrets', 'U') IS NULL
BEGIN
    CREATE TABLE gogateway.secrets (
        name        NVARCHAR(127)   NOT NULL,
        -- VARBINARY pra suportar AE (que armazena bytes cifrados). Em dev
        -- sem AE, recebe UTF-8 do valor plaintext (mesmo path do consumer).
        value       VARBINARY(MAX)  NOT NULL,
        created_at  DATETIMEOFFSET  NOT NULL DEFAULT SYSUTCDATETIME(),
        updated_at  DATETIMEOFFSET  NOT NULL DEFAULT SYSUTCDATETIME(),
        CONSTRAINT pk_gogateway_secrets PRIMARY KEY (name)
    );
END;
