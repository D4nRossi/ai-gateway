# CRUD Gap Analysis — fechamento antes de stress test

Inventário do que **está** vs. o que **falta** por entidade. Critério de
"100% CRUD" = Create, Read (lista + detail), Update (in-place ou modal),
Delete (soft ou hard), mais ações específicas relevantes (rotate, grant,
target mgmt, etc.).

## Estado por entidade (2026-05-21)

### Aplicações — **95%**
| Operação | Status | Onde |
|---|:---:|---|
| Criar | ✅ | Modal na lista |
| Listar | ✅ | `/applications` |
| Ler (detail) | ✅ | `/applications/:id` — 4 tabs |
| Editar | ✅ | Modal (ainda na lista, não inline) |
| Soft-delete | ✅ | "Desativar" |
| Rotacionar API key | ✅ | Detail + lista |
| Grants (vincular endpoint) | ✅ | Detail aba "Acessos" |
| Histórico de keys rotacionadas | ❌ | Backend mantém, UI não mostra |

Gap: ver histórico de rotações de chave (tabela `api_keys` tem `rotated_at`).

### Endpoints — **100%**
| Operação | Status | Onde |
|---|:---:|---|
| Criar | ✅ | Modal na lista |
| Listar | ✅ | `/endpoints` |
| Ler (detail) | ✅ | `/endpoints/:id` — 3 tabs |
| Editar | ✅ | Modal |
| Soft-delete | ✅ | "Desativar" |
| Targets CRUD | ✅ | Detail aba "Targets" |
| Grants (apps autorizadas) | ✅ | Detail aba "Acessos" |

### Usuários admin — **60%**
| Operação | Status | Onde |
|---|:---:|---|
| Criar | ✅ | Modal na lista |
| Listar | ✅ | `/users` |
| Ler (detail) | ❌ | Não existe rota |
| Editar (mudar role, ativar/desativar) | ❌ | Backend tem `UpdateUser`, UI não expõe |
| Mudar senha | ❌ | **Nem backend tem endpoint admin** (só `cmd/admin-create` cria) |
| Soft-delete | ✅ | "Desativar" |
| Forçar logout (revogar sessões) | ✅ | Acontece automaticamente em desativar |
| Listar sessões ativas | ❌ | Backend tem repo, UI não expõe |

Gaps:
- Editar usuário (mudar role, reativar) — backend pronto, falta UI
- Mudar senha de outro usuário (admin reset) — backend não tem endpoint, UI também não
- Mudar a própria senha (self-service) — não existe
- Ver sessões ativas — Lote D já considerava

### Models — **0%**
| Operação | Status | Onde |
|---|:---:|---|
| Tudo | ❌ | Toda configuração de model vive em `gateway.yaml`. Sem CRUD via console. |

Esse é o **Lote B inteiro**. Migração não-trivial porque:
- Auth middleware da rota `/v1/chat/completions` consulta `ModelByName` em memória (config)
- Pricing config (input/output BRL por 1k tokens) também só no YAML
- Migração precisa: nova tabela `models`, repository, service, handler, UI

### Configurações globais — **0%**
| Item | Status |
|---|:---:|
| Pricing rules | ❌ (YAML) |
| Tier definitions (quais guardrails por tier) | ❌ (hardcoded) |
| PromptShield / Content Safety config | ❌ (YAML) |
| Rate limit globais | ❌ (YAML) |

---

## Proposta de lote de fechamento — "Lote A.5: closing CRUD"

Antes de C (dashboards) e antes de stress test, fechar os gaps que **bloqueiam
operação real** sem editar arquivo do servidor:

### A.5.1 — Editar usuário (UI, backend já tem)
- POST → **PUT** `/admin/v1/users/{id}` (já existe `UpdateUser` no service)
- Form modal: mudar role + ativar/desativar
- Botão "Editar" no dropdown da linha
- Custo: ~1h

### A.5.2 — Reset de senha pelo admin
- Backend novo: `POST /admin/v1/users/{id}/reset-password` (admin role)
- adminservice: `ResetPassword(userID, newPassword string)` — bcrypt + UpdateUser
- UI: ação "Redefinir senha" → modal com password + confirm
- Reusa modal "show once" do rotate-key se quiser exibir senha gerada
- Custo: ~1.5h

### A.5.3 — Mudança de senha self-service
- Backend novo: `POST /admin/v1/me/password` (qualquer role logado)
- Body: `{current_password, new_password}` — valida current via bcrypt
- Força revogação de todas as outras sessões (mantém a atual)
- UI: novo item no menu do header "Trocar senha"
- Custo: ~1.5h

### A.5.4 — Histórico de rotações de chave
- Backend novo: `GET /admin/v1/applications/{id}/keys` retorna histórico
- UI: nova tab "Histórico" na ApplicationDetail
- Custo: ~1h

### A.5.5 — Edição inline nas detail pages
- Mover modal de edição da lista para a aba "Detalhes" das detail pages
- Form direto na tab, com botão Salvar/Cancelar
- Custo: ~2h

### Total estimado: **~7 horas** para fechar CRUD de Apps + Endpoints + Users.

---

## Lote B — Models DB-backed (separado)

Pode esperar. Justificativa:
- Hoje funciona via YAML (sysadmin edita + restart)
- Stress test do gateway não depende disso
- Implementação é grande: nova tabela + repo + service + UI + migration da config

Se for pra demo amanhã, **não fazer agora**. Deixar como Lote B dedicado depois.

---

## Recomendação de ordem

Fechar primeiro **A.5** (CRUD operacional 100%), depois **plano de teste de
UI executado**, então **stress test do backend**. Lote B (Models) e Lote C
(Dashboards) vêm em paralelo a partir daí.

### Sequência proposta

1. **A.fix (hoje)** — backend null→[], frontend defensivo ✓
2. **A.5** — fechar CRUD operacional (~7h)
3. **Executar UI test plan** — bater o checklist deste documento
4. **Stress test** — k6/vegeta no gateway + dashboards de carga
5. **C** — gráficos no console (com dados reais já gerados pelo stress test)
6. **B** — Models DB-backed
7. **D, E, F, G** — conforme docs/console-roadmap.md
