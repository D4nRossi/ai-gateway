IF OBJECT_ID('gogateway.budget_counters', 'U') IS NOT NULL DROP TABLE gogateway.budget_counters;
IF OBJECT_ID('gogateway.audit_events', 'U')    IS NOT NULL DROP TABLE gogateway.audit_events;
IF OBJECT_ID('gogateway.usage_events', 'U')    IS NOT NULL DROP TABLE gogateway.usage_events;
-- Nota: o schema gogateway é intencionalmente preservado aqui porque outras
-- migrations (002+) podem ter criado tabelas dentro dele. Para apagar o schema
-- inteiro, use DROP SCHEMA manualmente quando todas as migrations estiverem
-- desfeitas.
