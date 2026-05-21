# Plano de teste — console de administração

Checklist manual para validar o console antes de cada release. Cada caso é
binário (passou / falhou) e tem o passo de reprodução curto.

Atualizar este documento conforme novos lotes entregam features.

## Pré-requisitos

- Gateway rodando em `http://localhost:8080` (ou ambiente equivalente)
- Postgres com migrations aplicadas
- Pelo menos um admin criado via `cmd/admin-create`
- Browser moderno (Chrome 120+, Firefox 121+, Safari 17+, Edge 120+)
- DevTools acessível (F12)

---

## 1. Smoke tests — caminho feliz mínimo

| # | Cenário | Esperado |
|---|---|---|
| 1.1 | Visitar `/` | Redireciona para `/ui/` |
| 1.2 | Visitar `/ui/` sem sessão | Redireciona para `/ui/login` |
| 1.3 | Login com credenciais válidas | Vai para `/ui/dashboard`, mostra resumo |
| 1.4 | Login com senha errada | Erro "Usuário ou senha inválidos" — sem distinção entre user inexistente e senha errada (anti-enumeração) |
| 1.5 | 6 tentativas falhadas rápidas | 429 Too Many Requests com `Retry-After` |
| 1.6 | Logout pelo menu do header | Sessão limpa, redirect para `/ui/login` |
| 1.7 | Recarregar com sessão ativa | Mantém logado (sessionStorage) |
| 1.8 | Fechar e reabrir aba | Pede login de novo (sessionStorage limpa) |
| 1.9 | Token expira (passar 8h) | Próxima ação dispara 401, AuthGuard redireciona para login |

## 2. Navegação

| # | Cenário | Esperado |
|---|---|---|
| 2.1 | Clicar em cada item da sidebar | Rota muda; item ativo destacado em violeta |
| 2.2 | Como **viewer**: ver Aplicações/Endpoints/Usuários na sidebar | Não aparecem (só Dashboard + Observabilidade) |
| 2.3 | Como **operator**: ver Usuários na sidebar | Não aparece |
| 2.4 | Como **operator**: acessar `/ui/users` digitando URL | Redireciona para dashboard |
| 2.5 | Breadcrumbs em listagens | "Aplicações" / "Endpoints" / etc., último em branco |
| 2.6 | Breadcrumbs em detail page | "Aplicações > Detalhes" — primeiro item linka de volta |
| 2.7 | Atalho `Ctrl+K` / `Cmd+K` | Foca o input de busca da tabela ativa |
| 2.8 | `Esc` com input focado | Desfoca |

## 3. CRUD — Aplicações

| # | Cenário | Esperado |
|---|---|---|
| 3.1 | Criar aplicação válida | 201, modal de token aparece com `gwk_…` |
| 3.2 | Copiar token | Botão Copiar funciona, toast "Token copiado" |
| 3.3 | Fechar modal de token | Token desaparece; criar outra app não vaza o anterior |
| 3.4 | Criar com nome duplicado | Mensagem de erro clara, modal continua aberto |
| 3.5 | Criar com tier inválido (via API) | 400 invalid_tier |
| 3.6 | Editar via ação "Editar" | Modal pré-preenchido; salvar atualiza a linha |
| 3.7 | Editar e cancelar | Lista permanece igual |
| 3.8 | Rotacionar chave | Confirmação → token novo (uma vez) → toast |
| 3.9 | Desativar app | Confirmação → linha vira "Inativa" |
| 3.10 | Buscar por nome | Filter inline (case-insensitive, substring) |
| 3.11 | Buscar com texto que não bate | "Nenhuma aplicação corresponde ao filtro" |
| 3.12 | Botão Atualizar | Refaz fetch, mostra spinner |
| 3.13 | Clicar no nome da app | Vai para `/ui/applications/:id` |

## 4. Detail — Aplicação

