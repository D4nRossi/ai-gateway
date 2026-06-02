-- Reverte 010: remove o admin user 'root'.
-- O ON DELETE CASCADE da FK admin_sessions.admin_user_id cuida das sessões
-- ativas dele (revogadas automaticamente).
--
-- Nota: este DELETE é irreversível. Se o root foi modificado (senha trocada,
-- desativado, renomeado), a migration .down NÃO restaura o estado anterior —
-- apenas remove o user com username = 'root'. Em ambientes onde o operador
-- trocou o username pra evitar conflito, esse DELETE não faz nada (idempotente).

DELETE FROM gogateway.admin_users WHERE username = N'root';
