# Manutenção

Este documento cobre as tarefas operacionais do dia a dia: adicionar aplicações, rotacionar chaves, gerenciar migrations, monitorar logs e interpretar dados do banco.

---

## Adicionar uma nova aplicação

### Passo 1 — Gerar o token

```bash
TOKEN="gwk_novaapp_$(openssl rand -hex 24)"
HASH=$(echo -n "$TOKEN" | sha256sum | cut -d' ' -f1)

echo "Distribuir para a aplicação: $TOKEN"
echo "Inserir no gateway.yaml:     $HASH"
```

Entregue o `$TOKEN` completo para o time responsável pela aplicação consumidora. Armazene apenas o `$HASH`.

### Passo 2 — Editar `configs/gateway.yaml`

Adicione um novo bloco em `applications`:

```yaml
  - name: NomeDaApp
    key_prefix: gwk_novaapp     # mesmo prefixo usado no token acima
    key_hash: "64-chars-hex"    # resultado do sha256sum
    tier: tier_1                # tier_1 | tier_2 | tier_3
    allowed_models: [gpt-4.1-nano]
    streaming_allowed: true
    max_rpm: 120
    max_tpm: 100000
    monthly_budget_brl: 200.00
```

**Regra do prefixo:** o `key_prefix` deve ser exatamente o que `ExtractPrefix` retorna do token, i.e., `gwk_` + o segundo segmento separado por `_`. Exemplos:
- Token `gwk_crm_abc123` → prefix `gwk_crm`
- Token `gwk_voiceai_xyz789` → prefix `gwk_voiceai`

### Passo 3 — Reiniciar o gateway

```bash
# Local
# Ctrl+C + go run ./cmd/gateway

# Docker Compose
docker compose restart gateway

# Verificar que a app aparece nos modelos
curl -s http://localhost:8080/v1/models \
  -H "Authorization: Bearer $TOKEN" | jq
```

> **Nota (Phase 2):** No futuro, a migração para DB-backed policies (ADR-0002) eliminará a necessidade de restart para adicionar aplicações.

---

## Rotacionar a chave de uma aplicação

A rotação exige trocar o `key_hash` no YAML e reiniciar. Para minimizar downtime:

1. **Gerar novo token e hash:**
   ```bash
   NEW_TOKEN="gwk_appname_$(openssl rand -hex 24)"
   NEW_HASH=$(echo -n "$NEW_TOKEN" | sha256sum | cut -d' ' -f1)
   ```

2. **Coordenar com o time da aplicação consumidora** para que eles estejam prontos para atualizar o token deles.

3. **Atualizar `key_hash`** no YAML para o novo hash.

4. **Reiniciar o gateway** (brevíssima janela de indisponibilidade durante o restart).

5. **A aplicação consumidora** atualiza seu token para o novo.

6. O token antigo não funciona mais imediatamente após o restart.

---

## Gerenciar migrations

### Ver migrations aplicadas

```bash
docker exec -it $(docker compose ps -q postgres) psql -U gateway -d gateway \
  -c "SELECT version, dirty, applied_at FROM schema_migrations ORDER BY version;"
```

### Aplicar migrations manualmente

Se precisar rodar migrations fora do boot do gateway (ex: manutenção):

```bash
# Instalar ferramenta migrate
go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest

# Aplicar todas as pendentes
migrate -database "$DATABASE_URL" -path migrations up

# Reverter 1 passo
migrate -database "$DATABASE_URL" -path migrations down 1

# Ver versão atual
migrate -database "$DATABASE_URL" -path migrations version
```

### Criar uma nova migration

```bash
# Nomear com descrição curta
migrate create -ext sql -dir migrations -seq add_model_column

# Isso cria:
# migrations/002_add_model_column.up.sql
# migrations/002_add_model_column.down.sql
```

Sempre preencha **tanto o `.up.sql` quanto o `.down.sql`** (o down deve reverter exatamente o que o up faz).

---

## Monitorar logs

### Formato dos logs

Em produção (`format: json`), cada linha é um JSON estruturado:

```json
{"time":"2026-05-19T14:23:01Z","level":"INFO","msg":"request completed",
 "request_id":"01HXYZ...","status_code":200,"latency_ms":342,"event_type":"request_completed"}
```

### Campos principais

| Campo | Quando aparece |
|---|---|
| `request_id` | Em todos os logs de request lifecycle |
| `application_name` | Após auth bem-sucedido |
| `event_type` | Identifica o tipo de evento (ver tabela abaixo) |
| `latency_ms` | No log de `request_completed` |
| `status_code` | No log de `request_completed` |
| `err` | Em `level=ERROR` |

### Event types importantes

