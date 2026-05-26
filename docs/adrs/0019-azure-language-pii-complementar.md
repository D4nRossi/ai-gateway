# ADR-0019: Azure Language PII como camada complementar ao regex local

- **Status**: accepted
- **Date**: 2026-05-26
- **Decision makers**: Daniel (owner)
- **Consulted**: Claude Opus 4.7

## Context

O gateway hoje (`internal/security/masking/`) faz detecção de PII via **regex
local** — CPF (mod-11), CNPJ (mod-11), cartão (Luhn), e-mail, telefone BR e
CEP. Cobre os casos mais comuns com latência sub-ms.

O que regex **não pega** (e nunca vai pegar com boa precisão):

- **Nomes próprios** (`João Silva`, `Maria das Graças`) — não tem padrão
- **Endereços completos** (`Rua Tal, 123 — Bairro Tal — Cidade Tal`)
- **Datas de nascimento** em formato livre (`nasci em 14 de março de 1989`)
- **Identificadores estrangeiros** (`SSN`, `IBAN`, etc. — relevante se o
  gateway receber prompts multilíngues)
- **Variações criativas** (`cinco-dois-nove-pontos-nove-oito-dois...` —
  alguém soletrando um CPF)

O Azure AI Language tem o recurso **PII Entity Recognition** que cobre todas
essas categorias com modelo ML treinado. Endpoint do user:
`https://tp-language-pii.cognitiveservices.azure.com/`.

A questão é: **como integrar sem quebrar a tese arquitetural existente**
(regex local rápido, fail-open por padrão, hot path enxuto) e sem
introduzir latência inaceitável.

## Decision

Adicionar **`internal/security/azlanguage/`** com um cliente HTTP do Azure
Language Service, e plugar ele no pipeline de chat como **camada
complementar** ao regex local:

- **Pipeline sequencial**: regex local roda primeiro (sub-ms, sempre).
  Depois, no body já mascarado, o Language roda só pra Tier 2 e 3.
- **Substituição no body**: PII detectada vira placeholder
  `[CATEGORIA_REDACTED]` antes do call ao provider. Mesma semântica do
  regex local.
- **Fail-open / fail-closed por tier** (decisão prévia do user):
  - Tier 2: fail-open — Language fora ou erro → logger.Warn + segue
  - Tier 3: fail-closed — Language fora ou erro → 503 + audit error
- **Categorias amplas (default Azure)**: deixar o serviço decidir todas
  as entidades suportadas (Person, Email, PhoneNumber, BRCPFNumber,
  BRLegalEntityNumber, Address, IPAddress, CreditCardNumber, etc.). Mais
  cobertura no escopo declarado pelo user.
- **Reconstrução do placeholder**: o `redactedText` que o Azure devolve
  substitui PII por asteriscos (`***`). Pra manter consistência com o
  regex local (que usa `[CATEGORIA_REDACTED]`), o cliente **reconstrói
  o texto** a partir do array `entities` da resposta, substituindo cada
  trecho `[offset:offset+length]` pelo placeholder nomeado.

### Config

Nova seção opcional em `gateway.yaml` (resolvida do KV via Onda 3):

```yaml
azure_language:
  endpoint: ${AZURE_LANGUAGE_ENDPOINT}      # env, não é segredo
  api_key:  ${kv:AZURE-LANGUAGE-API-KEY}    # KV
  api_version: "2024-11-01"
  timeout_ms: 1500
  language: pt-BR
```

Quando a seção está ausente, Tier 2 e 3 simplesmente pulam essa etapa
(fail-open implícito do `ShieldClient == nil` padrão). Isso permite que
o gateway rode sem KV/Language em dev ou em ambientes que não querem
custo de chamada cloud.

### Latência esperada

Endpoint `/language/:analyze-text?api-version=2024-11-01`:

- p50 observado: ~150-250ms (West Europe → Brasil)
- p99: ~400-600ms
- Timeout configurado: 1500ms

Como roda sequencialmente após o regex local (sub-ms), o overhead total
é dominado por essa latência. Aceitável pra Tier 2 e 3 — o consumidor
desses tiers já tolera latência adicional do PromptShield + Content
Safety quando ativo.

## Options considered

### Option 1: Só regex local (status quo)
- **Pros:** latência <1ms, zero dependência externa, custo zero.
- **Cons:** nomes próprios, endereços, datas em texto livre escapam.
  Em domínios sensíveis (saúde, financeiro, RH), isso é gap real.
- **Por que não:** o user explicitamente pediu o serviço complementar.

### Option 2: Só Azure Language, sem regex local
- **Pros:** menos código no gateway, uma fonte de verdade.
- **Cons:**
  - Tier 1 deixa de ter qualquer proteção (não chama cloud) → regressão
  - Cada request paga latência de ~150-600ms mesmo pra CPF/cartão óbvio
    que regex pega em sub-ms
  - Dependência total: Azure Language fora = Tier 2/3 quebrado
  - Custo por requisição na conta da Azure
- **Por que não:** regressão de latência inaceitável, e regex local cobre
  ~90% dos casos brasileiros com custo zero.

### Option 3 (chosen): Sequencial — regex primeiro, Language depois no body mascarado
- **Pros:**
  - Mantém Tier 1 enxuto (regex only, latência <1ms)
  - Tier 2/3 ganha cobertura ML sem trabalhar duplicado: Language vê
    texto já mascarado, então só detecta o que regex perdeu (menos
    barulho no resultado)
  - Fail-open por tier preserva semântica existente (consistente com
    PromptShield e Content Safety)
  - Substituição no body mantém compatibilidade — provider recebe texto
    redacted, igual ao regex local
- **Cons:**
  - Latência sequencial = regex + Language. Pra Tier 2/3, p50 vira
    ~150-250ms só dessa etapa
  - Custo por request na conta Azure
