# Handoff — retomada da sessão

> **Quando ler:** ao abrir o projeto amanhã. Esse documento é "como continuar
> sem perder contexto". Tem 5 seções; siga em ordem.
>
> **Quando editar:** ao final de cada sessão de trabalho, sobrescreva com o
> novo estado. Não acumular versões — esse arquivo é de **uso atual**, não
> histórico (pra histórico use `roadmap.md` §6 e o `git log`).

---

## 1. Estado em que paramos

**Data:** sessão fechada na noite anterior.
**Último commit aplicado:** `399935c fix: rotação de chave + catálogo de exemplos no playground`

### O que está commitado (e validado em runtime)

- ✅ Ondas 1–5 inteiras (ASCII tokens, path translation, KeyVault, Azure Language PII, UI completa)
- ✅ Bugfix da rotação de Application Key (migration 007)
- ✅ Catálogo de exemplos no Playground (Onda 5F)

### O que está pendente de commit (mas pronto e testado em build)

**Onda 6 — Latency Breakdown Observável (ADR-0021)**.

Arquivos modificados:
- `docs/how-it-works.md` — nova seção "Latência observável por bucket"
- `docs/roadmap.md` — reorganização inteira em 7 eixos + Onda 6 anotada
- `internal/api/handlers/chat.go` — instrumentação dos 5 buckets + header
- `internal/usage/event.go` — 5 campos `Lat*Ms` em `UsageEvent`
- `internal/usage/writer.go` — `insertSQL` ampliado pra 17 colunas

Arquivos novos:
- `docs/adrs/0021-latency-breakdown-observavel.md` — ADR completa
- `docs/handoff.md` — este documento
- `internal/observability/trace.go` — `LatencyTrace` (Start/Mark/Bucket/Header)
- `internal/observability/trace_test.go` — 7 cases (incl. concurrency)
- `migrations/008_usage_events_latency_breakdown.{up,down}.sql`

**Validação atual:** `go vet`, `go build`, `go test ./...` — 15 pacotes OK, zero
falhas. Falta apenas validação ao vivo (Postgres + Azure real).

---

## 2. Próxima ação — em 4 passos

### Passo 1 — Confirma que nada foi perdido

```pwsh
cd "E:/Teleperformance CRM SA/Arquitetura/Fontes/GoGateway/ai-gateway"
git status --short
```

Deve listar exatamente os mesmos arquivos da seção 1 acima (com possível
`.idea/workspace.xml` extra do GoLand — ignorável). Se houver coisa
inesperada, **pare e revise antes de prosseguir**.

### Passo 2 — Commit da Onda 6

Mensagem pronta (use HEREDOC, formato igual aos commits anteriores):

```
feat(observability): latency breakdown por bucket (ADR-0021)

Instrumentação que decompõe a latência total de cada request em 5 buckets
agregados (auth, mask, guardrails, provider, encode). Exposto via header
de response E persistido em usage_events.

Motivação
Owner reportou latência alta (~2.6s pra gpt-4.1). Sem decomposição, qualquer
afirmação sobre causa era chute. Agora dá pra defender com dado: "p50 de
provider é 2400ms, mask é 180ms, gateway adiciona ~50ms" etc.

Mecanismo
- internal/observability/trace.go: LatencyTrace thread-safe com
  Start/Mark/Bucket/Header. nil-safe pra permitir instrumentação
  incremental. ~50ns por Mark; ~250ns total por request.
- Buckets canônicos em LatencyBuckets: auth, mask, guardrails, provider,
  encode. Ordem fixa pra header determinístico.

Persistência
- migrations/008: 5 colunas INTEGER NULL em usage_events. Sem backfill
  — linhas antigas ficam NULL nas novas colunas; dashboards filtram
  WHERE lat_provider_ms IS NOT NULL.
- internal/usage/event.go: UsageEvent ganha 5 campos Lat*Ms.
- internal/usage/writer.go: helper interno transforma todos-zero em
  todos-NULL (callers legados que não instrumentam não dirty colunas
  novas com falsos zeros).

Header
- X-Gateway-Latency-Breakdown sempre presente em response. ~80 bytes.
- Formato "auth=2;mask=180;guardrails=0;provider=2400;encode=3".
- No SSE, header sai antes do primeiro chunk — provider/encode aparecem
  como 0 no header mas são persistidos corretamente em usage_events ao
  final do stream.

Instrumentação no handler
- chat.go: Mark após decode+policy (auth), após Language (mask), após
  Content Safety (guardrails), após provider call, após JSON encode.
- Requests bloqueados cedo (injection_detected, model_not_allowed) NÃO
  emitem usage_event — por design, só requests completas persistem
  breakdown. Erros de provider (502/504) também não emitem.

Tests
- internal/observability/trace_test.go: 7 cases — nil-safe, single
  mark, accumulação no mesmo bucket, format do header, bucket
  inexistente, concorrência (-race), TotalMs.

Docs
- docs/adrs/0021-latency-breakdown-observavel.md: 4 opções comparadas
  (log-only, granularidade fina, agregada chosen, OTel), schema do
  trace, queries de exemplo.
- docs/how-it-works.md: nova seção com tabela de buckets, queries SQL
  p50/p95/p99, limitação no streaming.
- docs/roadmap.md: reorganização inteira em 7 eixos estratégicos
  (auditoria, desempenho, segurança, requisitos, dados, legalidade,
  escalabilidade). Onda 6 anotada como entregue.
- docs/handoff.md: novo. Documento de retomada de sessão.

Suite 100% verde (15 pacotes). Migration roda no boot.
```

