# ADR-0022: Troca de PostgreSQL por SQL Server (banco corporativo de homologação)

- **Status**: proposed
- **Date**: 2026-05-27
- **Decision makers**: Daniel (owner)
- **Consulted**: Claude Opus 4.7

## Context

Phase 1 (demo) e v2 (admin/proxy plane) do AI Gateway usaram PostgreSQL como
banco operacional. A escolha foi de conveniência: container `postgres:17-alpine`
no `docker-compose.yml`, driver `pgx/v5` ergonômico, sintaxe SQL familiar.

Em **homologação corporativa**, a infraestrutura disponível é Microsoft SQL
Server, instância `BRSPVPDEV003`, banco `AzureAI_Gateway_hom`, usuário de
serviço `usr_sist_AzureAI_Gateway_hom`. O banco **já possui dados de outras
aplicações** — o gateway precisa coexistir, isolado em schema próprio.

Restrições do ambiente:
- Não há flexibilidade pra trocar SQL Server por Postgres no datacenter
- Senha do usuário de serviço não pode trafegar em `.env`/YAML em claro;
  precisa viver no Azure Key Vault (já temos infraestrutura via ADR-0018)
- Banco compartilhado → schema dedicado `gogateway` qualificado em todas as
  queries
- A troca é classificada como **emergencial** pelo owner: bloqueia o avanço
  da homologação até estar funcional

## Decision

Substituir PostgreSQL por SQL Server como **único** backend operacional do
gateway, com:

1. **Driver Go**: `github.com/microsoft/go-mssqldb` (driver oficial Microsoft,
   ativo, compatível com `database/sql` stdlib)
2. **Migration driver**: `golang-migrate/migrate/v4/database/sqlserver`
   (mesma lib que já usamos para PG)
3. **Schema dedicado**: `gogateway`, qualificado explicitamente em toda query
   (`gogateway.applications`, `gogateway.api_keys`, etc.)
4. **Senha no Key Vault**: secret `AzureAIGateway-DB-Password-hom` no vault
   `danieldev`, referenciado no `gateway.yaml` via `${kv:AzureAIGateway-DB-Password-hom}`
5. **Migrations PG existentes**: movidas para `migrations/postgres-legacy/`
   como artefato histórico; não rodam mais

## Options considered

### Option 1: Manter PostgreSQL provisionando instância nova no datacenter
- **Pros:**
  - Zero reescrita de infraestrutura: driver pgx, sintaxe SQL, migrations,
    repos infra todos permanecem
  - Tipos nativos PG preservados (JSONB, TEXT[], TIMESTAMPTZ)
- **Cons:**
  - Política corporativa de TI: SQL Server é o padrão homologado; provisionar
    PG fora do padrão exige aprovações, processo, e tempo que a homologação
    não tem
  - Manutenção: o time de DBA da empresa suporta SQL Server; PG ficaria
    "ilha" sem rotinas de backup/monitoração corporativas
- **Por que não:** restrição operacional firme. Não é uma escolha técnica
  aberta.

### Option 2: SQL Server com `microsoft/go-mssqldb` (chosen)
- **Pros:**
  - Alinhamento com infraestrutura corporativa (TI suporta, backup automático,
    monitoração centralizada)
  - Driver Microsoft oficial — ativo, com releases frequentes, compatível com
    Azure SQL Database caso a migração para cloud-native seja decidida no futuro
  - Schema isolado (`gogateway`) evita conflito com dados pré-existentes
  - Senha no KV elimina segredo em arquivo de config
  - `golang-migrate` já suporta SQL Server — não troca de ferramenta
- **Cons:**
  - Reescrita massiva (~30 arquivos): driver, pool, migrations 001-009,
    repos infra, config, docs
  - Perda de tipos ergonômicos do PG:
    - JSONB → `NVARCHAR(MAX)` + funções JSON nativas (SQL Server 2016+)
    - TEXT[] → tabela auxiliar ou string com delimiter (decisão por campo)
    - TIMESTAMPTZ → `DATETIMEOFFSET` (semanticamente equivalente, ergonomia
      similar)
    - BIGSERIAL → `BIGINT IDENTITY(1,1)` (idem)
  - Driver `database/sql` + `go-mssqldb` é mais verboso que `pgx` (sem
    suporte a `pgxpool` específico; `*sql.DB` já é pool internamente)
  - `pgx.ErrNoRows` → `sql.ErrNoRows` (renomeação trivial)
  - Local development perde o `postgres:17-alpine` no docker-compose; precisa
    acesso (VPN/firewall) ao SQL Server real, OU container `mcr.microsoft.com/mssql/server`
    como alternativa local pesada
- **Why:** opção única tecnicamente viável dado o constraint corporativo.