- **Why:** equilibra cobertura, latência e custo. Não regride Tier 1, e
  ativa cobertura ML onde faz sentido (tiers que já toleram chamadas
  cloud).

### Option 4: Paralelo — regex e Language ao mesmo tempo, merge dos resultados
- **Pros:** latência total = max(regex, Language) ≈ Language sozinho
  (~150-250ms). Ganho real pra Tier 2/3 em volume alto.
- **Cons:**
  - Language vê o texto **original**, sem regex aplicado. Pode emitir
    entidades sobre conteúdo que o regex já ia mascarar — gera
    duplicação no merge
  - Merge não é trivial: dois conjuntos de placeholders sobre o mesmo
    texto exigem reconciliação de offsets. Bug-prone
  - Goroutine + canal por request adiciona alocação no hot path
- **Por que não agora:** complexidade vs ganho não justifica nesta
  entrega. Pode virar otimização futura se a latência sequencial doer
  em prod.

### Option 5: Bloquear request quando detectar PII (não substituir)
- **Pros:** postura mais rígida — PII nunca vaza pro provider.
- **Cons:**
  - Frustrante pro consumidor: prompt legítimo pode ter PII implícita
    (`meu cliente João Silva fez...`) e ser rejeitado sem explicação
    útil. Categoria sem severidade ajustável
  - Inconsistente com regex local que mascara, não bloqueia
- **Por que não:** decisão prévia do user — manter semântica de mascarar.

## Consequences

### Positive
- Cobertura amplia significativamente em Tier 2 e 3 (nomes próprios,
  endereços, dados estrangeiros, identificadores criativos)
- Categorias detectáveis aumentam de ~6 (regex) pra ~30+ (Azure)
- Fail-open Tier 2 garante que dev local sem Language continua rodando
- Tier 3 ganha um terceiro pilar de defesa (regex + Language + Content
  Safety) — defesa em profundidade real
- Audit gera evento separado (`pii_detected_remote`) por chamada,
  permitindo comparar precisão regex local vs cloud

### Negative / Trade-offs
- Latência Tier 2/3 adiciona ~150-250ms p50 por request. Mensurável,
  mas tolerável pro caso de uso
- Custo: Azure Language cobra por 1k character transactions. Em prompts
  grandes (chat com histórico) e volume alto, fatura sobe — operador
  precisa monitorar
- Dependência adicional: Language fora em Tier 3 = 503. Mitigação:
  fail-open Tier 2 absorve casos não-críticos; Tier 3 já é o tier
  paranoico por design
- Body enviado ao provider pode ficar diferente do prompt original em
  formas não-óbvias. Debug visual exige inspecionar a resposta do
  Language no audit

### Mitigations
- Timeout agressivo (1500ms default, configurável) impede que Language
  travado prenda a request indefinidamente
- Cliente faz HTTPS keep-alive (transport compartilhado) pra evitar
  custo de handshake por request
- Audit event registra `categories` detectadas — permite ajustar lista
  futuramente se categoria específica for ruidosa
- Quando seção `azure_language` está ausente do YAML, etapa é skipped
  sem erro — gateway continua subindo

## Schema do contrato Azure

### Request
```json
POST {endpoint}/language/:analyze-text?api-version=2024-11-01
Ocp-Apim-Subscription-Key: {api_key}
Content-Type: application/json

{
  "kind": "PiiEntityRecognition",
  "parameters": { "modelVersion": "latest" },
  "analysisInput": {
    "documents": [
      { "id": "1", "language": "pt-BR", "text": "..." }
    ]
  }
}
```

### Response
```json
{
  "kind": "PiiEntityRecognitionResults",
  "results": {
    "documents": [
      {
        "id": "1",
        "redactedText": "Meu cliente *** *** mora em ***",
        "entities": [
          {
            "text": "João Silva",
            "category": "Person",
            "subcategory": null,
            "offset": 12,
            "length": 10,
            "confidenceScore": 0.99
          }
        ],
        "warnings": []
      }
    ],
    "errors": [],
    "modelVersion": "2023-09-01"
  }
}
```

### Algoritmo de reconstrução (em vez de usar `redactedText`)

```
texto_original = "Meu cliente João Silva mora em Belo Horizonte"
entities = [
  {text:"João Silva", category:"Person",  offset:12, length:10},
  {text:"Belo Horizonte", category:"Address", offset:32, length:14}
]

# Ordena por offset DECRESCENTE para não invalidar offsets anteriores
# ao substituir
for ent in sorted(entities, by=offset, desc):
    placeholder = f"[{ent.category.upper()}_REDACTED]"
    texto = texto[:ent.offset] + placeholder + texto[ent.offset+ent.length:]

# Resultado:
# "Meu cliente [PERSON_REDACTED] mora em [ADDRESS_REDACTED]"
```

Esta abordagem mantém consistência com placeholders do regex local
(`[BR_CPF_REDACTED]`, `[PCI_CARD_REDACTED]`).

## References

- ADR-0010 — generic HTTP proxy engine
- ADR-0017 — path translation (não usado aqui, só relevante p/ contexto)
- ADR-0018 — Key Vault (a `api_key` do Language entra via `${kv:…}`)
- SPEC.md §5.3 — tier pipeline table
- SPEC.md §10 — PII masking specification
- Azure AI Language PII docs:
  https://learn.microsoft.com/azure/ai-services/language-service/personally-identifiable-information/overview
- Categorias suportadas:
  https://learn.microsoft.com/azure/ai-services/language-service/personally-identifiable-information/concepts/entity-categories
- REST reference:
  https://learn.microsoft.com/rest/api/language/text-analysis-runtime/analyze-text
