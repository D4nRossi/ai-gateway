# ADR-0020: Target credentials com modo de armazenamento por target (AES / KV / Both)

- **Status**: accepted
- **Date**: 2026-05-28
- **Implementation date**: 2026-05-28 (mesmo dia — escopo enxuto, fechado em uma sessão)
- **Decision makers**: Daniel (owner)
- **Consulted**: Claude Opus 4.7
- **Supersedes**: nenhum
- **Roadmap**: Onda 4.5 (P1 Segurança)

## Context

Hoje (pós-Onda 7), credenciais de upstream targets ficam cifradas em
`gogateway.proxy_targets.auth_config_enc` via AES-256-GCM (ADR-0012). A chave
AES é resolvida do Key Vault corporativo pelo resolver de config no boot:

```yaml
# configs/gateway.yaml
database:
  encryption_key_hex: ${kv:DB-ENCRYPTION-KEY}
```

Esse desenho tem três limitações operacionais:

1. **Rotação atômica da chave AES quebra todos os targets simultaneamente.**
   Trocar a versão do secret `DB-ENCRYPTION-KEY` no KV invalida todo
   `auth_config_enc` na tabela, exigindo re-cifragem em batch como
   pré-requisito do deploy.
2. **A chave AES é single point of compromise.** Vazamento da chave expõe
   *todas* as credenciais de target persistidas.
3. **Não há mecanismo nativo de rotação/audit por credencial individual.** Todo
   audit é a nível de chave AES; quem leu/atualizou cada credencial em particular
   não fica registrado.

A Onda 3 (ADR-0018) introduziu Azure Key Vault como provider de segredos
resolvido no boot, com cliente cacheado (`internal/infra/keyvault.Client`,
TTL default 5min). Esse cliente é reusável pra credenciais de target em runtime.

A motivação pra trazer essa frente agora (Onda 4.5):

- Onda 7 finalizou e estabilizou; nenhuma frente bloqueia esta.
- Owner relatou em sessões anteriores cenário concreto onde rotacionar
  `DB-ENCRYPTION-KEY` quebrou targets durante validação.
- Onda 8 (streaming de áudio) foi rejeitada (ADR-0023); Onda 4.5 é a próxima P1.

**Restrições de design (anotadas com owner em 2026-05-28):**

- V1 **não pode forçar migração ao KV.** Em produção corporativa, o KV pode
  estar provisionado mas com SLA/latência incertos. AES tem que continuar
  funcionando como antes.
- Owner usa KV pessoal `danieldev` em dev; KV corporativo tem latência maior
  e foi indisponível pela rede móvel durante a primeira tentativa de boot
  desta sessão.
- Boot **não pode bloquear** se o KV cair pra leitura de credenciais de target
  (diferente do boot dos `${kv:...}` no yaml — esses bloqueiam intencionalmente
  porque referenciam segredos críticos do gateway: API keys, DB password).
- AES **não morre na V1.** É o default; coexiste com KV indefinidamente até
  uma Onda futura decidir descontinuar.

## Decision

Cada `proxy_target` ganha um campo `credential_storage_mode` que controla
explicitamente onde a credencial vive, com três valores:

| Mode | Onde fica a credencial | Read path | Write path |
|---|---|---|---|
| `aes` (default) | `auth_config_enc` (status quo) | Decifra AES | Encripta novo valor em AES |
| `kv` | KV apenas, em `kv_secret_name` | `kv.Get` com timeout 200ms; falha = 503 | Grava no KV; falha = update falha |
| `both` | KV + AES como cache cifrado | `kv.Get` 200ms; em erro/timeout → AES fallback | Grava no KV primeiro; em sucesso re-encripta em AES |

**Fallback em `both`**: se o KV não responde em 200ms ou retorna erro, o
read decifra `auth_config_enc` (último valor sincronizado conhecido). Emite
`warn event_type=kv_fallback_used` com `target_id` e `kv_error`. O cache de
5min do `keyvault.Client` (ADR-0018) cobre a maior parte das janelas curtas
de indisponibilidade — o fallback AES é a segunda linha de defesa, pra quedas
mais longas ou expiração de cache concorrente.

