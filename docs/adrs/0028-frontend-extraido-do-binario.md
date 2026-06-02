# ADR-0028: Frontend React+Vite extraído do binário Go

- **Status**: proposed
- **Date**: 2026-06-02
- **Decision makers**: Daniel (owner)
- **Consulted**: Claude Opus 4.7
- **Supersedes**: ADR-0014 (Frontend React+Vite embedado)
- **Aplica-se a**: `web/`, `cmd/gateway/main.go:390`, `internal/api/router.go:104+`

## Context

ADR-0014 (2026-Q1) decidiu embedar o console React+Vite no binário Go via
`go:embed`. Os motivos originais eram: simplicidade de deploy (1 binário),
sem CORS, sem terminator separado pra UI.

A realidade evoluiu:

- Mudanças cosméticas no console forçam rebuild + redeploy do `gateway.exe`,
  acoplando time de UI ao time de runtime
- Owner declarou em 2026-06-02 que o produto ganha quantum lógico separado
  pro admin .NET (ADR-0027); frontend embedado no Go ficaria amarrado a um
  dos quanta sem motivo arquitetural
- Driver Conway mudou: a UI fica naturalmente sob domínio de equipe
  frontend especializada quando assumida — embed em Go bloqueia esse alinhamento
- Deploy alvo passou a ser **Linux + Docker** com nginx terminator
  (ADR-0030); nginx já está hosting estático nativo, hosting da SPA é trivial

Esta ADR formaliza a reversão da decisão da ADR-0014.

## Decision

Remover `go:embed` do binário gateway e fazer do console uma **aplicação
standalone**:

- Console move-se de `web/` para `apps/console/` (ADR-0030)
- Build resulta em `dist/` estático servido por **nginx Alpine** num container
  separado
- `vite.config.ts` ganha `VITE_API_BASE` (default `/admin/v1` quando atrás de
  nginx terminator) — não precisa hardcode de origem
- nginx terminator roteia:
  - `/` (raiz) e `/assets/*` → container `console`
  - `/admin/v1/*` → container `admin-api` (.NET)
  - `/v1/*` → container `gateway` (Go)
- Mesma origem na URL exposta ao browser → **sem CORS necessário**
- `cmd/gateway/main.go:390` deixa de chamar `web.Handler()`
- `internal/api/router.go:104+` deixa de montar `/ui/*`
- Arquivo `web/embed.go` e referências em `go.mod`/`go.sum` removidos
- ADR-0014 vira `superseded by ADR-0028`

## Options considered

### Option 1: Manter `go:embed` (status quo)
- **Pros**: 1 binário; deploy mais simples
- **Cons**: release coupling forte; squad frontend não pode evoluir independente
- **Why not**: bloqueia a virada arquitetural da ADR-0027

### Option 2: Frontend standalone servido por nginx (CHOSEN)
- **Pros**: redeploy independente; squad frontend autônoma; nginx já é o terminator (zero infra extra); sem CORS (mesma origem); cache de assets otimizável por header `Cache-Control` no nginx
- **Cons**: 1 container a mais; build pipeline separado pra `apps/console/`
- **Why chosen**: alinhado com Conway + redução de release coupling

### Option 3: Frontend num CDN
- **Pros**: latência global ↓; cache CDN
- **Cons**: deploy mais complexo (origem separada); console é uso interno corp, CDN não justifica; CORS volta ao desenho
- **Why not**: complexidade desproporcional ao ganho — console serve usuários da intranet

### Option 4: Frontend embedado na admin-api .NET
- **Pros**: ainda single deployable (.NET + estático)
- **Cons**: amarra console à equipe admin .NET; viola separação de quantum lógico que a ADR-0027 pretende; não habilita squad frontend
- **Why not**: equivale a "mudar de Go pra .NET sem ganhar separação"

## Consequences

### Positive

- Squad frontend pode evoluir UI sem coordenar com Go ou .NET
- `gateway.exe` fica menor (sem `dist/`)
- nginx serve estáticos com cache headers otimizados (Vite gera nome-com-hash em `dist/assets/`)
- TLS terminator único (nginx) — sem certificado embutido no binário Go
- Habilita futuras otimizações (lazy chunks, SSR opcional) sem tocar runtime
- Pré-requisito da ADR-0027 (sem extrair frontend, migração de admin pra .NET fica esquisita)

### Negative / Trade-offs

- 1 container/serviço a mais no Docker Compose
- Atualização de versão exige rebuild + push de 2 imagens (console + nginx config se mudar rota)
- Em dev local, owner agora roda `npm run dev` + binário Go separados (igual já era em dev moderno)

### Mitigations

- Dockerfile do console é leve (multi-stage: Node build → `nginx:alpine` final com só `dist/`)
- `apps/console/.env.development` documenta `VITE_API_BASE=http://localhost:8080` pra dev local sem terminator
- nginx config no repo (`infra/nginx/nginx.conf`) versionada com path filters de CI futuras

## References

- ADR-0014 — Frontend embedado (superseded por esta)
- ADR-0027 — Admin .NET (esta ADR é pré-requisito daquela)
- ADR-0030 — Monorepo (define onde fica `apps/console/`)
- Plano: `/home/daniel/.claude/plans/n-o-quero-usa-ef-glowing-locket.md` Fase 2
- [[KB/AI-Gateway/Melhorias-Arquitetura]] §4.2 — "Opção B — Frontend extraído como quantum próprio"
- Vite multi-environment build: https://vitejs.dev/guide/env-and-mode
- nginx serving SPA com fallback: https://router.vuejs.org/guide/essentials/history-mode.html#nginx (mesmo padrão pra React Router)