| `event_type` | Level | Significado |
|---|---|---|
| `request_started` | INFO | Início do request |
| `request_completed` | INFO | Fim normal |
| `auth_failed` | WARN | Token inválido ou desconhecido |
| `pii_masked` | INFO | PII redatado no prompt |
| `injection_detected` | WARN | Injeção detectada localmente (Tier 2+) |
| `prompt_shield_block` | WARN/ERROR | Bloqueado pelo Azure CS |
| `content_safety_block` | WARN/ERROR | Bloqueado pelo Azure CS Text Analyze |
| `rate_limited` | WARN | App excedeu RPM |
| `budget_exceeded` | WARN | App atingiu limite mensal |
| `provider_error` | ERROR | Erro na chamada ao Azure OpenAI |
| `stream_cancelled` | INFO | Cliente desconectou durante stream |
| `panic_recovered` | ERROR | Panic recuperado (bug — investigar) |
| `usage_dropped` | WARN | Canal de uso cheio (sobrecarga) |
| `audit_dropped` | WARN | Canal de auditoria cheio (sobrecarga) |

### Filtrar logs por request_id (rastrear um request específico)

```bash
# Num container Docker:
docker compose logs gateway | grep '"request_id":"ID-DO-REQUEST"'

# Com jq para leitura melhor:
docker compose logs gateway | jq -R 'fromjson? | select(.request_id == "ID-DO-REQUEST")'
```

### Alertas recomendados

| Condição | Ação |
|---|---|
| `level=ERROR` com `event_type=panic_recovered` | Investigar imediatamente — é um bug |
| `level=ERROR` com `event_type=provider_error` frequente | Verificar status do Azure OpenAI |
| `event_type=usage_dropped` ou `audit_dropped` | Gateway sob sobrecarga — escalar |
| `event_type=budget_exceeded` para aplicação crítica | Ajustar limite ou investigar consumo |

---

## Consultar dados no banco

### Top 10 requests mais lentos hoje

```sql
SELECT request_id, application_name, model, latency_ms, status_code, created_at
FROM usage_events
WHERE created_at >= NOW() - INTERVAL '24 hours'
ORDER BY latency_ms DESC
LIMIT 10;
```

### Consumo por aplicação no mês corrente

```sql
SELECT
  application_name,
  total_requests,
  total_tokens,
  ROUND(estimated_cost_brl::numeric, 4) AS custo_brl
FROM budget_counters
WHERE period_yyyymm = TO_CHAR(NOW() AT TIME ZONE 'UTC', 'YYYYMM')
ORDER BY estimated_cost_brl DESC;
```

### Eventos de segurança nas últimas 24h

```sql
SELECT event_type, severity, COUNT(*) AS total, MAX(created_at) AS ultimo
FROM audit_events
WHERE created_at >= NOW() - INTERVAL '24 hours'
  AND event_type != 'pii_masked'   -- excluir eventos normais de masking
GROUP BY event_type, severity
ORDER BY total DESC;
```

### Rastrear um request completo pelo request_id

```sql
-- Usage
SELECT * FROM usage_events WHERE request_id = 'ID-AQUI';

-- Audit trail
SELECT event_type, severity, metadata, created_at
FROM audit_events
WHERE request_id = 'ID-AQUI'
ORDER BY created_at;
```

### Taxa de bloqueio por tipo nas últimas 1h

```sql
SELECT event_type, COUNT(*) AS bloqueios
FROM audit_events
WHERE created_at >= NOW() - INTERVAL '1 hour'
  AND event_type IN ('auth_failed','model_blocked','injection_detected',
                     'prompt_shield_block','content_safety_block',
                     'rate_limited','budget_exceeded')
GROUP BY event_type
ORDER BY bloqueios DESC;
```

---

## Manutenção do banco

### Estimativa de crescimento

Estimativa conservadora com 1.000 req/dia:
- `usage_events`: ~1.000 linhas/dia ≈ 365.000/ano ≈ 100 MB/ano
- `audit_events`: ~2.000–5.000 linhas/dia (múltiplos eventos por request)
- `budget_counters`: 3 linhas/mês (uma por app) — irrelevante

### Limpeza de dados antigos (LGPD / retenção)

```sql
-- Deletar usage_events com mais de 90 dias
DELETE FROM usage_events
WHERE created_at < NOW() - INTERVAL '90 days';

-- Deletar audit_events com mais de 1 ano
DELETE FROM audit_events
WHERE created_at < NOW() - INTERVAL '1 year';

-- VACUUM após delete em massa
VACUUM ANALYZE usage_events;
VACUUM ANALYZE audit_events;
```

Considere criar uma cron job ou extensão `pg_cron` para automatizar isso.

### Backup

```bash
# Backup completo
pg_dump -h localhost -U gateway gateway > backup_$(date +%Y%m%d).sql

# Restaurar
psql -h localhost -U gateway gateway < backup_20260519.sql
```

---

## Ajustar limites sem reiniciar (workaround Phase 1)

Na Phase 1, qualquer mudança no YAML exige restart. Para mudanças urgentes (ex: aumentar budget de uma app):

```sql
-- Zerar o contador do mês atual para uma app (emergency reset)
UPDATE budget_counters
SET estimated_cost_brl = 0,
    total_requests = 0,
    total_tokens = 0,
    updated_at = NOW()
WHERE application_name = 'NomeDaApp'
  AND period_yyyymm = TO_CHAR(NOW() AT TIME ZONE 'UTC', 'YYYYMM');
```

Isso dá folga imediata enquanto o YAML não é atualizado. Use com cautela.