**Write em `both`** é dupla escrita ordenada: KV primeiro (autoritativo), AES
em seguida como atualização do cache. Se o KV falha, o update **falha** —
não há divergência permitida. Se o AES falha após KV ok, loga `warn
event_type=kv_aes_cache_write_failed` mas o update é considerado **bem-sucedido**
(KV é a fonte de verdade; AES é só cache).

**Targets existentes** ficam todos em `aes`. Zero migração compulsória.

**Naming dos secrets no KV**: `gateway-target-{uuid_v7}` — UUID v7 gerado uma
vez na criação/migração do target. Imutável. Armazenado em
`proxy_targets.kv_secret_name`.

- 15 chars de prefixo + 36 do UUID = 51 chars. KV permite até 127.
- UUID v7 é timestamp-prefixed: ordenação natural por criação no
  `az keyvault secret list`.
- Não enumerável por adivinhação (vs `gateway-target-1`, `gateway-target-2`).
- Não vaza identificadores de negócio (endpoint slug, nome de target).
- Renomear endpoint ou target **não quebra** o link com o KV.

**Override manual do nome** é permitido via UI/CLI pra cenários onde o owner
quer alinhar com convenção corporativa existente. Validação:
alfanumérico + hífen, 1-127 chars (regra do KV).

**Onda futura (sem ETA neste documento):**

- Quando o KV corporativo tiver SLA validado e adoção amadurecer, abrir
  ADR próprio pra descontinuar o modo `aes` (drop coluna `auth_config_enc`,
  remoção do fallback path, simplificação do resolver).

## Options considered

### Option 1: Status quo (AES only)

Mantém o desenho atual. Toda credencial em `auth_config_enc`. Rotação da chave
AES continua sendo o problema operacional descrito acima.

- **Pros:**
  - Zero mudança de código.
  - Sem dependência runtime do KV pra targets.
- **Cons:**
  - Não resolve nenhuma das três limitações do Context.
  - Reduz a opção do owner de operar credenciais individualmente.
- **Why not:** o problema motivador permanece sem solução.

### Option 2: KV only, sem fallback

Toda credencial vai pro KV. `auth_config_enc` é dropada. Read direto no KV
com timeout. Falha = 503 ao cliente.

- **Pros:**
  - Single source of truth.
  - Audit/rotação por credencial naturalmente cobertos pelo KV.
  - Drop completo do AES simplifica o resolver.
- **Cons:**
  - Boot e runtime bloqueiam totalmente em queda do KV.
  - Migração compulsória em V1 — incompatível com a restrição de owner
    ("V1 ideal é só AES").
  - Em produção corporativa, latência do KV vira parte do p99 das requisições
    proxy plane sem amortização.
- **Why not:** fere a restrição de V1 não-compulsória e introduz dependência
  runtime forte do KV sem rede de segurança.

### Option 3: KV primário + cache memória TTL (sem AES persistente)

KV é fonte única. `auth_config_enc` dropada. Cliente cacheia em RAM (já existe
no `keyvault.Client`, TTL 5min). Em queda do KV, cache cobre até expiração.

- **Pros:**
  - Single source of truth.
  - Cache TTL existente reduz pressão no KV.
- **Cons:**
  - Boot do gateway exige KV alcançável pra carregar credenciais ativas
    (ou aceitar boot vazio até primeiro request).
  - Quedas > TTL deixam o gateway parcialmente funcional.
  - Migração compulsória em V1 — mesma incompatibilidade da Option 2.
  - Cache é per-process; restart com KV indisponível = gateway sem credenciais.
- **Why not:** mesma restrição de V1 não-compulsória; cache memória sozinho
  não é suficiente pra cobrir cenários de manutenção planejada do KV.

### Option 4: AES sempre + KV opcional como espelho

`auth_config_enc` é sempre fonte de verdade. KV é cópia secundária quando
configurado. Read sempre lê AES; KV nunca é lido pelo gateway, só serve pra
audit externo.

- **Pros:**
  - Zero risco de leitura: AES sempre disponível.
  - Naturalmente backward-compatible.
- **Cons:**
  - **Não resolve rotação atômica da chave AES** — o problema motivador
    principal continua.
  - KV vira "log adicional" sem valor operacional concreto pro gateway.
  - Esforço de implementação sem retorno arquitetural.