### Option 3: Dual-mode (PG local + SQL Server homolog/prod) via abstração
- **Pros:**
  - Preserva docker-compose local enxuto pra dev
  - Permite testes locais sem VPN
- **Cons:**
  - Repositórios viram interfaces com duas implementações — duplicação de
    cada query em dois dialetos
  - SQL diferente entre os ambientes esconde bugs específicos de um dialeto
    até produção
  - Migrations duplicadas: cada change vira 2 arquivos
  - Manter dois caminhos a longo prazo é dívida
- **Por que não:** custo de manutenção desproporcional ao benefício; o ganho
  de "rodar local sem VPN" pode ser resolvido com container MSSQL local
  quando necessário (sem precisar de dois dialetos no código).

## Consequences

### Positive
- Conformidade com infraestrutura corporativa (TI, backup, monitoração)
- Senha do banco fora do controle do gateway (vault gerenciado)
- Schema dedicado evita acoplamento com outras aplicações que vivem no mesmo
  servidor
- Stack alinha com prática Microsoft: caso evolua para Azure SQL Database
  ou Managed Instance, driver e ferramentas continuam idênticos
- Defesa em profundidade: migration 009 ganhou self-heal antes da troca,
  então o mesmo padrão será aplicado nas migrations T-SQL portadas (CTE
  cleanup + idempotência via `IF NOT EXISTS`)

### Negative / Trade-offs
- Reescrita arquitetural ampla (~30 arquivos tocados em uma única frente)
- Perda da elegância do pgx (`Conn.QueryRow(...).Scan(...)` é igual, mas a
  ergonomia geral do pgx é superior)
- Tipos JSON: SQL Server tem `JSON` (SQL Server 2025+) ou `NVARCHAR(MAX)` +
  funções JSON (SQL Server 2016+). Para portabilidade, usaremos `NVARCHAR(MAX)`
- Local dev sem PG: dev sem VPN não roda gateway com banco real até montarmos
  alternativa local (futura ADR se necessário)
- O dia 27/05/2026 vê **dois grandes movimentos**: a entrega da Onda 6 e a
  troca de banco. Risco de regressão composto — mitigação: commitar Onda 6
  + fix Bug 1 ANTES de iniciar a troca (checkpoint git limpo)

### Mitigations
- Migrations PG preservadas em `migrations/postgres-legacy/` (referência
  para qualquer rollback ou comparação)
- Migration `001_init.up.sql` em T-SQL inclui `CREATE SCHEMA IF NOT EXISTS
  gogateway` para idempotência
- Migration de cleanup de duplicatas (Bug 1) é portada para T-SQL com a
  mesma lógica self-healing
- Smoke test obrigatório em homologação antes do release: bootar gateway,
  criar app pela UI, autenticar request `/v1/proxy/...`, ver `usage_events`
  recebendo linhas
- ADR fica `proposed` até o smoke test passar; só então vira `accepted`

## Implementation plan (resumo)

Ver `docs/handoff.md` (a ser atualizado) para o plano detalhado em 6 fases:

| Fase | Conteúdo | Estado git esperado ao fim |
|---|---|---|
| 0 | Commits pendentes (Onda 6 + fix Bug 1); senha cadastrada no KV | working tree limpo, novo secret no vault |
| 1 | ADR + atualização do CLAUDE.md §4.2/§4.5/§9 | commit de docs |
| 2 | Driver swap, config layer | `go vet` + `go build` verde, sem testes de runtime |
| 3 | Migrations T-SQL portadas (schema gogateway) | tabelas criadas em homolog |
| 4 | Repos infra reescritos (`internal/infra/mssql/`) | `go test -race` verde |
| 5 | Documentação (SPEC, how-it-works, local-dev, keyvault-setup, roadmap, handoff) | docs sincronizados |
| 6 | Smoke test ao vivo, ADR → accepted | release-ready |

## References

- Microsoft Go driver: https://github.com/microsoft/go-mssqldb
- golang-migrate SQL Server driver: https://github.com/golang-migrate/migrate/tree/master/database/sqlserver
- T-SQL data types: https://learn.microsoft.com/en-us/sql/t-sql/data-types/data-types-transact-sql
- SQL Server JSON support: https://learn.microsoft.com/en-us/sql/relational-databases/json/json-data-sql-server
- ADR-0009 — DB-backed admin plane (esquema lógico das tabelas)
- ADR-0018 — Azure Key Vault como provider de segredos (mecânica do `${kv:NAME}`)
- ADR-0021 — Latency breakdown observável (tabela usage_events ganhou colunas; precisa portar)
- SPEC.md §8 — schema autoritativo (a ser reescrito após Fase 5)
- CLAUDE.md §4.2 — stack autorizada (a ser atualizada na Fase 1)
