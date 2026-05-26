# ADR-0018: Azure Key Vault como provider de segredos

- **Status**: accepted
- **Date**: 2026-05-26
- **Decision makers**: Daniel (owner)
- **Consulted**: Claude Opus 4.7

## Context

Hoje (`internal/config/config.go`) o gateway carrega segredos via variáveis de
ambiente expandidas no YAML (`${AZURE_OPENAI_API_KEY}`, `${DATABASE_URL}`,
`${DB_ENCRYPTION_KEY}`). Funciona em dev (`.env` local) e em prod (env vars
do container), mas tem três problemas:

1. **Auditoria**: quem leu o segredo? Quando? `.env` no disco não responde.
   Cloud secret manager registra cada `GetSecret` com identidade do chamador.
2. **Rotação**: trocar `AZURE_OPENAI_API_KEY` exige redeployar (ou bounce do
   container) para a env nova entrar em vigor. Um secret manager permite
   rotação centralizada — basta o cache local expirar.
3. **Postura**: segredos em texto plano em arquivos de configuração, mesmo
   em ambientes restritos, é mau cheiro recorrente em auditorias internas
   de segurança. Cloud secret manager é o padrão esperado.

O usuário tem um Key Vault próprio (`https://danieldev.vault.azure.net/`)
pronto para integração. A decisão é: como o gateway resolve segredos do KV
sem regredir em ergonomia, performance ou clareza de erros?

## Decision

Introduzir um **resolver de segredos do Azure Key Vault** que roda dentro
do `config.Load()`, **antes** do unmarshal do YAML, com a sintaxe:

```yaml
azure_openai:
  endpoint: ${AZURE_OPENAI_ENDPOINT}
  api_key:  ${kv:AZURE-OPENAI-API-KEY}
```

- `${VAR}` continua expandindo de env (status quo via `os.ExpandEnv`).
- `${kv:NAME}` é resolvido via Azure SDK (`azsecrets.Client.GetSecret`).
- Autenticação: **`DefaultAzureCredential`** — em dev usa `az login`; em
  prod usa Managed Identity do App Service/AKS automaticamente.
- Endereço do vault: `KEYVAULT_URI` no ambiente (ex.
  `https://danieldev.vault.azure.net/`).

### Cache em memória

`internal/infra/keyvault/client.Get` usa um cache `map+sync.RWMutex` com TTL
de **5 minutos** por entrada. Justificativa:

- Cada `GetSecret` no Azure KV custa **100-300ms** (HTTPS + AAD). Sem cache,
  o boot ficaria proporcional ao número de segredos × latência.
- Rotação de segredo no Azure propaga em até 5 min para o gateway — bom
  trade-off entre latência hot path e frescor de chave.
- Lazy refresh: o primeiro request pós-expiry paga o miss; sem goroutine
  proativa de prefetch (cardinalidade baixa, ~5 segredos, simplicidade vence).

### Fail-fast no boot

Se `KEYVAULT_URI` está setado e qualquer `${kv:NAME}` referenciado no YAML
falhar (vault inacessível, secret inexistente, permissão negada), o gateway
**não sobe** e loga o motivo. Justificativa: melhor uma falha barulhenta no
boot do que comportamento degradado e bug latente em produção.

Se `KEYVAULT_URI` NÃO está setado, qualquer `${kv:NAME}` no YAML também
falha o boot — clareza importa. Para dev sem KV, o operador remove o
`${kv:…}` do YAML ou seta `KEYVAULT_URI` apontando para um vault dev.

### Escopo desta onda

Migram para KV nesta entrega (todos os 4):

| Variável atual (env) | Sintaxe pós-KV (no YAML) |
|---|---|
| `AZURE_OPENAI_API_KEY` | `${kv:AZURE-OPENAI-API-KEY}` |
| `DB_ENCRYPTION_KEY` | `${kv:DB-ENCRYPTION-KEY}` |
| `DATABASE_URL` | `${kv:DATABASE-URL}` |
| `AZURE_CS_API_KEY` (opcional) | `${kv:AZURE-CS-API-KEY}` |

O endereço Azure (`AZURE_OPENAI_ENDPOINT`, etc.) **não é segredo** — fica
em env normal.

## Options considered

### Option 1: Manter env vars + `.env` (status quo)
- **Pros:** zero código novo, simplicidade máxima, funciona offline.
- **Cons:** sem auditoria, rotação manual, exposição em disco. Mau cheiro
  recorrente em auditoria de segurança.
- **Por que não:** o usuário explicitamente pediu integração com KV.

### Option 2: HashiCorp Vault
- **Pros:** vendor-neutral, roda on-prem e em qualquer cloud, comunidade
  enorme.
- **Cons:** operação extra (servidor Vault, política, sealing). Para um
  stack que já é 100% Azure, é overhead sem retorno. AAD/Managed Identity
  do KV resolve auth sem nenhum agente adicional.