- **Why not:** falha em endereçar a motivação central.

### Option 5: Modo por target {aes | kv | both} (CHOSEN)

Cada target declara explicitamente o modo. AES é default. KV puro existe
pra targets críticos onde rotação granular é desejada. `both` cobre o caso
intermediário: KV pra rotação + AES como rede de segurança.

- **Pros:**
  - V1 não-compulsória — targets existentes ficam em `aes`.
  - Owner escolhe risco vs benefício target-a-target.
  - Modo `both` resolve a queda de KV sem sacrificar rotação granular.
  - Cache existente do `keyvault.Client` (5min TTL) amortiza latência;
    fallback AES cobre quedas > TTL.
  - Migração progressiva: owner muda modo via UI/CLI quando confortável.
  - Caminho de simplificação claro: Onda futura promove tudo pra `kv` e
    dropa `auth_config_enc`.
- **Cons:**
  - Read path do resolver tem 3 caminhos diferentes — mais complexo
    de testar e diagnosticar.
  - Modo `both` requer dupla escrita coordenada; falha entre KV e AES
    deixa janela curta de cache desatualizado.
  - Esquema cresce em 2 colunas; UI/CLI/repo ganham concerns extras.
- **Why chosen:** é a única que respeita a restrição "V1 só AES" (default)
  enquanto endereça as 3 limitações do Context e dá ao owner controle
  granular sem migração forçada.

## Consequences

### Positive

- Rotação **granular** de credenciais (target-by-target via KV versions).
- Resiliência a queda do KV no read path em modo `both` (fallback AES).
- Suporte gradual à migração: zero impacto em V1, owner promove targets
  quando KV corporativo provar SLA.
- Operações isoladas: girar a chave AES master deixa de afetar targets
  em modo `kv`.
- Audit nativo via KV access logs (Azure Monitor / Diagnostic Settings)
  pra modos `kv` e `both`.
- Caminho de evolução claro: Onda futura simplifica pra KV-only.

### Negative / Trade-offs

- **Complexidade do resolver**: três caminhos (`aes`, `kv`, `both`) com
  semânticas diferentes. Diagnóstico de incidente exige saber qual modo
  o target estava configurado.
- **Dupla escrita em `both`**: ordenada (KV primeiro). Falha entre passos
  deixa AES desatualizado até próxima escrita bem-sucedida. Mitigação:
  log explícito + read sempre prioriza KV.
- **Latência adicional**: timeout de 200ms no read pra modos `kv` e `both`
  entra no p99 da request. Mitigação parcial: cache de 5min reduz a
  ~1 fetch por target a cada 5min em steady-state.
- **Naming UUID obscuro**: pouco humano-legível. Mitigação: UI mostra um
  link clicável pro secret no Azure Portal junto com o nome.
- **Migração não é atômica**: targets em modos diferentes durante a transição
  exigem disciplina operacional.

### Mitigations

- **Testes table-driven do resolver** cobrindo: aes happy path, kv happy
  path, kv timeout em `kv` (503 ao cliente), kv timeout em `both` (fallback
  AES + log), kv erro em `both` (fallback AES), write em `both` com KV ok
  + AES erro, write em `both` com KV erro (abort).
- **Log estruturado de transições importantes** (`kv_fallback_used`,
  `kv_aes_cache_write_failed`) com `target_id` e `kv_error`. Permite
  alerta operacional sem expor valor da credencial (CLAUDE.md §1.4 — nunca
  loga credencial).
- **CLI `cmd/migrate-targets-to-kv` é idempotente**: re-rodar contra um
  target já migrado é no-op. Permite scripts de batch com retry.
