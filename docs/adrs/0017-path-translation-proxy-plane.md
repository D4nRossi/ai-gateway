# ADR-0017: Path translation no proxy plane

- **Status**: accepted
- **Date**: 2026-05-26
- **Decision makers**: Daniel (owner)
- **Consulted**: Claude Opus 4.7

## Context

O ADR-0016 introduziu `provider_kind` em `proxy_endpoints` como **metadata
only** — o motor de proxy (ADR-0010) continuou sendo passthrough total. O
cliente que chama `/v1/proxy/{slug}/*` precisa colar **o path completo do
upstream**, incluindo todas as idiossincrasias da API:

```
POST /v1/proxy/azure-foundry/openai/deployments/gpt-4.1/chat/completions?api-version=2025-01-01-preview
```

Para um endpoint Azure OpenAI, isso significa que o consumidor precisa saber:

1. Que Azure expõe o path sob `/openai/deployments/{deployment_name}/...`
2. O nome exato do deployment (não do modelo — pode ser igual mas não precisa
   ser)
3. A `api-version` em uso (que muda a cada release Microsoft)
4. Qual operação (chat/completions, completions, embeddings) sob esse mesmo
   prefixo

Esse acoplamento quebra o objetivo do gateway: a aplicação cliente deveria
**ignorar** a forma do upstream. Ela tem um token, um endpoint slug, um
modelo lógico — o resto é problema do gateway.

Esse ADR formaliza o próximo passo da ADR-0016: ativar `provider_kind` para
**traduzir caminhos**, mantendo o motor de proxy intacto.

## Decision

Introduzir um **path translator por `provider_kind`** que roda como hook
dentro do `Rewrite` do `httputil.ReverseProxy` (`internal/proxy/director.go`).
Cada translator é responsável por consumir um path canônico OpenAI-style
recebido do cliente e devolver o path nativo do upstream.

**Contrato cliente após este ADR:**

```
POST /v1/proxy/{slug}/chat/completions
Authorization: Bearer gwk_...
Content-Type: application/json

{"model": "gpt-4.1", "messages": [...]}
```

Para um endpoint com `provider_kind = azure_openai`, o gateway traduz para:

```
POST {target.URL}/openai/deployments/{deployment}/chat/completions?api-version={api_version}
api-key: {target.Auth.Value}
```

A tradução resolve `deployment` a partir do mapping configurado por endpoint
(`model_to_deployment`) e injeta `api_version` da config do endpoint.

Para `provider_kind = custom` o translator é **no-op** — passthrough total
preservado (compatibilidade com endpoints genéricos não-IA, alinhado com a
visão "gateway escalável além de IA" do roadmap).

### Configuração por endpoint

Nova coluna **`provider_config JSONB DEFAULT '{}'`** em `proxy_endpoints`
(migration 006). Para `provider_kind = azure_openai` o JSON tem o shape:

```json
{
  "api_version": "2025-01-01-preview",
  "model_to_deployment": {
    "gpt-4.1": "gpt-4.1",
    "gpt-4.1-mini": "gpt-4.1-mini"
  }
}
```

A `adminservice` valida o shape obrigatório por kind no `CreateEndpoint` /
`UpdateEndpoint`. Endpoint Azure sem `api_version` ou com `model_to_deployment`
vazio falha o create com `ErrInvalidProviderConfig`.

### Erro de modelo desconhecido

Quando o cliente envia um `model` que não está no mapping do endpoint, o
gateway retorna **400** com a lista de modelos disponíveis (não 404 — o
endpoint existe, o cliente é que pediu um modelo não configurado):

```json
{
  "error": {
    "code": "unknown_model",
    "message": "model 'gpt-5' is not configured for endpoint 'azure-foundry'. Available: gpt-4.1, gpt-4.1-mini"
  }
}
```

Decisão deliberada de **não fazer passthrough silencioso** (usar o `model`
como `deployment_name` direto) — esquisitices de "às vezes o nome coincide"
quebram debugging quando não coincidem.

## Options considered

### Option 1: Passthrough completo + transformação só do header `api-version`
Cliente cola `/openai/deployments/gpt-4.1/chat/completions`; gateway só anexa
`?api-version=...` se faltar.
- **Pros:** mudança mínima, zero impacto em provider_config
- **Cons:**
  - Cliente continua precisando saber a estrutura Azure (`/openai/deployments/...`)
  - Não destrava clientes OpenAI-style padrão (LangChain, llamaindex, sdk
    `openai` python) que mandam `/chat/completions` puro
  - Resolve só 1% do atrito real

### Option 2: Tradução completa de payload + path (LiteLLM-style)
Gateway aceita request OpenAI; converte para Anthropic, Gemini, Azure conforme
provider_kind. Cobre payload, headers, streaming format, erros.
- **Pros:**
  - UX máxima — cliente só fala OpenAI, escolhe o slug
  - Vendor-lock-out total (troca de Azure→Gemini é trocar slug)
