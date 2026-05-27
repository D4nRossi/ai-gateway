# ADR-0021: Latency breakdown observável por request

- **Status**: accepted
- **Date**: 2026-05-26
- **Decision makers**: Daniel (owner)
- **Consulted**: Claude Opus 4.7

## Context

A `usage_events.latency_ms` mede só o **total** end-to-end por request.
Quando o owner reporta "latência alta" (ex.: 2.6s pra gpt-4.1), não há jeito
de saber onde foi gasto:

- Foi o Azure OpenAI sendo lento? (típico, 1.5-3s)
- Foi o Azure AI Language adicionando 200ms a mais? (Tier 2/3)
- Foi DB hit em auth + grant lookup? (~5-10ms)
- Foi serialização de uma response gigante?

Sem decomposição, qualquer otimização vira chute. Esse ADR formaliza a
instrumentação por etapa do pipeline pra dar ao operador (e ao desenvolvedor)
dados acionáveis.

## Decision

Instrumentar o handler de chat com um trace de 5 buckets agregados,
expostos via:

- **Header de response** `X-Gateway-Latency-Breakdown` (sempre).
- **Colunas em `usage_events`** (`lat_auth_ms`, `lat_mask_ms`,
  `lat_guardrails_ms`, `lat_provider_ms`, `lat_encode_ms`) — null-able,
  sem backfill em dados antigos.

### Buckets

| Bucket | O que conta | Etapas medidas |
|---|---|---|
| `auth` | Resolução de identidade da request | Bearer extract, prefix lookup, hash compare, policy fetch |
| `mask` | Mascaramento de PII | Regex local + Azure AI Language (cloud) |
| `guardrails` | Defesas adicionais Tier 2/3 | Local injection scan + Azure Prompt Shield + Azure Content Safety |
| `provider` | Chamada ao LLM upstream | Marshal request + HTTP roundtrip + unmarshal (não-stream); ou stream open + chunks loop (stream) |
| `encode` | Serialização da response ao cliente | JSON marshal final + Write |

**5 buckets** foram escolhidos (em vez de 10 finos) porque:

- Cobrem ~95% da latência observável com 5 colunas no DB (vs 10 que polluem
  o schema)
- Cada bucket é **acionável** — tem uma frente de otimização distinta no
  roadmap (cache de auth, paralelizar mask, streaming Tier 3, etc.)
- Granularidade fina por sub-etapa (e.g. "injection vs shield") pode ser
  obtida ad-hoc via `slog.Debug` quando necessário

**"Other"** (tempo total - soma dos buckets) é implícito: o operador
calcula com `latency_ms - (lat_auth_ms + lat_mask_ms + ...)`. Geralmente
fica em 1-5ms (chi router, middlewares triviais, audit emit que é async).
Se virar >50ms, é sinal de buraco e vira frente nova.

### Header sempre emitido

`X-Gateway-Latency-Breakdown: auth=2;mask=180;guardrails=0;provider=2400;encode=3`

- Sempre (não condicional a header de debug do cliente)
- ~80 bytes adicionais por response
- Formato `key=value;` separado por `;` (fácil de parsear; alinhado com
  `Server-Timing` HTTP header sem virar o `Server-Timing` formal — esse
  exige a unidade `dur` e tem semântica do W3C que não queremos amarrar)
- Valores em milissegundos inteiros (sub-ms vira 0)

### Persistência

Migration 008 adiciona as 5 colunas como `INTEGER NULL` em `usage_events`.
Linhas antigas (pré-Onda) ficam NULL nessas colunas; dashboards que
consultam o breakdown filtram `WHERE lat_provider_ms IS NOT NULL`.

**Sem backfill heurístico** — não vamos inventar que "100% da latência
antiga era do provider" porque isso é estimativa tratada como fato.
Análises históricas usam apenas `latency_ms` global.

## Options considered

### Option 1: Apenas log estruturado (slog.Debug com timings)
- **Pros:** zero schema change, zero header
- **Cons:** debug log fica enorme; agregação requer parser de logs;
  não acessível ao cliente pra debug imediato
- **Por que não:** owner pediu dados pra dashboard. Log não vira gráfico
  sem mais infra.

### Option 2: Granularidade fina (10 buckets separados)
- **Pros:** dashboards super detalhados
- **Cons:** 10 colunas em `usage_events`, header de 200+ bytes, e a maioria
  dos buckets fica em <5ms (ruído estatístico). Schema bloat por marginal
  insight
