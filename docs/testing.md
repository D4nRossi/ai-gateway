# Suite de Testes

Este documento descreve como executar os testes, o que cada arquivo de teste cobre, e os números de performance medidos em desenvolvimento.

---

## Como rodar

```bash
# Rodar todos os testes (sem banco — testes unitários não precisam de PostgreSQL)
go test ./...

# Com detector de race conditions (recomendado antes de qualquer merge)
go test -race ./...

# Verbose — ver nome de cada caso
go test -v ./...

# Um pacote específico
go test ./internal/security/masking/...
go test ./internal/api/handlers/...

# Apenas um teste específico
go test -run TestValidCPF ./internal/security/masking/...
go test -run TestAuth_SQLInjectionInToken ./internal/api/middleware/...

# Benchmarks
go test -bench=. -benchmem ./internal/ratelimit/...
go test -bench=. -benchmem ./internal/api/handlers/...
go test -bench=. -benchmem ./internal/security/...

# Benchmarks de todos os pacotes
go test -bench=. -benchmem -benchtime=2s ./...

# Teste de latência com saída detalhada
go test -v -run 'TestChat_LatencyDistribution|TestChat_TierPipeline_Latency' \
  ./internal/api/handlers/...
```

---

## Mapa de arquivos de teste

### `internal/auth/`

| Arquivo | Casos | O que cobre |
|---|---|---|
| `hash_test.go` | 6 | `ExtractPrefix`: formatos válidos, prefixo curto, sem underscore, vazio |
| `policy_test.go` | 4 | `PolicyStore.Lookup`: hit, miss, prefixo errado, store vazio |

### `internal/config/`

| Arquivo | Casos | O que cobre |
|---|---|---|
| `config_test.go` | 10 | `Validate()`: porta inválida, endpoint vazio, tier inválido, hash malformado, modelo não no catálogo, Content Safety parcialmente configurado; `ModelByName()` |

### `internal/ratelimit/`

| Arquivo | Casos | O que cobre |
|---|---|---|
| `limiter_test.go` | 9 testes + 3 benchmarks | `newLimiter`: burst floor (rpm<10→1), burst=rpm/10, zero/negativo nega tudo; `Manager`: register+allow, app desconhecida negada, estouro de burst, isolamento por app, 50 goroutines paralelas sem race |

### `internal/security/masking/`

| Arquivo | Casos | O que cobre |
|---|---|---|
| `luhn_test.go` | 8 | Algoritmo Luhn: Visa, Mastercard, Amex, Discover; dígito errado; len<2; all-zeros passa |
| `masker_test.go` | 7 | Integração do Masker: sem detecções, CPF, cartão, Luhn inválido não mascara, email só tier_2, múltiplos, sobreposição |
| `algorithm_test.go` | 30+ + 4 benchmarks | CPF mod-11: vetores válidos verificados, inválidos, all-same, comprimento; CNPJ mod-11: idem; `stripNonDigits`; `allSame`; benchmarks por tamanho e carga de PII |

### `internal/security/promptshield/`

| Arquivo | Casos | O que cobre |
|---|---|---|
| `local_test.go` | 15 | Cada padrão de injeção; case-insensitive; falsos positivos; prompt longo |
| `evasion_test.go` | 40+ + 6 benchmarks | Os 14 padrões individualmente; 9 variantes de casing; tentativas de evasão comuns; 11 textos legítimos que NÃO devem disparar; payload de 50 KB; benchmarks short/long/parallel/worst-case |

### `internal/security/postvalidation/`

| Arquivo | Casos | O que cobre |
|---|---|---|
| `validator_test.go` | 18 + 2 benchmarks | Saída limpa não bloqueada; 8 padrões de jailbreak na saída do modelo; injeção no meio de resposta longa; injeção no início; whitespace-only |

### `internal/tiers/`

| Arquivo | Casos | O que cobre |
|---|---|---|
| `engine_test.go` | 4 | PipelineFor: tier_1, tier_2, tier_3, unknown |
| `pipeline_test.go` | 8 + 1 benchmark | Cada campo de cada tier; 7 tiers desconhecidos fail-closed; semântica fail-mode; escalada de guards (cada tier é superconjunto do anterior) |

### `internal/api/middleware/`

| Arquivo | Casos | O que cobre |
|---|---|---|
| `requestid_test.go` | 2 | UUID v7 gerado; unicidade entre requests |
| `auth_test.go` | 6 | Token válido, errado, ausente, prefixo errado, esquema não-Bearer, body de erro |
| `auth_security_test.go` | 30+ | SQL injection no token; consistência de status para diferentes falhas; 9 variantes de header; 7 headers malformados (nunca 500); policy em contexto após sucesso; 50 goroutines sem race |
| `ratelimit_test.go` | 6 | Permite, nega, body 429, Retry-After header, passthrough sem policy, Content-Type |
| `recover_test.go` | 7 | Panic → 500; body correto; não vaza internos; Content-Type; handler normal não afetado; nil panic; error panic |

