# ADR-0026: Secrets locais em Windows — Always Encrypted + DPAPI híbrido

- **Status**: proposed
- **Date**: 2026-05-28
- **Decision makers**: Daniel (owner)
- **Consulted**: Claude Opus 4.7
- **Supersedes**: nenhum
- **Aplica-se a**: deploy Windows (apenas; Linux fica em follow-up)

## Context

A intenção arquitetural era manter Azure Key Vault como provedor de secrets
(ADR-0018). O ambiente de produção corporativo onde o gateway vai rodar
**não tem KV disponível** — pelo menos não no primeiro deploy. Owner pediu
solução self-contained "sem quebrar normas de segurança".

Secrets que precisam migrar:

1. `AZURE_OPENAI_API_KEY` — credencial pra Azure OpenAI (boot)
2. `AZURE_LANGUAGE_API_KEY` — Tier 2/3 PII via Azure (boot)
3. `DATABASE_PASSWORD` — conexão SQL Server corp (boot)
4. `DB_ENCRYPTION_KEY` — AES-256 master pra `proxy_targets.auth_config_enc` (boot)
5. `gateway-target-{uuid}` × N — credenciais individuais de target em modes
   `kv` ou `both` da Onda 4.5 (runtime, lookup por target ID)

**Restrições e drivers:**

- Servidor Windows corporativo (manual de deploy em `docs/deploy/windows.md`)
- Sem KV externo na rede do servidor
- Normas corporativas Microsoft preferem ferramentas nativas do SO sobre
  soluções multiplataforma
- Owner decidiu: rodar CLI **no servidor** (cert nunca sai), rotação **sem
  hot-reload** (CLI + restart de serviço aceitável)
- A senha do banco tem o problema **chicken-and-egg**: pra ler qualquer
  secret cifrado em Always Encrypted, gateway precisa **já estar conectado
  ao banco** — logo, pelo menos um secret precisa morar fora do AE

## Decision

**Solução híbrida em duas camadas:**

| Camada | Cobertura | Mecanismo |
|---|---|---|
| **Bootstrap** | `DATABASE_PASSWORD` apenas | DPAPI sobre `gateway.env` (cifrado com escopo `LocalMachine`) |
| **Application** | 4+N secrets restantes | SQL Server **Always Encrypted** em `gogateway.secrets`, com **Column Master Key** vinculada a cert auto-assinado no `LocalMachine\My` |

**Fluxo de boot:**

```
1. WinSW inicia gateway.exe
2. Gateway lê gateway.env.dpapi do disco
3. Gateway descriptografa via CryptUnprotectData (golang.org/x/sys/windows)
4. DATABASE_PASSWORD vai pro ambiente de processo
5. Gateway abre conexão SQL com `ColumnEncryption=true`
6. Driver microsoft/go-mssqldb usa MSSQL_CERTIFICATE_STORE provider
   pra resolver a CEK via cert do Windows Cert Store
7. Gateway SELECT em gogateway.secrets pra cada secret necessário
8. Valores em memória até shutdown
```

**Yaml mantém a sintaxe `${kv:NAME}` existente** — só o backend muda. Env
var `SECRET_PROVIDER` controla:

- `kv` (default — backward compat) → `keyvault.Client`
- `db` (Windows prod) → novo `secretsdb.Client` implementando o mesmo
  `keyvault.SecretGetter` interface

`CredentialResolver` (Onda 4.5) e qualquer outro consumidor da interface
**não precisa de mudança alguma** — drop-in replacement.

**CMK / CEK setup:**

- Cert auto-assinado gerado no servidor via `New-SelfSignedCertificate`
- Importado em `LocalMachine\My`
- Permissão de leitura na chave privada concedida à service account (gMSA)
- `CREATE COLUMN MASTER KEY` aponta pro thumbprint do cert
- `CREATE COLUMN ENCRYPTION KEY` cifrada com o CMK
- Coluna `gogateway.secrets.value` declarada `ENCRYPTED WITH (...)`
- **Backup do PFX** (com senha) em local seguro corporativo — perda do PFX
  = perda permanente de todos os secrets cifrados

**CLI de operação no servidor:**