- **Por que não:** 95% do valor cabe em 5 buckets. Granularidade fina é
  trivial via `slog.Debug` quando precisar de zoom.

### Option 3 (chosen): 5 buckets agregados, header sempre, colunas null-able
- **Pros:**
  - Cobre os 5 pontos de otimização do roadmap (auth cache, mask
    paralelo, streaming Tier 3, connection warming, encode)
  - Header é debug-friendly imediato (curl + olho nu)
  - Colunas no DB permitem dashboards históricos via SQL puro
  - Sem backfill = deploy zero-risk
- **Cons:**
  - Granularidade média perde "qual guardrail específico (injection vs
    shield)". Aceito — guardrails são compostos só em Tier 3 com CS
    configurado; dá pra granular ad-hoc com debug log
  - Schema cresce em 5 colunas. Aceitável (já tem 9; vira 14)
- **Why:** equilíbrio entre acionabilidade e custo de schema/header.

### Option 4: OpenTelemetry tracing completo
- **Pros:** observability gold standard, dashboards prontos (Jaeger, Tempo)
- **Cons:** owner foi explícito que observabilidade externa fica fora
  do escopo agora ("apenas logs no banco"). OTel sem coletor externo é
  overhead sem benefício
- **Por que não agora:** será uma frente própria quando observabilidade
  externa entrar no roadmap.

## Consequences

### Positive
- Owner consegue defender qualquer afirmação sobre latência com dado
  específico em vez de chute
- Dashboards futuros (frente §3.5 do roadmap) ganham 5 séries acionáveis
  por aplicação
- Header habilita debug ad-hoc (`curl -i` ou DevTools Network)
- Cada otimização futura tem métrica clara de antes/depois
- Custo de execução é desprezível: ~5 `time.Now()` por request (~50ns
  cada, ~250ns total)

### Negative / Trade-offs
- Schema de `usage_events` cresce em 5 colunas (de 9 pra 14)
- Header adiciona ~80 bytes por response — irrelevante mesmo em volume
  alto, mas tecnicamente é bandwidth
- Migration nova exige restart do gateway pra aplicar (idempotente)
- Buckets agregados não distinguem sub-etapas em casos raros (e.g.
  injection vs shield em Tier 3 — fica `guardrails=X` sem split)

### Mitigations
- Sub-etapas detalhadas continuam disponíveis via `slog.Debug` quando
  o log level está em debug. Sem custo em prod (info default)
- Colunas null-able + sem backfill = deploy sem janela de manutenção
- Header pode ser desabilitado por config futura se virar problema
  (não previsto, mas hook fica)

## Implementation sketch

```go
// internal/observability/trace.go
type LatencyTrace struct {
    start time.Time
    last  time.Time
    mu    sync.Mutex
    bucks map[string]time.Duration
}

func StartTrace() *LatencyTrace { /* ... */ }
func (t *LatencyTrace) Mark(bucket string) { /* delta = now - last; bucks[bucket] += delta; last = now */ }
func (t *LatencyTrace) Bucket(name string) int /* ms */
func (t *LatencyTrace) Header() string  // "auth=2;mask=180;..."
```

```go
// internal/api/handlers/chat.go (resumo)
trace := observability.StartTrace()
// ... auth (já feito por middleware; chat marca o final de auth ao entrar)
trace.Mark("auth")
// ... masking local + language
trace.Mark("mask")
// ... injection + shield + CS
trace.Mark("guardrails")
// ... provider call
trace.Mark("provider")
// ... json encode + write
trace.Mark("encode")

w.Header().Set("X-Gateway-Latency-Breakdown", trace.Header())
usageWriter.Emit(usage.UsageEvent{
    // ...
    LatAuthMs:       trace.Bucket("auth"),
    LatMaskMs:       trace.Bucket("mask"),
    LatGuardrailsMs: trace.Bucket("guardrails"),
    LatProviderMs:   trace.Bucket("provider"),
    LatEncodeMs:     trace.Bucket("encode"),
})
```

## References

- ADR-0019 — Azure AI Language (a maior fonte de latência mensurável
  no gateway hoje)
- ADR-0008 — `WriteTimeout=0` (streaming, relevante pra `encode` em SSE)
- SPEC.md §13 — observability requirements (a parte que ficou genérica
  agora ganha forma concreta nesse ADR)
- W3C Server-Timing (header similar, não adotado): https://www.w3.org/TR/server-timing/
