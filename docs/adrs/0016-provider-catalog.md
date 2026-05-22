# ADR-0016: Provider Catalog — endpoints tipados por provider

- **Status**: accepted
- **Date**: 2026-05-22
- **Decision makers**: Daniel (owner)
- **Consulted**: Claude Sonnet 4.6

## Context

A versão inicial do plano de proxy genérico (ADR-0010) trata todo upstream
como "qualquer HTTP API": o operador cadastra slug + URL + auth method
livre. Funciona — qualquer endpoint Azure OpenAI, Gemini, Anthropic, vLLM
local ou serviço interno passa pelo mesmo motor — mas exige que quem cadastra
conheça de cabeça:

- URL base do provider
- Mecanismo de autenticação (Bearer? `api-key` header? query string?)
- Quais cabeçalhos extras o provider exige

Plataformas reais (LiteLLM, Cloudflare AI Gateway, Portkey, Kong AI Gateway)
resolvem isso com um **catálogo de providers conhecidos**: o usuário escolhe
"Azure OpenAI" / "Anthropic" / "Gemini" e a UI preenche os defaults razoáveis,
deixando o usuário só com a parte que muda (URL específica, secret).

Sem esse catálogo, a UX fica seca e propensa a erro — cada provider tem suas
peculiaridades (Gemini usa `?key=` em query string, Anthropic precisa de
`anthropic-version`, etc.).

## Decision

Adicionar uma coluna **`provider_kind`** em `proxy_endpoints` com um conjunto
enumerado de tipos suportados. No frontend, manter um **catálogo TypeScript**
com metadata (label, URL base sugerida, tipo de auth padrão, ícone, link de
documentação) usada para popular defaults nos formulários.

**Metadata-only nesta fase**: o backend valida que `provider_kind` é um valor
conhecido, persiste no DB e expõe na API — **mas não muda o comportamento do
motor de proxy**. O motor continua sendo passthrough: o que o cliente manda
chega ao upstream verbatim, com a auth do target injetada. Transformações de
payload (cliente fala OpenAI, gateway traduz pra Gemini) ficam para um ADR
futuro.

## Options considered

### Option 1: Manter passthrough genérico, sem `provider_kind`
- **Pros:**
  - Zero código novo no backend
  - Já funciona para qualquer API HTTP
- **Cons:**
  - UX seca: cada cadastro exige o operador colar URL + saber qual header de auth
  - Sem espaço para evolução (validações por provider, transformações futuras,
    UI rica por tipo)
- **Por que não:** o atrito é real e o user explicitamente pediu para resolver.

### Option 2: Transformações completas no backend (LiteLLM-style)
Cliente sempre chama `/v1/chat/completions` no formato OpenAI; gateway traduz
para cada provider.
- **Pros:** UX máxima para o consumer (uma única API).
- **Cons:**
  - Implementação grande: mapping de payloads, streaming format diferente por
    provider, tratamento de erros, cobertura de features (function calling,
    tools, vision) varia entre providers
  - Hoje o gateway tem `/v1/chat/completions` específico do Azure (não generaliza
    facilmente sem refatorar tudo)
  - Risco de quebrar features-específicas-de-provider que clientes querem usar
- **Por que não agora:** muito grande para entregar antes do stress test.
  Vira ADR-0019 ou similar.

### Option 3 (chosen): `provider_kind` como metadata, catálogo no frontend
- **Pros:**
  - Resolve a ergonomia (selector visual + defaults) imediatamente
  - Mantém passthrough total — funciona para qualquer provider, incluindo os que
    não estão no catálogo (via `custom`)
  - Catálogo no frontend é fácil de estender (adicionar provider = 5 linhas em
    `web/src/lib/providers.ts`)
  - Backend simples: enum + validação no `Create`/`Update`
  - Deixa o caminho aberto para Option 2 quando fizer sentido — basta o motor
    inspecionar `provider_kind` no roteamento futuro
- **Cons:**
  - Cliente ainda precisa falar a "língua" do provider escolhido (URL/payload)
  - Mudança de schema requer migration
- **Why:** equilíbrio entre custo e benefício imediato. O `provider_kind` é o
  *hook* arquitetural que destrava todo o resto.

## Consequences

### Positive
- UI mostra cards com logos/labels familiares — operadora reconhece "Azure",
  "Gemini" etc. sem decorar URL
- Selecionar provider preenche URL base + auth method do target — menos
  digitação, menos erro
- Listagem ganha badge informativo (`Anthropic`, `Gemini`, `Custom`)
- DB ganha visibilidade analítica: "quantos endpoints usam Gemini? Quantos
  são custom?"

### Negative / Trade-offs
- Migração: tabela existente precisa de `ALTER TABLE` (default 'custom' para
  endpoints já cadastrados, preservando comportamento)
- Catálogo precisa ser mantido conforme novos providers aparecem (~5 LOC
  cada)
- Selecionar provider errado (ex: "OpenAI" para upstream Azure) é responsabilidade
  do operador — o backend não detecta inconsistência. UI pode validar URL
  pattern num lote futuro

### Mitigations
- `custom` cobre 100% dos casos não previstos no catálogo — nada fica fora
- Catálogo bem documentado em `web/src/lib/providers.ts` com docs link por
  provider
- Backend valida que `provider_kind` ∈ enum conhecido (CHECK constraint +
  validação no service)

## Provider catalog inicial (10 + custom)

| Kind | Base URL | Auth method | Doc |
|---|---|---|---|
| `azure_openai` | `https://{resource}.cognitiveservices.azure.com` | `api-key` header | [Azure OpenAI](https://learn.microsoft.com/azure/ai-services/openai/) |
| `openai` | `https://api.openai.com/v1` | Bearer | [OpenAI](https://platform.openai.com/docs) |
| `anthropic` | `https://api.anthropic.com/v1` | `x-api-key` header | [Anthropic](https://docs.anthropic.com) |
| `gemini` | `https://generativelanguage.googleapis.com/v1beta` | query `?key=` | [Gemini](https://ai.google.dev/) |
| `mistral` | `https://api.mistral.ai/v1` | Bearer | [Mistral](https://docs.mistral.ai/) |
| `cohere` | `https://api.cohere.com/v1` | Bearer | [Cohere](https://docs.cohere.com/) |
| `groq` | `https://api.groq.com/openai/v1` | Bearer | [Groq](https://console.groq.com/docs) |
| `together` | `https://api.together.xyz/v1` | Bearer | [Together](https://docs.together.ai/) |
| `ollama` | `http://localhost:11434` | nenhuma | [Ollama](https://github.com/ollama/ollama) |
| `vllm` | `http://host:8000/v1` | Bearer/nenhuma | [vLLM](https://docs.vllm.ai/) |
| `custom` | livre | livre | passthrough |

Não inclusos nesta fase (auth complexa, ficam para Lote H):
- AWS Bedrock (Sig v4)
- Google Vertex AI (OAuth/service account)
- Azure AD-authenticated endpoints

## References

- ADR-0010 — generic HTTP proxy engine (motor que continua intocado)
- ADR-0013 — load balancing strategies
- LiteLLM provider model: https://docs.litellm.ai/docs/providers
- Cloudflare AI Gateway connectors: https://developers.cloudflare.com/ai-gateway/providers/
- Portkey virtual keys: https://portkey.ai/docs/product/ai-gateway/virtual-keys