```
cmd/secrets set    --name X     # lê valor do stdin (nunca CLI flag)
cmd/secrets get    --name X     # debug — gated por env GATEWAY_SECRETS_ALLOW_GET
cmd/secrets list                # mostra names + created_at, NÃO valores
cmd/secrets rotate --name X     # idem set, mas valida que existe
cmd/secrets delete --name X     # pede confirmação
```

CLI usa a mesma config do gateway (env DPAPI pra DATABASE_PASSWORD).

## Options considered

### Option 1: Tudo em DPAPI (`gateway.env` cifrado com tudo dentro)

- **Pros:** uma única camada, simples
- **Cons:** N secrets em um arquivo; rotação de 1 exige reescrever o arquivo
  inteiro; sem audit nativo; não escala pros gateway-target-{uuid} da Onda 4.5
- **Why not:** quebra o caminho do CredentialResolver (Onda 4.5) que espera
  lookup por nome

### Option 2: Tudo em Always Encrypted

- **Pros:** consistente — tudo no banco
- **Cons:** **chicken-and-egg** — sem DATABASE_PASSWORD plaintext o gateway
  não conecta no banco; sem conexão não lê os secrets cifrados
- **Why not:** impossível sem outro mecanismo pra bootstrap

### Option 3: Tudo em Windows Credential Manager

- **Pros:** API nativa, sem arquivo no disco
- **Cons:** Credential Manager é per-user; gMSA service account complica
  acesso programático; rotação manual via PowerShell
- **Why not:** ergonomia pior, sem audit, sem versionamento

### Option 4: Híbrido AE + DPAPI (CHOSEN)

Conforme detalhado em "Decision".

- **Pros:** AE pros secrets de application (audit, versioning, rotação
  granular); DPAPI cobre só o bootstrap mínimo; reaproveita interface
  `keyvault.SecretGetter` da Onda 4.5; CMK rotation via processo MS padrão
- **Cons:** duas tecnologias pra documentar; cert backup obrigatório
- **Why chosen:** entrega self-contained sem chicken-and-egg; alinhado com
  Microsoft Security Baseline pra Windows Server

### Option 5: HashiCorp Vault auto-hosted

- **Pros:** funcionalmente equivalente ao KV cloud
- **Cons:** mais um serviço pra operar (HA, backup, upgrade); owner pediu
  "self-contained"
- **Why not:** complexidade operacional fora do escopo do deploy inicial

## Consequences

### Positive

- **Self-contained**: zero dependência de KV externo
- **Audit nativo**: SQL Server registra access aos valores em `gogateway.secrets`
  via auditing default (eventos `column-level access`)
- **Rotação granular**: rotacionar 1 secret é UPDATE de 1 linha + restart do
  gateway; cache 5min do `secretsdb.Client` cobre os 5min de propagação
- **Compatibilidade com Onda 4.5**: `CredentialResolver` recebe um
  `SecretGetter` diferente, código permanece intacto
- **Backup nativo via SQL Server**: secrets vão junto com o backup do banco
- **Sem nova dep externa**: DPAPI via `golang.org/x/sys/windows`; AE via
  `microsoft/go-mssqldb` já no `go.mod`
- **Aceitável em audit corporativo**: DPAPI reconhecido por CIS Baseline;
  Always Encrypted reconhecido por Microsoft Security Baseline

### Negative / Trade-offs

- **Windows-only no V1**: build tags Windows-only no DPAPI wrapper; deploy
  Linux fica como follow-up (provavelmente systemd-creds + SQL Server AE
  equivalente, mas é ADR próprio)
- **Cert backup é responsabilidade do operador**: perda do PFX = perda
  permanente dos secrets cifrados. Mitigação: procedimento no manual de
  deploy + lembrete operacional
- **CMK rotation é manutenção não-trivial**: trocar cert do CMK exige
  re-encrypt das CEKs + valores. Processo Microsoft documentado mas precisa
  janela de manutenção. Provavelmente 1× ao ano
- **Migration 012 cria a tabela mas não a `ENCRYPTED WITH` clause**: o
  thumbprint do cert varia por servidor, golang-migrate não suporta SQL
  parametrizado. Operador precisa rodar PowerShell separado pra: criar CMK
  + CEK + ALTER COLUMN
