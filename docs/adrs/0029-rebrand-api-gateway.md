# ADR-0029: Rebrand de "AI Gateway" para "API Gateway"

- **Status**: proposed
- **Date**: 2026-06-02
- **Decision makers**: Daniel (owner)
- **Consulted**: Claude Opus 4.7
- **Supersedes**: nenhum (afeta naming, não decisão técnica anterior)
- **Aplica-se a**: nome do produto, repo, headers HTTP, README, eventualmente schema

## Context

O produto nasceu como "AI Gateway" — proxy de Azure OpenAI com governance.
Owner declarou em 2026-06-02:

> *"Ela precisa aceitar outros tipos de request além dos de IA, vamos focar
> nos endpoints REST nesse momento, depois vemos SOAP e GraphQL."*

A direção é **gateway de APIs corporativas genérico**, onde IA passa a ser
**um dos providers cadastráveis**, não o domínio do produto. Tecnicamente o
schema atual já suporta isso — ADR-0010 introduziu plano proxy genérico com
`provider_kind = "custom"` (passthrough HTTP). O bloqueio é simbólico: naming
empurra usuário pro "fluxo Azure OpenAI"; UI default é IA; ADRs e docs
assumem IA.

Naming consistente é pré-requisito pra:
- Console com fluxo neutro (REST genérico como caminho default; IA como preset)
- Pitch interno pra outras squads TP adotarem o gateway pra suas APIs
- Migração de admin pra .NET (ADR-0027) — vale mais investir em nome que
  amplia escopo do que em nome legado
- Futuro suporte SOAP/GraphQL/gRPC sem soar deslocado

## Decision

Renomear o produto para **"API Gateway"** (sem sufixo TP, sem "Corporate").

**Escopo do rename — V1 (junto com ADRs 0027/0028/0030):**

| Local | Antes | Depois |
|---|---|---|
| Nome do produto | AI Gateway | API Gateway |
| Repo local (pasta) | `/home/daniel/projects/ai-gateway/` | manter por enquanto; renomear pra `/api-gateway/` quando GH-rename concluir |
| Repo GitHub | `D4nRossi/ai-gateway` | rename via GitHub settings (preserva redirects) |
| README.md | título e descrição | "# API Gateway" |
| SPEC.md | seção §0 | "Nome: API Gateway" |
| CLAUDE.md | §0 Identidade | atualizar |
| Header HTTP `X-Gateway-*` | mantido | sem mudança (já neutro) |
| User-Agent default | `ai-gateway/{version}` | `api-gateway/{version}` |
| Solution / project names .NET | `APIGateway.Admin.*` | já adotado em ADR-0027 |

**Escopo do rename — V2 (decisão postergada):**

| Local | Comentário |
|---|---|
| Schema SQL `gogateway` | **mantido por ora** — rename de schema em DB de homolog é caro; impacta queries do Go atual + DbUp + tooling Dapper. Reavaliar quando todos os 3 quanta estabilizarem |
| Tabelas (`applications`, `proxy_endpoints`...) | **mantidas** — naming já é genérico (não tem `ai_models` ou `llm_providers`) |
| Imports Go (`github.com/D4nRossi/ai-gateway/...`) | reavaliar junto com mudança de pasta `apps/gateway/go.mod` |

**Console UI:**

- Texto institucional ("Bem-vindo ao AI Gateway") → "Bem-vindo ao API Gateway"
- Fluxo "Criar Endpoint": passa a destacar REST genérico como primeira opção;
  Azure OpenAI vira preset selecionável depois (não menu pai)
- Catálogo de "providers" vira catálogo de "API categories" (IA, REST, futuros)

## Options considered

### Option 1: Manter "AI Gateway"
- **Pros**: zero esforço; reconhecimento atual; histórico de ADRs consistente
- **Cons**: vocabulário fica esquisito conforme aceita REST/SOAP/GraphQL — "AI Gateway que aceita SOAP" não fecha
- **Why not**: bloqueia narrativa de produto

### Option 2: "TP API Gateway"
- **Pros**: marca explícita; identidade corp clara
- **Cons**: amarra reuso fora da TP; soa burocrático; depois você descobre que outra área da TP tem "TP API Gateway" deles
- **Why not**: prefiro neutro sem prefixo de área

### Option 3: "Corporate Gateway"
- **Pros**: foco em uso corporativo; ampara SOAP/gRPC futuramente
- **Cons**: ambíguo — "gateway corporativo" é qualquer coisa; pior SEO interno
- **Why not**: menos preciso que API Gateway

### Option 4: "API Gateway" (CHOSEN)
- **Pros**: descreve exatamente o que é; alinhado com terminologia da indústria (AWS API Gateway, Kong, Apigee); preserva possibilidade de reuso futuro
- **Cons**: nome genérico, não dá pra googlear o nosso especificamente
- **Why chosen**: alinhado com a direção do produto; baixo custo; alta clareza interna

### Option 5: Rename completo incluindo schema
- **Pros**: consistência total
- **Cons**: rename de schema em SQL Server requer migration delicada; toda query Go + Dapper afetada; risco operacional alto; ganho cosmético
- **Why not now**: V2 quando todos os quanta estabilizarem

## Consequences

### Positive

- Narrativa de produto fica honesta — IA é um caso, não o ponto
- Habilita pitch pra outras squads TP que têm APIs corporativas (não-IA)
- Console pode reorganizar fluxo de cadastro centrando REST genérico
- ADRs futuras (SOAP, GraphQL, gRPC) não precisam justificar "por que estamos colocando isso num produto chamado AI Gateway"
- Alinha com naming já adotado em ADR-0027 pra projetos .NET (`APIGateway.Admin.*`)

### Negative / Trade-offs

- Repo GitHub rename quebra links externos (mitigado por redirects do GH por 1 ano)
- Documentação interna TP precisa rebrand passar por validação
- ADRs anteriores citam "AI Gateway" — não vão ser editadas (CLAUDE.md §1.5: "refatoração estética só com permissão explícita"). Owner aceita "ADR antiga citará nome antigo, ADRs novas citam nome novo"
- Schema continua `gogateway` — incoerência cosmética persiste

### Mitigations

- README.md ganha nota: "Antes chamado **AI Gateway**; rebrand em 2026-06-02 pra refletir escopo genérico de APIs corporativas"
- GitHub rename feito com `Old name` redirect (default)
- `User-Agent` antigo (`ai-gateway/{version}`) aceito durante 1 ciclo de release por compatibilidade (sem ação ativa; só não quebrar parsers externos se existirem)
- Schema rename fica como item em `docs/roadmap.md` pra V2

## References

- ADR-0010 — Proxy genérico (base técnica que viabiliza esta ADR)
- ADR-0027 — Admin .NET (companion; nome `APIGateway.Admin.*` já adota)
- ADR-0028 — Frontend extraído (companion)
- ADR-0030 — Monorepo (companion)
- Plano: `/home/daniel/.claude/plans/n-o-quero-usa-ef-glowing-locket.md` §"Generalização REST (não-IA)"
- [[KB/AI-Gateway/Melhorias-Arquitetura]] §"Naming"
- GitHub repo rename redirects: https://docs.github.com/en/repositories/creating-and-managing-repositories/renaming-a-repository