### `internal/api/handlers/`

| Arquivo | Casos | O que cobre |
|---|---|---|
| `health_test.go` | 1 | `/healthz` sempre 200 |
| `chat_test.go` | 7 | ModelNotAllowed, StreamingNotAllowed, InvalidJSON, BudgetExceeded, NonStreamSuccess, BodyTooLarge (413), PIIMaskedInTier1 |
| `chat_load_test.go` | 5 testes + 5 benchmarks | 50 goroutines × 10 requests sem falha; distribuição P50/P95/P99; latência por tier; 30 goroutines de erro de policy; 30 goroutines de budget negado; benchmarks tier_1/2/3, PII, policy denial |

---

## Números de performance (hardware: AMD Ryzen 5 5625U)

### Handler chat (mock provider, sem I/O real)

| Cenário | Throughput | Latência/op | Allocs/op |
|---|---|---|---|
| Tier 1 (masking CPF+cartão) | ~138.000 req/s | 7.2 µs | 68 |
| Tier 2 (+injeção local) | ~126.000 req/s | 7.6 µs | 68 |
| Tier 3 (+todos os guards) | ~64.000 req/s | 15.6 µs | 69 |
| Com PII (CPF+cartão) Tier 1 | ~80.000 req/s | 12.5 µs | 89 |
| Policy denial (fast path) | ~84.000 req/s | 11.9 µs | 73 |

### Distribuição de latência (P50/P95/P99 — 200 requests sequenciais)

| Métrica | Valor |
|---|---|
| P50 | 8.6 µs |
| P95 | 29 µs |
| P99 | 45 µs |

### Por tier (P50 / P99 — 100 requests sequenciais, com PII)

| Tier | P50 | P99 |
|---|---|---|
| tier_1 | 24 µs | 1.2 ms |
| tier_2 | 52 µs | 453 µs |
| tier_3 | 76 µs | 1.2 ms |

### Componentes individuais

| Componente | Latência | Allocs |
|---|---|---|
| `rate.Allow()` (Manager) | 182 ns | 0 |
| `Allow()` app desconhecida | 7.6 ns | 0 |
| `PipelineFor()` (switch) | 2.7 ns | 0 |
| Masker — sem detecção (2 KB) | 29 µs | 0 |
| Masker — CPF only | 5 µs | 11 |
| Masker — multi-PII (6 categorias) | 51 µs | 33 |
| Masker — texto longo (2 KB, 1 CPF) | 560 µs | 9 |
| LocalScanner — texto curto, limpo | 19 µs | 1 |
| LocalScanner — detecção imediata | 749 ns | 1 |
| Validator (post-validation) — limpo | 938 µs | 1 |
| Validator — injeção early-exit | 1.5 µs | 0 |

> Os tempos do Validator limpo são altos porque o texto percorre **todos os 14 padrões** regex antes de concluir que não há injeção. É o pior caso esperado.

---

## Pacotes sem testes (e por quê)

| Pacote | Motivo da ausência |
|---|---|
| `internal/db/` | Requer PostgreSQL real; coberto indiretamente nos testes de integração (quando rodando com banco) |
| `internal/audit/`, `internal/usage/` | Writers assíncronos com lógica mínima; o comportamento do canal é testado via stubs nos handler tests |
| `internal/budget/` | `Checker` e `Counter` dependem de banco; cobertos via stubs (`allowBudget{}`, `denyBudget{}`) nos handler tests |
| `internal/providers/azureopenai/` | Dependência de Azure real; mock provider é suficiente para testes unitários |
| `internal/observability/` | Factory de logger; sem lógica de negócio testável |

---

## Padrões de teste adotados

- **Table-driven** (`[]struct{ name, input, want }`) com `t.Parallel()` em cada sub-teste.
- **Stubs via interface**: `audit.Emitter`, `usage.Emitter`, `budget.PreChecker`, `budget.Recorder`, `ratelimit.Limiter` — todos injetáveis por meio de structs locais nos arquivos `_test.go`.
- **Sem frameworks externos**: apenas stdlib `testing` + `net/http/httptest`. Sem testify, gomock, etc.
- **Race detector**: todos os testes passam com `go test -race ./...` sem warnings.
- **Benchmarks** com `b.RunParallel` onde aplicável (simula concorrência real).