- **`MSSQL_CERTIFICATE_STORE` provider do driver Go é menos exercitado que
  no .NET/JDBC**: risco médio de bugs em edge cases. Mitigação: testes
  unit com fake `SecretGetter`; smoke test em homolog antes de prod
- **CLI exige RDP/SSH ao servidor** pra qualquer operação: opção consciente
  do owner — cert nunca sai

### Mitigations

- **PowerShell script de setup** no manual de deploy Windows guia o operador
  pelo cert + CMK + CEK + ALTER COLUMN, evitando erros de digitação
- **CLI tem `--dry-run`** em `set` e `rotate` que mostra o ciphertext que
  seria escrito sem persistir — útil pra confirmar que o cert está acessível
  antes de gravar valor real
- **`cmd/secrets get` é gated** por env `GATEWAY_SECRETS_ALLOW_GET=1` —
  evita vazamento acidental em command history
- **`cmd/secrets list`** mostra apenas names + created_at + updated_at — nunca
  valores
- **Logs do gateway no boot** registram `event_type=secret_loaded` com `name`
  (não value) + `provider` (db ou kv) + `latency_ms` — owner consegue auditar
  qual secret foi lido e quando
- **DPAPI scope `LocalMachine`** (não `CurrentUser`) garante que qualquer
  conta no servidor com acesso ao arquivo consegue descriptografar — mas o
  arquivo tem ACL apertado (`SYSTEM:F` + `<gMSA>:R`)
- **Testes table-driven** do `secretsdb.Client` cobrem: secret existe / não
  existe / valor vazio / cache hit / cache expiry / DB indisponível
- **`cmd/secrets` recusa rodar** sem `SECRET_PROVIDER=db` setado, pra evitar
  conexão acidental num ambiente KV-backed

## Implementation sketch

### `internal/infra/dpapi/dpapi.go`

```go
// Build tags split the implementation per OS. Windows uses
// CryptProtectData/CryptUnprotectData syscalls via golang.org/x/sys/windows.
// Other platforms get a stub that returns an explicit error.

// dpapi_windows.go (//go:build windows)
func Protect(data []byte) ([]byte, error)   { /* CryptProtectData LocalMachine */ }
func Unprotect(data []byte) ([]byte, error) { /* CryptUnprotectData */ }

// dpapi_other.go (//go:build !windows)
func Protect(_ []byte) ([]byte, error)   { return nil, ErrUnsupportedOS }
func Unprotect(_ []byte) ([]byte, error) { return nil, ErrUnsupportedOS }
```

### `internal/infra/secretsdb/client.go`

```go
type Client struct {
    db     *sql.DB
    ttl    time.Duration   // default 5min, mirrors keyvault.Client
    logger *slog.Logger

    mu    sync.RWMutex
    cache map[string]entry
}

// Compile-time assertion: implements keyvault.SecretGetter
var _ keyvault.SecretGetter = (*Client)(nil)

func New(db *sql.DB) *Client { /* default TTL, default logger */ }
func (c *Client) Get(ctx context.Context, name string) (string, error)
func (c *Client) Set(ctx context.Context, name, value string) error  // mesma interface do SecretSetter da ADR-0020
```

### Migration `012_gogateway_secrets.up.sql`

```sql
-- Cria APENAS a tabela. CMK/CEK/ENCRYPTED WITH são manuais via PowerShell
-- (manual de deploy §X) porque o thumbprint do cert varia por servidor.

IF OBJECT_ID('gogateway.secrets', 'U') IS NULL
BEGIN
    CREATE TABLE gogateway.secrets (
        name        NVARCHAR(127)   NOT NULL PRIMARY KEY,
        value       VARBINARY(MAX)  NOT NULL,
        created_at  DATETIMEOFFSET  NOT NULL DEFAULT SYSUTCDATETIME(),
        updated_at  DATETIMEOFFSET  NOT NULL DEFAULT SYSUTCDATETIME()
    );
END;
```

### Setup manual no servidor (PowerShell)

Detalhado no manual de deploy. Resumo:

1. Gerar cert auto-assinado:
   ```powershell
   $cert = New-SelfSignedCertificate -Subject "CN=AIGateway-CMK" `
       -CertStoreLocation Cert:\LocalMachine\My `
       -KeyExportPolicy Exportable -KeySpec KeyExchange
   ```