### Passo 3 — Restart do gateway

No GoLand: Stop → Run na Run Configuration do gateway. Confirme no log:

```json
{"level":"INFO","msg":"migrations applied"}
```

A migration 008 vai rodar idempotente (adiciona 5 colunas null-able).
Se houver erro de migration aqui, **abrir issue antes de continuar**.

### Passo 4 — Validar em runtime (testes funcionais)

Faça **10-20 requests no Playground** com cenários variados:

**Request 1 — Tier 2 com PII regex + Language**
- Token: `AppPro`
- Endpoint Azure (o que você cadastrou)
- Cenário: "PII regex — CPF + cartão + telefone BR" no dropdown de exemplos
- Modelo: `gpt-4.1`

**Request 2 — Tier 3 com guardrails completos** (se você tiver Content Safety configurado)
- Token: `AppVault`
- Cenário: "Sanidade — pergunta simples"
- Modelo: `gpt-4.1-mini`

**Request 3 — Tier 1 sem guardrails** (baseline)
- Token: `AppBasico`
- Cenário: "Sanidade — pergunta simples"
- Modelo: `gpt-4.1-nano`

**O que olhar:**

1. **Header de cada response** (clica em "Headers" no painel Response do Playground):
   ```
   X-Gateway-Latency-Breakdown: auth=2;mask=180;guardrails=0;provider=2400;encode=3
   ```
   Os 5 campos devem estar presentes, com valores inteiros em ms.

2. **Query no Postgres** depois de rodar tudo:
   ```sql
   SELECT request_id, application_name, tier, model, latency_ms,
          lat_auth_ms, lat_mask_ms, lat_guardrails_ms,
          lat_provider_ms, lat_encode_ms,
          latency_ms - COALESCE(lat_auth_ms,0) - COALESCE(lat_mask_ms,0)
                     - COALESCE(lat_guardrails_ms,0) - COALESCE(lat_provider_ms,0)
                     - COALESCE(lat_encode_ms,0) AS other_ms
   FROM usage_events
   WHERE lat_provider_ms IS NOT NULL
   ORDER BY created_at DESC
   LIMIT 20;
   ```

   Verifique:
   - Linhas antigas (pré-migration) têm `NULL` nas 5 colunas novas — esperado
   - Linhas novas têm valores inteiros
   - `other_ms` (subtração) fica em 1-10ms tipicamente. Se algum request der
     `other_ms > 50ms`, anote o `request_id` — vira investigação

3. **Análise agregada** (depois de ≥10 requests):
   ```sql
   SELECT application_name, tier,
          AVG(lat_auth_ms)::int       AS avg_auth,
          AVG(lat_mask_ms)::int       AS avg_mask,
          AVG(lat_guardrails_ms)::int AS avg_guardrails,
          AVG(lat_provider_ms)::int   AS avg_provider,
          AVG(lat_encode_ms)::int     AS avg_encode,
          AVG(latency_ms)::int        AS avg_total
   FROM usage_events
   WHERE created_at >= NOW() - INTERVAL '1 hour'
     AND lat_provider_ms IS NOT NULL
   GROUP BY application_name, tier;
   ```

---

## 3. Como interpretar os resultados — decisão da próxima onda

A próxima onda depende do que essa validação mostrar. Cenários:

### Cenário A — `lat_provider_ms` >> resto (esperado)

Exemplo: `provider=2400 mask=180 auth=2 encode=3 guardrails=0`.

**Significado:** Azure é o gargalo (~85-95% do total). Gateway adiciona
~200ms (Language + outros). Não dá pra reduzir Azure.

**Próxima onda recomendada:** **Streaming Tier 3** (P1 Desempenho). Reduz
**perceived latency** drasticamente — primeiro token chega em 200-500ms em
vez do user esperar 2.5s pela resposta inteira.

### Cenário B — `lat_mask_ms` alto e variável

Exemplo: alguns requests com `mask=400+`.

**Significado:** Azure Language está lento em casos específicos (talvez
prompts grandes ou degraded SLO).

**Próxima onda recomendada:** **Azure Language em paralelo com regex**
(P2 Desempenho, revisita decisão da Onda 4). Latência total = max(local,
cloud) em vez de soma. Ganho ~50ms p50.

### Cenário C — `other_ms` > 50ms

**Significado:** tem trabalho fora dos 5 buckets que não está sendo
medido. Provavelmente middleware (chi, auth, audit emit).