| # | Cenário | Esperado |
|---|---|---|
| 4.1 | Acessar `/ui/applications/9999` (inválido) | Toast "Aplicação não encontrada", redireciona |
| 4.2 | Aba "Detalhes" | Mostra ID, tier, RPM/TPM, budget, modelos, timestamps |
| 4.3 | Aba "Uso recente" sem dados | "Nenhuma requisição registrada nas últimas 24h" |
| 4.4 | Aba "Uso recente" com dados | Tabela com modelo, tokens, latência, custo, status |
| 4.5 | Aba "Auditoria" | Tabela com event_type, severity, metadata |
| 4.6 | Aba "Acessos" | Lista de endpoints com toggle de grant |
| 4.7 | Conceder grant via toggle | Estado muda; toast "Acesso concedido" |
| 4.8 | Revogar grant via toggle | Estado muda; toast "Acesso revogado" |
| 4.9 | Filtrar endpoints na aba Acessos | Filter funciona |
| 4.10 | Botões Rotacionar / Desativar no header | Mesmos comportamentos da listagem |

## 5. CRUD — Endpoints

| # | Cenário | Esperado |
|---|---|---|
| 5.1 | Criar endpoint com slug `meu-endpoint` | 201, aparece na lista |
| 5.2 | Slug com caracteres inválidos | (não há validação client-side ainda — backend aceita; documentar) |
| 5.3 | Editar endpoint | Modal pré-preenchido; salvar atualiza |
| 5.4 | Desativar endpoint | Status muda |
| 5.5 | Detail page → aba Targets | Vazia: "Sem targets cadastrados" |
| 5.6 | Adicionar target tipo `none` | Aparece na tabela; auth_type=none |
| 5.7 | Adicionar target tipo `bearer_token` | Form aparece; após salvar credencial fica cifrada |
| 5.8 | Adicionar target tipo `api_key_header` | Form pede header+value |
| 5.9 | Adicionar target tipo `basic_auth` | Form pede user+pass |
| 5.10 | Editar target | URL e peso atualizam; auth_type pode mudar |
| 5.11 | Remover target | Linha some |
| 5.12 | Aba Acessos | Lista todas apps com toggle; conceder/revogar funciona |

## 6. CRUD — Usuários (admin only)

| # | Cenário | Esperado |
|---|---|---|
| 6.1 | Criar usuário viewer | Aparece na lista, papel "viewer" |
| 6.2 | Criar com senha <8 chars | Erro de validação |
| 6.3 | Criar com username duplicado | Erro do backend (23505) |
| 6.4 | Desativar outro usuário | Status muda; sessões revogadas |
| 6.5 | Tentar desativar a si mesmo | (UI deveria bloquear — verificar se permite no momento) |
| 6.6 | Login do usuário recém-criado | Funciona com a senha definida |

## 7. Observabilidade (viewer+)

| # | Cenário | Esperado |
|---|---|---|
| 7.1 | Aba Uso sem dados | Mensagem "Nenhum evento no intervalo" |
| 7.2 | Aba Uso com dados | Tabela popula; status code colorido (verde/amber/vermelho) |
| 7.3 | Filtrar por intervalo de tempo | Apenas eventos no intervalo aparecem |
| 7.4 | Filtrar por aplicação | Apenas eventos da app |
| 7.5 | Limit acima de 1000 | Erro 400 |
| 7.6 | Aba Auditoria | Filtros adicionais: event_type |
| 7.7 | Aba Budget | Período YYYYMM aplica filtro |
| 7.8 | Período inexistente | "Sem dados para o período" |

## 8. Permissões — matriz

Cada papel deve ver/atuar diferentemente. Logue com cada um:

| Ação | viewer | operator | admin |
|---|:---:|:---:|:---:|
| Ver Dashboard | ✓ | ✓ | ✓ |
| Ver Observabilidade | ✓ | ✓ | ✓ |
| Ver Aplicações | ✗ | ✓ | ✓ |
| Criar/Editar Aplicação | ✗ | ✓ | ✓ |
| Rotacionar chave | ✗ | ✓ | ✓ |
| Ver/Editar Endpoints | ✗ | ✓ | ✓ |
| Gerenciar grants | ✗ | ✓ | ✓ |
| Ver/Criar/Desativar Usuários | ✗ | ✗ | ✓ |
| Logout | ✓ | ✓ | ✓ |

## 9. Edge cases e robustez

