# Console de administração — roadmap de features

Lotes incrementais. Cada lote é fechado com commit + validação (`go build`,
`go vet`, `npm audit`, `npm run build`).

## Status

| Lote | Tema | Status | Entregue em |
|---|---|---|---|
| **A** | Quality of life (search, refresh, detail pages, breadcrumbs, Cmd+K) | ✅ | 2026-05-21 |
| **A.6** | Provider Catalog (multi-provider, defaults inteligentes) | ✅ | 2026-05-22 |
| B | Gestão de Models (CRUD + pricing) | ⏳ | — |
| C | Dashboards visuais (gráficos timeseries) | ⏳ | — |
| D | Segurança avançada (sessões, mudança de senha, 2FA opcional) | ⏳ | — |
| E | Alertas & exports (thresholds, webhook, CSV) | ⏳ | — |
| F | Operação avançada (drain mode, multi-key, IP allowlist) | ⏳ | — |
| G | Provider management (multi-Azure, failover) | ⏳ | — |

---

## Lote A.6 — Provider Catalog (entregue 2026-05-22)

Decisão registrada em [ADR-0016](adrs/0016-provider-catalog.md).

### Backend
- `domain/endpoint.ProviderKind` enum + 10 providers + `custom`
- Migration `005_provider_kind`: `ALTER TABLE proxy_endpoints` adiciona coluna
  com CHECK constraint, default `custom`, índice parcial
- `adminservice.CreateEndpoint/UpdateEndpoint` validam o valor; `ErrInvalidProvider`
- Repository CRUD inclui o campo nas queries e nos helpers de scan
- Handler expõe `provider_kind` em request/response JSON; HTTP 400 em valor
  inválido

### Frontend
- `web/src/lib/providers.ts` — catálogo completo (label, URL base, auth method,
  hint, ícone, link de doc, cor de badge)
- `ProviderSelector` — grid de cards selecionáveis (substitui dropdown seco)
- `ProviderBadge` — versão compacta para listagem e detail header
- Listagem ganha coluna **Provider** com badge colorido por accent
- Detail page mostra badge + link "docs ↗" do provider
- Form de endpoint: provider em destaque no topo; selecionar pré-preenche
  nome e LB strategy
- Form de target: ao adicionar target em endpoint não-custom, URL base e
  tipo de auth vêm preenchidos com defaults do catálogo
- Busca da listagem agora cobre `provider_kind` + label do provider

### Limitações (metadata-only nesta fase)
- O motor de proxy **continua passthrough total** — provider_kind é apenas
  metadata. Não há tradução de payload (cliente fala a "língua" do provider
  escolhido)
- Catálogo cobre 10 providers comuns + custom; AWS Bedrock e Vertex AI ficam
  para um Lote H futuro (precisam de auth complexa: Sig v4 / OAuth)

### Validação
```
go vet ./...        → 0 issues
go build ./...      → OK
npm run build       → 428 KB JS (+ 8 KB do catálogo)
npm audit           → 0 vulnerabilidades
```

---

## Lote A — Quality of life (entregue 2026-05-21)

### Backend
- `endpoint.Repository.ListGrantedEndpointIDs(applicationID)` — lista
  endpoints aos quais uma aplicação tem acesso (inversa do
  `ListGrantedApplicationIDs` existente)
- `adminservice.Service.ListEndpointGrants(applicationID)` — hidrata IDs em
  objetos `ProxyEndpoint` completos
- `GET /admin/v1/applications/{id}/grants` — handler HTTP correspondente

### Frontend
- **DataTableToolbar** — componente reutilizável com search inline (filter
  client-side) + botão refresh + slot pra ações; expõe ref imperativa
  para o atalho global
- **Cmd+K / Ctrl+K** — listener global em `useKeyboardShortcuts` foca a
  search da tabela ativa; Esc desfoca
- **Breadcrumbs dinâmicos** — substituem o título estático do header;
  detail pages declaram seu label via `Route handle.crumb`
- **`/applications/:id` — ApplicationDetail** com 4 tabs:
  - **Detalhes**: ID, tier, RPM/TPM, budget, modelos permitidos, timestamps
  - **Uso recente**: tabela de `usage_events` filtrada pela aplicação (24h)
  - **Auditoria**: tabela de `audit_events` filtrada pela aplicação
  - **Acessos**: matriz de toggles (endpoint × switch) com search inline
- **`/endpoints/:id` — EndpointDetail** com 3 tabs:
  - **Detalhes**: slug, estratégia LB, limites, contagem de targets
  - **Targets**: CRUD inline (substitui o modal que vivia na lista)
  - **Acessos**: matriz inversa (aplicação × switch)
- **Linkagem nas listas** — nomes e slugs viram link para o detalhe;
  dropdowns ganham item "Ver detalhes"
- **Filter inline + empty state** — toda lista agora exibe "Nenhum X
  corresponde ao filtro" quando search não bate

### Bug fix incluso
- React error #185 (Maximum update depth exceeded) no `useSession`. Causa:
  `getSession()` retornava um novo objeto a cada chamada → `useSyncExternalStore`
  comparava com `Object.is` → loop infinito. Fix: cache de snapshot com chave
  derivada do conteúdo do sessionStorage, invalidada por
  `setSession`/`clearSession` (emit) e pelo evento `storage` (cross-tab).

### Limitações conhecidas (a resolver em lotes seguintes)
- Edição de aplicação/endpoint ainda só pela tela de lista (modal). Lote D
  vai mover pra detail page com edição inline.
- `GrantsPanel` do EndpointDetail faz N+1 (`listGrants(app.id)` para cada
  aplicação). Aceitável para <50 apps; quando crescer, criar endpoint
  dedicado `GET /admin/v1/endpoints/{id}/grants`.
- Detail pages não revalidam automaticamente após mutação por terceiros
  (precisa refresh manual). Lote C vai introduzir SWR-like revalidation.

### Validação
```
go vet ./...        → 0 issues
go build ./...      → OK
npm run build       → 418 KB JS / 31 KB CSS / 0 errors
npm audit           → 0 vulnerabilidades
```

---

## Próximos lotes (resumo)

### Lote B — Gestão de Models
Hoje os models vêm do `gateway.yaml`. Migrar pro DB destrava admin completo:
nova tabela `models`, CRUD admin, mapeamento `public_name → deployment_name`,
preço por 1k tokens (input/output), tiers que podem usar cada model,
status (ativo/depreciado), health check contra Azure.

### Lote C — Dashboards visuais
Gráficos de timeseries (request rate, latência p50/p95/p99, erro rate) nas
últimas 24h; gauge de budget; top apps por gasto; heat map por hora.
Lib: **recharts** (leve, embed-friendly, sem CDN).

### Lote D — Segurança avançada
Sessões ativas (listar + revogar individualmente), mudança de senha
self-service, force-logout em mudança de senha, 2FA TOTP opcional, edição
inline nas detail pages.

### Lote E — Alertas & exports
Thresholds por aplicação (80%/95% budget) com webhook/email; status do
PromptShield/CS; export CSV/JSON de usage/audit/budget; snapshot da
configuração inteira.

### Lote F — Operação avançada
Drain mode, bulk operations, rotação programada de keys, multi-key (rolling
rotation sem downtime), IP allowlist do admin plane, limits per-endpoint.

### Lote G — Provider management
Múltiplos endpoints Azure (regiões), failover automático, pricing comparison
entre regions, per-application provider preference.