**Próxima onda recomendada:** **Instrumentar middleware** (extensão da
Onda 6 — adicionar Mark no middleware de auth). Ou investigação ad-hoc
com `slog.Debug` antes de virar onda.

### Cenário D — `lat_auth_ms + lat_encode_ms` > 30ms

**Significado:** DB hits ou serialização pesando. Vale cache.

**Próxima onda recomendada:** **Cache de policy/endpoint/grant lookup**
(P1 Desempenho). LRU+TTL em memória. ~5-10ms ganho consistente.

---

## 4. Decisões em aberto (não bloqueiam mas precisam fechar quando virarem prioridade)

### 4.1 Desacoplamento do frontend (registrado em `roadmap.md` §4.1)

5 sub-decisões antes de virar PR:
- Como o frontend descobre o endpoint do gateway? (env var no build? config runtime?)
- CORS: hoje aceita `localhost:5173`; prod precisa configurar
- Versionamento: como negociar quando front e back têm versões diferentes?
- Plataforma de deploy: S3+CloudFront / Vercel / Cloudflare Pages / GitHub Pages
- Repo: monorepo separado ou novo do zero (histórico migra?)

**Sem urgência.** Quando virar prioridade, abrir ADR-0022.

### 4.2 Cache de prompts (semantic) — registrado em `roadmap.md` §4.3

Decidido: hash exato (não embedding). 4 perguntas em aberto:
- Tier do cache: por endpoint? por aplicação? global?
- TTL default
- Headers de cache control (`Cache-Control: no-cache` força bypass?)
- Métricas: hit rate, savings em $/mês

**Sem urgência.** É P3 (Phase 3 escalabilidade).

---

## 5. Trabalho parado / dívidas conhecidas

Itens que **NÃO** são pendência de Onda 6, mas vale lembrar:

### Migration 007 — recadastro de targets necessário se houver órfãos

A Onda 5/migration 007 corrigiu `api_keys.application_id UNIQUE` que bloqueava
rotação. Se a rotação falhou várias vezes antes do fix, podem ter ficado
linhas órfãs no DB. Query pra confirmar:

```sql
SELECT a.name, k.id, k.key_prefix, k.created_at, k.rotated_at
FROM applications a
JOIN api_keys k ON k.application_id = a.id
WHERE k.rotated_at IS NULL
ORDER BY a.name, k.created_at DESC;
```

Se a mesma `application_name` aparecer 2x ou mais com `rotated_at IS NULL`,
limpe mantendo só a mais recente:

```sql
UPDATE api_keys SET rotated_at = NOW()
WHERE rotated_at IS NULL
  AND id NOT IN (
    SELECT MAX(id) FROM api_keys WHERE rotated_at IS NULL GROUP BY application_id
  );
```

### Onda 4.5 — Target credentials no KV (ainda não iniciada)

Resolve definitivamente o problema "rotacionar `DB_ENCRYPTION_KEY` quebra
targets" que você viveu. Está marcada como P1 Segurança e é a **próxima
onda sugerida** depois de validar a Onda 6. Escopo completo em
`roadmap.md` §3.3.

### Latência ainda dominada pelo Azure

Premissa importante pra discussão amanhã: o gateway está adicionando
~150-310ms ao total (~85-95% é Azure puro). Mesmo otimizando tudo no
gateway, latência mínima ficará em ~1.5s pra `gpt-4.1`. Pra "latência
perceived menor", a frente real é **streaming**, não otimizar pipeline.

---

## 6. Comandos úteis pra retomar

```pwsh
# Confirmar estado
cd "E:/Teleperformance CRM SA/Arquitetura/Fontes/GoGateway/ai-gateway"
git status --short
git log --oneline -5

# Validar que tudo ainda buila
go vet ./...
go build ./...
go test ./...

# Frontend (se quiser rodar separado em dev)
cd web
npm run dev   # Vite em http://localhost:5173

# Frontend embedado (build pro Go go:embed)
cd web
npm run build

# Subir Postgres se não estiver
docker compose up -d postgres
```

---

## 7. Quem está nas Run Configurations do GoLand

Lembre que pra rodar localmente você precisa de:
- **gateway** (Run Config principal, `cmd/gateway`, `.env` carregado pelo plugin EnvFile, working dir = raiz do projeto)
- **admin-create** (CLI, só pra provisionar admin novo, raramente usada)

Variáveis críticas no `.env`:
- `KEYVAULT_URI=https://danieldev.vault.azure.net/`
- `AZURE_OPENAI_ENDPOINT=https://danie-mc4ryviy-westeurope.cognitiveservices.azure.com`
- `AZURE_LANGUAGE_ENDPOINT=https://tp-language-pii.cognitiveservices.azure.com`
- `DATABASE_URL=postgres://gateway:gateway@localhost:5432/gateway?sslmode=disable`

Auth Azure: `az login --tenant c050c98c-b463-4591-ac3b-deb782c0ba6e`
(tem MFA — fluxo interativo pelo browser).

Detalhes completos em `docs/local-development.md` e `docs/keyvault-setup.md`.