- **Cons:**
  - Escopo enorme: mapping de payloads (tools, vision, function calling),
    formato SSE diferente por provider, tratamento de erro próprio de cada
    upstream
  - Features-específicas-de-provider ficam inacessíveis (cliente pediu
    `prompt_filter_results` Azure → como mapear pra Anthropic?)
  - Manutenção pesada: cada release de cada provider potencialmente quebra
    a tradução
- **Por que não agora:** ADR-0016 já antecipou essa decisão — fica como ADR
  futuro depois que tivermos uso real e demanda comprovada. Translation só de
  path entrega 80% do valor com 5% do custo.

### Option 3 (chosen): Path translator por kind, payload passthrough
- **Pros:**
  - Cliente fala dialecto OpenAI-style canônico (`/chat/completions`,
    `/embeddings`, `/models`) para qualquer provider que mapeie pra isso
    (Azure OpenAI, Groq, Together, vLLM, qualquer OpenAI-compat)
  - Body do request continua passthrough — o cliente decide se quer usar
    features específicas do provider, sem o gateway no caminho
  - Para `custom`, comportamento idêntico ao de hoje (passthrough)
  - Migration: 1 coluna JSONB, defaults seguros
  - Translator é registry simples: 1 implementação por kind, plugável
- **Cons:**
  - Endpoints Azure pré-existentes precisam ser editados depois da migração
    para preencher `provider_config` (validação obrigatória). Mitigação:
    documentar no CHANGELOG e na UI mostrar warning visível
  - `api_version` por endpoint duplica config quando há múltiplos endpoints
    pra mesma Azure resource. Aceitável — multi-version é caso real
  - JSONB sem schema enforcement no DB. Validação fica no app layer
    (`adminservice`) — aceitável e padrão Go
- **Why:** equilíbrio entre destravar a UX prometida pela ADR-0016 sem
  abraçar a complexidade de tradução de payload. Path translation cobre
  Azure (caso atual) e abre caminho pra OpenAI/Groq/Together/vLLM no futuro
  (todos OpenAI-compat) com a mesma mecânica.

### Option 4: Tabela `endpoint_models` separada com FK
Schema relacional puro (1 row por modelo configurado).
- **Pros:** queries SQL trivialmente indexáveis, listagem natural
- **Cons:** 1 tabela nova só pra Azure agora; outros providers (Anthropic,
  Gemini) podem precisar de campos diferentes (`anthropic_version`,
  `safety_settings`) que não cabem na mesma tabela
- **Por que não:** JSONB cobre os dois mundos com menos schema. Pode ser
  revisitado quando houver 3+ providers com config complexa

## Consequences

### Positive
- Cliente OpenAI-compat (LangChain, sdk `openai`, llamaindex, qualquer
  ferramenta de comunidade) funciona out-of-the-box contra endpoint Azure
  cadastrado no gateway
- Mudança de `api_version` é um update via UI, sem deploy de cliente
- Mudança de `deployment_name` no Azure é um update via UI, sem deploy de
  cliente (deployments renomeados, blue-green entre versões de modelo)
- Endpoints `custom` mantêm passthrough — compromisso da visão genérica do
  proxy plane
- Hook plugável: implementação de translator para OpenAI nativo, Groq, vLLM
  vira ~30 LOC cada (mesma família OpenAI-compat)

### Negative / Trade-offs
- Endpoints Azure cadastrados antes da migration ficam quebrados até
  serem editados (sem `provider_config`, validação obrigatória rejeita)
- JSONB válido mas semanticamente inválido (ex: `model_to_deployment: {}`)
  só pega em runtime — validação fail-fast no admin layer mitiga
- Translator é layer extra: bug aqui afeta TODOS os requests do kind. Boa
  cobertura de testes é não-negociável

### Mitigations
- Migration 006 não modifica dados existentes — só adiciona coluna com
  default `'{}'`. Roll-out: deploy → editar endpoints existentes no UI →
  validar via playground → comunicar
- Mensagens de erro do translator são explícitas (`unknown_model`,
  `endpoint_misconfigured`) para reduzir tempo de diagnóstico
- Testes de translator são table-driven cobrindo: model presente, model
  ausente, mapping vazio, api_version vazio, body com campos extras
  preservados

## Consequências para a UI (Onda 5)

- Form de endpoint Azure ganha 2 campos novos: `api_version` (text) e
  uma tabela editável `model_to_deployment` (1 row por modelo, ambos editáveis)
- Listagem de modelos do endpoint (pra autocomplete no playground)
- Playground reformulado: quando endpoint selecionado é Azure, esconde
  campo "path" e mostra dropdown "model" populado do `model_to_deployment`,
  body é OpenAI-style pré-preenchido
- Endpoint `custom` continua com campo "path" livre (passthrough)

## References

- ADR-0010 — generic HTTP proxy engine (motor que permanece intocado)
- ADR-0012 — AES-256-GCM at rest (credenciais de target)
- ADR-0016 — Provider Catalog (a metadata que este ADR ativa)
- Azure OpenAI REST reference: https://learn.microsoft.com/azure/ai-services/openai/reference
- OpenAI API reference (formato canônico do cliente): https://platform.openai.com/docs/api-reference/chat
- LiteLLM provider config (referência de design): https://docs.litellm.ai/docs/proxy/configs
