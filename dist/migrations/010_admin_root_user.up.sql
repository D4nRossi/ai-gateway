-- 010_admin_root_user.up.sql (T-SQL)
--
-- Provisiona o admin "root" de bootstrap. Substitui o passo manual de rodar
-- `cmd/admin-create` toda vez que um banco novo precisa ser inicializado
-- (e.g. ambiente de homologação refeito do zero, dev local, novos deploys).
--
-- Senha temporária
-- ────────────────
-- Username: root
-- Senha:    Adm!nGogateway2026
-- Hash:     bcrypt cost=12 (consistente com adminservice.bcryptCost)
--
-- TROCAR IMEDIATAMENTE após o primeiro login: o hash desta senha está
-- versionado no Git, então qualquer um com acesso ao repo conhece a senha
-- inicial. O fluxo recomendado é:
--   1. Logar no Console como root + senha temporária
--   2. Criar seu próprio admin user pela UI (role=admin)
--   3. Trocar a senha do root pela UI (admin user edit) OU desativar
--      o root via UI (Active=false)
--
-- Quando a integração com Entra ID / SAML for implementada (ADR futura
-- — referência no roadmap.md §3.3 Segurança), o usuário local root pode
-- ser desativado permanentemente e removido em migration de cleanup.
--
-- Idempotência: o INSERT só executa se o username 'root' não existir,
-- então rodar a migration múltiplas vezes (ou em ambientes onde o root
-- já foi criado de outra forma) é seguro.
--
-- References:
--   - ADR-0011 — admin auth via bcrypt cost=12
--   - SPEC.md §15 — bootstrap and first-user provisioning

IF NOT EXISTS (
    SELECT 1 FROM gogateway.admin_users WHERE username = N'root'
)
BEGIN
    INSERT INTO gogateway.admin_users (username, password_hash, role, active)
    VALUES (
        N'root',
        N'$2a$12$Aa1yJ0EX2EdNSkPW3HX9W.WCb9AxYMH.idm1NOd/M.ig7OCgUEFJq',
        N'admin',
        1
    );

    -- Avisa o operador via log do SQL Server que o root foi criado e está
    -- com senha conhecida. Ver migration 009 pra o mesmo padrão de RAISERROR.
    RAISERROR(N'migration 010: admin user ''root'' provisioned with TEMPORARY password ''Adm!nGogateway2026''. CHANGE OR DEACTIVATE IMMEDIATELY after first login.', 0, 1) WITH NOWAIT;
END;