- **UI mostra estado atual do target** (mode, KV secret name, "última
  sincronização KV → AES") na página de edição.
- **Cache do `keyvault.Client` já existe** (ADR-0018, TTL 5min). Não
  precisa reimplementar.

## Implementation sketch

### Migration 011

Arquivo: `migrations/011_proxy_targets_kv_credential_mode.up.sql`

```sql
-- 011_proxy_targets_kv_credential_mode.up.sql (T-SQL)
--
-- Adiciona suporte a armazenamento de credenciais de proxy_targets no Key Vault
-- como alternativa ou complemento ao AES-256-GCM existente (ADR-0020).
--
-- Modes:
--   'aes'  (default) — credencial em auth_config_enc apenas. Status quo.
--   'kv'             — credencial em KV apenas (kv_secret_name preenchido).
--   'both'           — KV é fonte de verdade; auth_config_enc é cache cifrado.

ALTER TABLE gogateway.proxy_targets
    ADD credential_storage_mode NVARCHAR(10) NOT NULL
            CONSTRAINT df_proxy_targets_credential_storage_mode DEFAULT 'aes'
            CONSTRAINT ck_proxy_targets_credential_storage_mode
                CHECK (credential_storage_mode IN ('aes', 'kv', 'both')),
        kv_secret_name NVARCHAR(127) NULL
            CONSTRAINT ck_proxy_targets_kv_secret_name_format
                CHECK (kv_secret_name IS NULL OR LEN(kv_secret_name) BETWEEN 1 AND 127);

-- Index parcial: lookup rápido por nome do secret quando filtrando alguma
-- query operacional ("quais targets usam o secret X?"). Filtered porque a
-- maioria dos rows terá kv_secret_name NULL (default 'aes').
CREATE INDEX idx_proxy_targets_kv_secret_name
    ON gogateway.proxy_targets(kv_secret_name)
    WHERE kv_secret_name IS NOT NULL;
```

Down migration reverte com `ALTER TABLE … DROP COLUMN` em ordem inversa +
`DROP INDEX`. Inclui guards `IF EXISTS` conforme CLAUDE.md §9.2.

### Domain — `internal/domain/endpoint/endpoint.go`

Adicionar:

```go
// CredentialStorageMode controls where the target's credentials are persisted
// and how they are resolved at request time.
type CredentialStorageMode string

const (
    CredentialModeAES  CredentialStorageMode = "aes"
    CredentialModeKV   CredentialStorageMode = "kv"
    CredentialModeBoth CredentialStorageMode = "both"
)

// Target ganha:
type Target struct {
    // ... campos existentes ...

    // CredentialStorageMode determines whether Auth comes from AES-decrypted
    // auth_config_enc, from Key Vault, or both (KV primary + AES fallback).
    CredentialStorageMode CredentialStorageMode

    // KVSecretName is the Key Vault secret name backing this target's credentials.
    // Required when CredentialStorageMode is "kv" or "both". Format:
    // gateway-target-{uuid_v7} by default, or custom name set by admin.
    KVSecretName string
}
```

### Resolver — novo pacote `internal/app/proxyservice/credentialresolver.go`

```go
// CredentialResolver resolves a Target's plaintext credentials based on its
// CredentialStorageMode. It encapsulates the AES / KV / both branching logic
// so callers (proxy engine) work only with plaintext TargetAuth.
type CredentialResolver interface {
    Resolve(ctx context.Context, t endpoint.Target) (endpoint.TargetAuth, error)
}

type defaultCredentialResolver struct {
    encrypter crypto.Encrypter
    kv        keyvault.SecretGetter
    kvTimeout time.Duration // 200ms fixed in V1
    logger    *slog.Logger
}
```

Implementação resumida:

- `mode = aes`: decifra `t.AuthConfigEnc` (campo atual). Status quo.
- `mode = kv`: `ctx, cancel := context.WithTimeout(ctx, 200ms); defer cancel; kv.Get(ctx, t.KVSecretName)`. Falha = retorna erro propagado.
- `mode = both`: tenta KV com timeout 200ms; em erro, decifra AES com log
  `kv_fallback_used`.

Parsing do valor retornado pelo KV: JSON encoded `TargetAuth` (mesma estrutura
do `auth_config_enc` decifrado). Decisão de format detalhada em sub-tarefa
de implementação.

### CLI — `cmd/migrate-targets-to-kv/main.go`

Uso:

```
go run ./cmd/migrate-targets-to-kv \
    --target-id 42 \
    --mode both \
    [--secret-name gateway-target-018d9...]   # opcional; default gera UUID v7
```

Fluxo:

1. Carrega config (mesma fonte que `cmd/gateway`)
2. Conecta DB + KV
3. Lê target da DB; valida que existe e que `mode = aes` (refuse-on-already-migrated)
4. Decifra `auth_config_enc` em memória
5. Gera UUID v7 se `--secret-name` não passado
6. Serializa credencial e POST no KV (cria versão 1)
7. `UPDATE proxy_targets SET credential_storage_mode = @p1, kv_secret_name = @p2 WHERE id = @p3`
8. Em `--mode kv` (não `both`): também `SET auth_config_enc = NULL`
9. Loga `event_type=target_credential_migrated` com `target_id`, `mode`,
   `secret_name`

Idempotente: re-rodar contra target já em `mode = kv|both` é no-op com
mensagem informativa.

### Admin API — `internal/api/admin/handlers/endpoints.go`

- `POST /admin/v1/endpoints/{id}/targets` aceita `credential_storage_mode`
  e `kv_secret_name` no payload. Default `aes`.
- `PUT /admin/v1/endpoints/{id}/targets/{tid}` permite mudar o modo
  (validações: transição AES → KV exige migração via CLI; UI orienta).
- Endpoint novo `POST /admin/v1/endpoints/{id}/targets/{tid}/migrate-to-kv`
  pra invocar a lógica do CLI server-side (conveniência da UI).

### UI — `web/src/components/endpoints/TargetForm.tsx`

- Select `Storage mode`: AES local | Key Vault | Both
- Quando KV ou Both:
  - Campo `KV secret name` com placeholder `gateway-target-{auto}` e botão
    "Gerar UUID v7"
  - Link contextual pro Portal Azure (`https://portal.azure.com/...`)
- Em targets existentes em modo `aes` com credencial:
  - Botão "Migrar para Key Vault" que chama o endpoint admin
  - Modal de confirmação explicando modo (kv vs both)
- Indicador visual do modo atual na lista de targets do endpoint

### Observability

Eventos novos em `audit_events`:

- `target_credential_migrated` (mode antes, mode depois, secret name)
- `target_credential_updated` (mode, secret name; sem o valor)

Eventos de log (não DB):

- `kv_fallback_used` — warn — `target_id`, `kv_error`, `latency_ms`
- `kv_aes_cache_write_failed` — warn — `target_id`, `aes_error`

## Open questions

1. **Formato do valor armazenado no KV.** JSON do `TargetAuth` é a opção
   default; alternativa seria HCL ou um formato custom. Definir antes da
   implementação do resolver.
2. **Estratégia de retry em writes em modo `both`.** Se KV ok + AES falha,
   logamos mas declaramos sucesso. Vale tentar novamente em background?
   Provavelmente não na V1 — simplicidade vence.
3. **Bulk migration command.** Útil ter um modo `--all --mode both` no CLI?
   Provavelmente sim na V2; V1 é alvo por target (segurança).
4. **TTL do cache do `keyvault.Client` por modo.** Default 5min cobre o caso
   geral, mas modo `kv` (sem fallback) talvez queira TTL menor pra propagar
   rotação mais rápido. Revisitar com dado real depois da V1.

## References

- ADR-0010 — generic HTTP proxy engine
- ADR-0012 — AES-256-GCM target credentials at rest
- ADR-0013 — load balancing strategies
- ADR-0015 — domain/app/infra layering
- ADR-0017 — path translation per provider_kind
- ADR-0018 — Azure Key Vault secret provider
- ADR-0022 — troca PG → SQL Server (schema gogateway)
- `migrations/004_proxy_endpoints.up.sql` — schema atual `proxy_targets`
- `internal/infra/crypto/crypto.go` — AES-256-GCM impl
- `internal/infra/keyvault/client.go` — KV resolver com cache 5min
- `internal/domain/endpoint/endpoint.go` — domain types
- Azure Key Vault secrets reference: https://learn.microsoft.com/azure/key-vault/secrets/about-secrets
- Azure Key Vault naming rules: https://learn.microsoft.com/azure/key-vault/general/about-keys-secrets-certificates#vault-name-and-object-name
- UUID v7 spec: https://datatracker.ietf.org/doc/html/rfc9562#section-5.7
- google/uuid v1.x (já em uso no projeto): https://pkg.go.dev/github.com/google/uuid
