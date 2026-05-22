-- 005_provider_kind.up.sql
--
-- ADR-0016: tipa cada proxy_endpoint pelo provider conhecido. Endpoints
-- existentes recebem 'custom' (passthrough genérico — preserva o comportamento
-- atual). O CHECK constraint mantém os valores válidos sincronizados com o
-- enum domain/endpoint.ProviderKind.
--
-- Novos providers nesta fase: azure_openai, openai, anthropic, gemini,
-- mistral, cohere, groq, together, ollama, vllm.
-- 'custom' continua sendo o escape-hatch para qualquer HTTP API.

ALTER TABLE proxy_endpoints
    ADD COLUMN provider_kind VARCHAR(40) NOT NULL DEFAULT 'custom'
    CHECK (provider_kind IN (
        'azure_openai',
        'openai',
        'anthropic',
        'gemini',
        'mistral',
        'cohere',
        'groq',
        'together',
        'ollama',
        'vllm',
        'custom'
    ));

-- Índice parcial: queries do dashboard frequentemente filtram por provider
-- ("quantos endpoints Anthropic temos?"). Excluímos 'custom' do índice porque
-- não há ganho de seletividade (esperamos que seja a maioria das linhas hoje).
CREATE INDEX idx_proxy_endpoints_provider_kind
    ON proxy_endpoints (provider_kind)
    WHERE provider_kind <> 'custom';

COMMENT ON COLUMN proxy_endpoints.provider_kind IS
    'ADR-0016: tipo do provider (azure_openai, openai, anthropic, gemini, …). '
    'custom = passthrough HTTP genérico, sem metadata pré-configurada.';