### Option 3: AWS Secrets Manager
- **Pros:** mesma categoria do KV, API simples.
- **Cons:** stack não está na AWS. Misturar clouds só adiciona dependência
  cruzada de IAM, faturamento, downtime independente.

### Option 4 (chosen): Azure Key Vault com `${kv:NAME}` no YAML
- **Pros:**
  - Mesma cloud do Azure OpenAI — uma identidade (Managed Identity) cobre
    tudo em prod
  - `DefaultAzureCredential` resolve dev (`az login`) e prod (MI)
    transparentemente; zero código de auth no gateway
  - Sintaxe `${kv:NAME}` é simétrica ao `${VAR}` existente — operador
    aprende em 5 segundos
  - Cache LRU+TTL elimina o custo recorrente; degrada graciosamente para
    miss isolado durante refresh
  - Auditoria nativa: Azure Monitor já loga cada `GetSecret` com
    identidade, IP, timestamp — atende auditoria sem código no gateway
  - Sem agente extra, sem servidor extra, sem custo recorrente além do já
    pago no Azure
- **Cons:**
  - Dependência nova (2 pacotes do Azure SDK: `azidentity`, `azsecrets`).
    Aceito — Azure SDK é amplamente usado, mantido e versionado
  - Acopla mais um pedaço do stack à Azure. Já estávamos lá (Azure OpenAI,
    Content Safety) — irrelevante
  - Latência adicional de 100-300ms por miss de cache. Mitigado por TTL 5min
- **Why:** custo de integração é pequeno (2 deps + 1 pacote interno),
  retorno é alto (auditoria + rotação + postura). Encaixa naturalmente no
  loader de config existente.

### Option 5: `${kv:NAME?default=valor}` (default inline)
- **Pros:** permite fallback declarativo se KV falhar.
- **Cons:** sintaxe mais complexa, parser maior, e introduz "bug latente
  em prod" (decisão do user) — operador pode achar que estava lendo do KV
  mas estava no default há semanas.
- **Por que não:** decisão explícita do user por fail-fast.

## Consequences

### Positive
- Segredos saem do `.env` (uma fonte de leak a menos)
- Rotação no Azure propaga em ≤5 min sem deploy
- Auditoria automática via Azure Monitor
- Dev local sem segredos no disco (`az login` resolve identidade)
- Caminho aberto para Managed Identity em prod (zero credencial no
  container/pod)
- Sintaxe `${kv:NAME}` é trivial de operar — não exige treinamento

### Negative / Trade-offs
- Dev local sem `az login` quebra ao iniciar o gateway. Mitigação: doc
  clara em `docs/keyvault-setup.md`; alternativa de usar env vars normais
  se quiser rodar offline
- Boot fica ~500ms-1.5s mais lento (4 secrets × 100-300ms cada, paralelo
  ajuda). Aceitável — é uma vez por restart
- 2 deps novas no `go.mod` (azidentity + azsecrets)
- KV é single-point-of-failure no boot. Mitigação: KV tem SLA 99.99%; em
  caso de outage, o cache (se já populado e dentro do TTL) salva
  runtime — só boot novo durante outage falha

### Mitigations
- Doc `keyvault-setup.md` cobre: criar secrets via CLI, permissionamento
  RBAC do user (Key Vault Secrets User), `az login`, fallback para env
  em dev sem KV
- `KEYVAULT_URI` opcional — gateway sobe sem ele se nenhum `${kv:…}`
  estiver no YAML
- Cache lazy + TTL longo (5min) absorve interrupções breves

## Schema do cache

```go
type entry struct {
    value     string
    expiresAt time.Time
}

type Client struct {
    azClient *azsecrets.Client
    mu       sync.RWMutex
    cache    map[string]entry
    ttl      time.Duration  // default 5 * time.Minute
}
```

- `Get(ctx, name)`: leitura sob RLock, checa expiry; se miss/expirado,
  upgrade pra Lock, fetch do Azure, popula, retorna
- Cache compartilhado entre boot (resolve YAML) e runtime futuro
  (qualquer código que queira ler segredo on-demand)

## References

- ADR-0012 — AES-256-GCM at rest (DB_ENCRYPTION_KEY é uma das chaves
  migradas nesta onda)
- ADR-0017 — Path translation no proxy plane (não diretamente relacionado,
  mas o `api_key` Azure passa a vir do KV)
- Azure Key Vault REST API: https://learn.microsoft.com/azure/key-vault/secrets/
- Azure SDK for Go — azidentity: https://pkg.go.dev/github.com/Azure/azure-sdk-for-go/sdk/azidentity
- Azure SDK for Go — azsecrets: https://pkg.go.dev/github.com/Azure/azure-sdk-for-go/sdk/security/keyvault/azsecrets
- DefaultAzureCredential chain: https://learn.microsoft.com/azure/developer/go/azure-sdk-authentication
