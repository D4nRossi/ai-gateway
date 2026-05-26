# Azure Key Vault — setup

Como configurar o gateway para resolver segredos do seu Key Vault em vez de ler
do `.env`. Coberto: pré-requisitos no Azure, permissionamento do seu usuário,
cadastro de segredos, ativação no gateway (dev local + prod), troubleshooting.

> Decisão arquitetural: [ADR-0018](adrs/0018-azure-key-vault-secret-provider.md)
> (fail-fast, cache TTL 5 min, `DefaultAzureCredential` em dev e Managed
> Identity em prod).

---

## 1. Pré-requisitos

| Item | Como obter |
|---|---|
| Vault existente | Você já tem: `https://danieldev.vault.azure.net/` |
| Azure CLI instalado | `winget install Microsoft.AzureCLI` (Windows) ou [docs](https://learn.microsoft.com/cli/azure/install-azure-cli) |
| `az login` feito | `az login` no PowerShell — abre o browser e autentica |

Confirme que está logado e na assinatura certa:

```pwsh
az account show
# deve listar sua subscription "Pobreza" (ou o nome dela)
```

---

## 2. Permissionar seu usuário no Key Vault

O `DefaultAzureCredential` que o gateway usa em dev procura, na ordem: env vars
de Service Principal, Managed Identity, **Azure CLI (`az login`)**. Para o
caminho CLI funcionar, seu usuário precisa ter permissão de **leitura de
segredos** no Vault.

### 2.1 Pega seu objectId

```pwsh
$me = az ad signed-in-user show --query id -o tsv
$me   # deve imprimir um GUID
```

### 2.2 Atribui o role "Key Vault Secrets User"

```pwsh
$vaultId = az keyvault show --name danieldev --query id -o tsv

az role assignment create `
  --assignee $me `
  --role "Key Vault Secrets User" `
  --scope $vaultId
```

> Esse role dá `Microsoft.KeyVault/vaults/secrets/getSecret/action` apenas —
> leitura. Não permite criar/listar/apagar secrets. Para administrar pela CLI,
> peça também o role **"Key Vault Secrets Officer"** ao admin da subscription
> (ou atribua você mesmo se já for owner).

### 2.3 (Alternativa) Access policies legacy

Se o vault está configurado com Access Policies (não RBAC), use:

```pwsh
az keyvault set-policy `
  --name danieldev `
  --object-id $me `
  --secret-permissions get list
```

Para descobrir qual modelo seu vault usa: Portal → Key Vault → Access
configuration. Vaults criados em 2023+ vêm com RBAC por default.

---

## 3. Cadastrar os segredos no Vault

Nomes alinhados com o que o gateway espera (ADR-0018). Substitua o valor
`MEU-VALOR-AQUI` pelo segredo real.

```pwsh
# Azure OpenAI API key — vem da Azure AI Foundry → Keys and Endpoint
az keyvault secret set `
  --vault-name danieldev `
  --name AZURE-OPENAI-API-KEY `
  --value "MEU-VALOR-AQUI"

# Chave AES-256-GCM para credenciais de target no DB (ADR-0012)
# Gere com: -join ((1..32) | % { '{0:x2}' -f (Get-Random -Max 256) })
# Ou (mais seguro): RandomNumberGenerator do .NET
$bytes = New-Object byte[] 32
[Security.Cryptography.RandomNumberGenerator]::Fill($bytes)
$key = -join ($bytes | ForEach-Object { '{0:x2}' -f $_ })
az keyvault secret set `
  --vault-name danieldev `
  --name DB-ENCRYPTION-KEY `
  --value $key

# DATABASE URL completa (postgres://user:pass@host:port/db?sslmode=...)
az keyvault secret set `
  --vault-name danieldev `
  --name DATABASE-URL `
  --value "postgres://gateway:gateway@localhost:5432/gateway?sslmode=disable"

# Content Safety (opcional — só se for usar Tier 3)
az keyvault secret set `
  --vault-name danieldev `
  --name AZURE-CS-API-KEY `
  --value "MEU-VALOR-AQUI"
```

> **Convenção de nome**: Key Vault só aceita `[a-zA-Z0-9-]`. Por isso usamos
> `-` (e não `_`) entre palavras. O gateway suporta exatamente esse charset
> no parser de `${kv:NAME}`.

Verifique:

```pwsh
az keyvault secret list --vault-name danieldev --query "[].name" -o tsv
```

---

## 4. Ativar no gateway

### 4.1 Setar `KEYVAULT_URI`

No `.env`:

```env
KEYVAULT_URI=https://danieldev.vault.azure.net/
```

### 4.2 Substituir as referências no `configs/gateway.yaml`

Trocar de:

```yaml
azure_openai:
  api_key: ${AZURE_OPENAI_API_KEY}

database:
  url: ${DATABASE_URL}
  encryption_key_hex: ${DB_ENCRYPTION_KEY}
```

Para:

```yaml
azure_openai:
  api_key: ${kv:AZURE-OPENAI-API-KEY}

database:
  url: ${kv:DATABASE-URL}
  encryption_key_hex: ${kv:DB-ENCRYPTION-KEY}
```

E pode remover essas linhas do `.env`. O `KEYVAULT_URI` e os endpoints
não-segredos (`AZURE_OPENAI_ENDPOINT`) continuam no `.env`.

### 4.3 Reiniciar o gateway

Pelo GoLand: Stop → Run. O log de boot deve mostrar 4 fetches do KV nos
primeiros segundos. Se o cliente CLI estiver autenticado mas sem permissão
nos secrets, a falha sai com `RESPONSE 403 Forbidden` e o gateway não sobe
(fail-fast — ADR-0018).

---

## 5. Em produção

O gateway é deployado em container (App Service, AKS, ACI etc.). Em prod,
em vez de `az login`, use **Managed Identity**:

1. **Atribua** uma Managed Identity (system-assigned ou user-assigned) ao
   recurso de compute.
2. **Permissione** essa identity no vault com o mesmo role
   `Key Vault Secrets User` (passo 2.2 — só troque `--assignee` pelo principal
   id da MI).
3. **Não precisa configurar nada no container** — `DefaultAzureCredential`
   detecta a MI automaticamente via endpoint do metadata service.

Resultado: zero credencial no container, segredos rotacionados centralmente
no KV, propagação ≤5 min (TTL do cache em memória).

---

## 6. Cache em memória — comportamento esperado

- Cada `${kv:NAME}` é resolvido uma vez no boot e em cada miss subsequente.
- TTL default: **5 minutos** (`keyvault.DefaultCacheTTL`). Após isso, o
  próximo `Get` do mesmo nome refaz a chamada ao Azure.
- O cache é compartilhado entre o resolver de boot e qualquer código futuro
  que queira ler segredos on-demand.
- **Sem refresh proativo**: o cache é lazy — o primeiro request pós-expiry
  paga ~150ms de latência da round-trip ao Azure.

---

## 7. Troubleshooting

| Sintoma | Diagnóstico |
|---|---|
| `config references N Key Vault secret(s) but KEYVAULT_URI is not configured` | YAML tem `${kv:…}` mas o `.env` não tem `KEYVAULT_URI`. Setar ou remover as referências |
| `creating Azure credential: ChainedTokenCredential authentication failed` | `az login` não feito ou expirou. Refaça `az login` |
| `RESPONSE 403 Forbidden` no Get | Seu usuário não tem o role `Key Vault Secrets User`. Refaça o passo 2.2 |
| `RESPONSE 404 Not Found` no Get | Secret com esse nome não existe. Cheque com `az keyvault secret list` |
| `secret "X" value is empty` | Secret existe mas está vazio. Faça `az keyvault secret set ... --value "..."` de novo |
| Boot leva 5+ segundos | Normal no cold start (4 secrets × ~150ms cada). Restarts subsequentes não pagam cache |
| Gateway "não vê" rotação que fiz no Azure | Aguarda até 5 min (TTL do cache). Se urgente, restart o gateway força refresh |

---

## 8. Voltando para `.env` (rollback)

Se precisar desligar o KV temporariamente:

1. Trocar `${kv:NAME}` por `${NAME}` no `gateway.yaml`
2. Setar as variáveis correspondentes no `.env`
3. Remover ou comentar `KEYVAULT_URI` no `.env`
4. Restart

O gateway não usa o KV se `KEYVAULT_URI` está vazio E nenhum `${kv:…}` está
no YAML.
