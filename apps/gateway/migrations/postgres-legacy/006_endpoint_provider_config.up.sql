-- 006_endpoint_provider_config.up.sql
--
-- ADR-0017: ativa path translation por provider_kind. Endpoints precisam de
-- config específica por kind (api_version + model_to_deployment no caso Azure
-- OpenAI). Usamos JSONB para que cada provider possa carregar seu próprio
-- shape sem migration nova a cada um.
--
-- Validação semântica (campos obrigatórios por kind) vive em adminservice —
-- o DB só garante que é JSON válido. Default '{}' preserva linhas existentes;
-- endpoints Azure cadastrados antes desta migration ficam "misconfigured" até
-- serem editados via UI/API e preencherem o shape esperado.

ALTER TABLE proxy_endpoints
    ADD COLUMN provider_config JSONB NOT NULL DEFAULT '{}';

COMMENT ON COLUMN proxy_endpoints.provider_config IS
    'ADR-0017: configuração específica por provider_kind. '
    'Azure OpenAI: { "api_version": "...", "model_to_deployment": { "model": "deployment" } }. '
    'custom: ignorado (passthrough). Validação por kind em internal/app/adminservice.';