| # | Cenário | Esperado |
|---|---|---|
| 9.1 | Backend retorna lista vazia | Mensagem de empty state (sem tela preta) |
| 9.2 | Backend retorna 500 | Toast vermelho com mensagem; estado anterior preservado |
| 9.3 | Backend retorna 401 | sessionStorage limpa; redirect /login |
| 9.4 | Backend retorna 429 | Toast "Muitas tentativas" — só no /login no momento |
| 9.5 | Network offline | fetch error → toast "Não foi possível conectar" |
| 9.6 | JSON malformado da API | ApiError com message "request failed with status N" |
| 9.7 | Erro de render React | ErrorBoundary mostra fallback com stack — não tela preta |
| 9.8 | URL com ID inválido (`/applications/abc`) | Alert "ID inválido", link de volta |
| 9.9 | URL com ID inexistente (`/applications/9999`) | Toast + redirect para listagem |
| 9.10 | Refresh durante request em vôo | Sem orfãos; spinner para; sem warnings |

## 10. Performance — pontos de atenção

| # | Cenário | Esperado |
|---|---|---|
| 10.1 | Carregar página com 100 aplicações | Render < 200ms (depois de fetch) |
| 10.2 | Carregar Observabilidade com 200 eventos | Render < 300ms |
| 10.3 | Digitar busca | Filter local — sem flicker, sem fetch |
| 10.4 | Bundle inicial | < 500 KB minificado (alvo) — hoje ~420 KB |
| 10.5 | Memory leak ao navegar 50× entre páginas | Não cresce indefinidamente (DevTools → Performance Memory) |

## 11. Browser compat

Testar fluxo de login + 1 ação CRUD em cada:

- [ ] Chrome 120+ (Linux/Mac/Windows)
- [ ] Firefox 121+
- [ ] Safari 17+ (macOS, principalmente para validar `useSyncExternalStore` no WebKit antigo)
- [ ] Edge 120+ (Chromium — atalho `Ctrl+K`)

## 12. Acessibilidade básica

| # | Cenário | Esperado |
|---|---|---|
| 12.1 | Tab navega por todos os controles | Ordem lógica; foco visível |
| 12.2 | Inputs têm `<Label>` associado | Lighthouse a11y ≥ 90 |
| 12.3 | Botões só-ícone têm `aria-label` | Sim em Refresh, ações de linha |
| 12.4 | Contraste de texto | WCAG AA mínimo |
| 12.5 | Reduce motion (OS) | Animações suaves; nada vibrante |

## 13. Segurança — verificações visuais

| # | Cenário | Esperado |
|---|---|---|
| 13.1 | F12 → Application → Session Storage após login | Vê `ai_gateway_token`, `_expires`, `_role`. Hash do token nunca aparece |
| 13.2 | F12 → Network → Response do /login | Token está só na resposta, não em outros endpoints |
| 13.3 | F12 → Network → Headers de /healthz | CSP, X-Frame-Options, Permissions-Policy presentes |
| 13.4 | Tentar embed em `<iframe>` de outro site | Bloqueado (X-Frame-Options DENY) |
| 13.5 | Token aparece em algum log do console? | Não. Nada além de `key_prefix` |

---

## Automação futura

Quando o console for crítico, considerar:

| Ferramenta | Cobre |
|---|---|
| **Playwright** (recomendado) | E2E completo no browser; suporta multi-browser; gravação |
| **Vitest** + `@testing-library/react` | Unit + component tests (lib/, components/ isolados) |
| **MSW** (Mock Service Worker) | Stub do admin API para testes sem backend ativo |

Setup mínimo (futuro):
- `web/tests/e2e/` — Playwright specs por persona (viewer, operator, admin)
- `web/tests/unit/` — testes de lib (api.ts, auth.ts, filter.ts)
- CI: `pnpm test:e2e` rodando contra um container do gateway

Hoje o projeto **não tem testes automatizados de UI** — fase 1 prioriza
entrega. Antes de stress test no backend, é razoável fechar pelo menos a
suíte de smoke (cenários 1.x + 6.x) em Playwright para regression.

---

## Como reportar bug encontrado

1. Capturar: URL, papel logado, screenshot do console (F12)
2. Verificar se `ErrorBoundary` capturou (mostra stack) ou se foi runtime
3. Anotar na issue: passos de reprodução + comportamento esperado vs atual
4. Anexar payload da Network se for erro de API