2. Backup PFX (com senha forte) em local seguro corporativo
3. Conceder leitura da chave privada à gMSA
4. Conectar SQL Server e rodar:
   ```sql
   CREATE COLUMN MASTER KEY AIGateway_CMK
       WITH (KEY_STORE_PROVIDER_NAME = 'MSSQL_CERTIFICATE_STORE',
             KEY_PATH = 'LocalMachine/My/<THUMBPRINT>');
   -- CEK gerada via SSMS UI ou ferramenta MS dedicada
   CREATE COLUMN ENCRYPTION KEY AIGateway_CEK ...;
   ALTER TABLE gogateway.secrets
       ALTER COLUMN value VARBINARY(MAX)
           ENCRYPTED WITH (ENCRYPTION_TYPE = Randomized,
                           ALGORITHM = 'AEAD_AES_256_CBC_HMAC_SHA_256',
                           COLUMN_ENCRYPTION_KEY = AIGateway_CEK);
   ```
5. Cifrar `gateway.env` com DPAPI:
   ```powershell
   $env_content = "DATABASE_PASSWORD=<senha>`nCONFIG_PATH=C:\AIGateway\configs\gateway.yaml`n..."
   $bytes = [System.Text.Encoding]::UTF8.GetBytes($env_content)
   $cipher = [System.Security.Cryptography.ProtectedData]::Protect(
       $bytes, $null, 'LocalMachine')
   [System.IO.File]::WriteAllBytes('C:\AIGateway\configs\gateway.env.dpapi', $cipher)
   ```
6. Popular secrets via CLI:
   ```powershell
   "<azure-openai-key>" | C:\AIGateway\bin\secrets.exe set --name AZURE_OPENAI_API_KEY
   "<language-key>"    | C:\AIGateway\bin\secrets.exe set --name AZURE_LANGUAGE_API_KEY
   "<db-encryption-key-hex>" | C:\AIGateway\bin\secrets.exe set --name DB_ENCRYPTION_KEY
   ```

## Open questions

1. **Migração de targets em mode=kv|both** que hoje apontam pro KV: precisa
   re-criar via CLI no `gogateway.secrets` com o mesmo nome (`gateway-target-{uuid}`).
   Procedimento documentado no manual de deploy + ADR-0026 §Implementation.
2. **Cert renewal**: cert auto-assinado tem validade default de 1 ano. Antes
   de expirar, processo é: gerar novo cert → criar novo CMK referenciando ele
   → re-encriptar a CEK com novo CMK (`ALTER COLUMN ENCRYPTION KEY ... ADD VALUE`)
   → revoga CMK antigo. Documentar no manual quando virar relevante.
3. **Deploy Linux equivalente**: fica como ADR follow-up. Caminho provável:
   `systemd-creds encrypt` (Linux moderno) ou `age` para o bootstrap; AE no
   SQL Server idêntico ao Windows (cert no PEM store local).
4. **Audit em `gogateway.secrets`**: SQL Server permite auditing column-level.
   Quando virar requisito, ligar via `CREATE SERVER AUDIT` apontando pra
   arquivo + spec específica pra tabela secrets.

## References

- ADR-0012 — AES-GCM target credentials at-rest (mantida; cobre auth_config_enc)
- ADR-0018 — Azure Key Vault provider (substituído em deploy Windows; mantido como path opcional)
- ADR-0020 — Credential storage mode per target (Onda 4.5; `secretsdb.Client`
  é drop-in pra `keyvault.Client`)
- ADR-0022 — SQL Server troca (driver `microsoft/go-mssqldb` é mantido)
- ADR-0025 — MIGRATIONS_AUTO_APPLY (continua relevante; migration 012 vai
  via `migrate up` manual em prod)
- `docs/deploy/windows.md` — setup operacional completo
- microsoft/go-mssqldb Always Encrypted: https://github.com/microsoft/go-mssqldb
- Always Encrypted documentation:
  https://learn.microsoft.com/sql/relational-databases/security/encryption/always-encrypted-database-engine
- Windows DPAPI: https://learn.microsoft.com/windows/win32/api/dpapi/
- CryptProtectData reference (LocalMachine scope):
  https://learn.microsoft.com/windows/win32/api/dpapi/nf-dpapi-cryptprotectdata
