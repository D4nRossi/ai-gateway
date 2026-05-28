# Deploy Windows — IIS + WinSW

> Manual operacional pra subir o AI Gateway em Windows Server corporativo
> (air-gap parcial: Azure liberado, GitHub bloqueado). O binário Go roda
> como **Windows Service** via WinSW; o **IIS** termina TLS e faz proxy
> reverso pro gateway na porta 8080 (loopback).
>
> Owner mencionou: "menos otimizada possível com NGINX, DOCKER, etc." — em
> Windows seguimos **sem Docker**, **sem nginx**, usando ferramentas
> nativas do ecossistema MS (IIS + Service Control Manager via WinSW).

---

## 0. Pré-requisitos do servidor

| Item | Versão / requisito | Como verificar |
|---|---|---|
| OS | Windows Server 2019+ ou 2022 | `winver` |
| CPU | 2+ vCPU (recomendado 4) | `wmic cpu get NumberOfCores` |
| RAM | 4 GiB confortável | `systeminfo \| findstr Memory` |
| Disco | 10 GiB livres (binário + logs + IIS cache) | `Get-PSDrive C` (PowerShell) |
| .NET Framework 4.8 | Pra WinSW v3 | `Get-WindowsFeature NET-Framework-45-Features` |
| IIS 10.0+ | Role | `Get-WindowsFeature Web-Server` |
| IIS URL Rewrite Module 2.1 | — | conferir em IIS Manager → módulos |
| IIS Application Request Routing 3.0 | — | conferir em IIS Manager → módulos |
| `migrate` CLI | v4.18+ | `migrate -version` |
| `sqlcmd` ou SSMS | qualquer | — |

A **máquina de build (workstation)** precisa de Go 1.25+, Node 20+, internet.

---

## 1. Outbound permitido no firewall corporativo

Idêntico ao manual Linux — substitua endpoints e nomes pelos do seu ambiente.

| Host | Porta | Motivo |
|---|---|---|
| `*.cognitiveservices.azure.com` | 443 | Azure OpenAI / Language |
| `*.vault.azure.net` | 443 | Key Vault |
| `login.microsoftonline.com` | 443 | DefaultAzureCredential auth |
| `<sql-server-corp>` | 1433 | SQL Server interno |
| `<ntp-corp>` | 123 (UDP) | Sincronização de clock |

---

## 2. Build do artefato — feito na workstation, NÃO no servidor

### 2.1 Binário Windows

```powershell
# Na workstation (PowerShell)
cd C:\projects\ai-gateway

# Frontend embed
Push-Location web
npm ci
npm run build
Pop-Location

# Binário Windows estático, sem CGO
$env:CGO_ENABLED = "0"
$env:GOOS = "windows"
$env:GOARCH = "amd64"
$ver = (git rev-parse --short HEAD)
go build `
    -ldflags="-s -w -X main.Version=$ver" `
    -o dist\gateway.exe `
    ./cmd/gateway

# CLI de migração de credenciais (opcional, ADR-0020)
go build -o dist\migrate-targets-to-kv.exe ./cmd/migrate-targets-to-kv

# Hash pra auditoria
Get-FileHash dist\gateway.exe, dist\migrate-targets-to-kv.exe | Format-List
```

Saída: `dist\gateway.exe` (~30 MB estático), `dist\migrate-targets-to-kv.exe`.

### 2.2 Baixar WinSW (na workstation com internet)

```powershell
# WinSW v3 — Windows Service Wrapper. Single-file .exe.
# https://github.com/winsw/winsw/releases — baixar WinSW-x64.exe
Invoke-WebRequest `
    -Uri https://github.com/winsw/winsw/releases/download/v3.0.0-alpha.11/WinSW-x64.exe `
    -OutFile dist\WinSW-x64.exe
Get-FileHash dist\WinSW-x64.exe
```

### 2.3 Empacotar tudo

```powershell
# Copiar configs + migrations
Copy-Item configs\gateway.yaml dist\configs\ -Force
Copy-Item migrations\*.sql dist\migrations\
Copy-Item .env.example dist\gateway.env.template

# Zipar
Compress-Archive -Path dist\* -DestinationPath ai-gateway-deploy.zip -Force
Get-FileHash ai-gateway-deploy.zip
```

---

## 3. Transporte pro servidor

Copiar `ai-gateway-deploy.zip` + hash via:
- SMB share corporativo
- SCP via OpenSSH (Windows Server tem nativo desde 2019)
- USB removível com aprovação de segurança

No servidor, validar o hash antes de extrair:

```powershell
# No servidor
Get-FileHash C:\Temp\ai-gateway-deploy.zip
# Comparar com o hash da workstation
```

---

## 4. Setup do host

### 4.1 Conta de serviço

Recomendação corporativa: **gMSA (Group Managed Service Account)** atribuída
pelo time de identidade. Solicitar e instalar com:

```powershell
# Pré-requisito: domain admin já criou a gMSA
Install-ADServiceAccount -Identity gmsa_aigateway
# Teste
Test-ADServiceAccount gmsa_aigateway
# Esperado: True
```

**Alternativa pra dev**: criar conta local "ai-gateway" com `Logon as a service`
right.

### 4.2 Diretórios

```powershell
# PowerShell elevado
$root = "C:\AIGateway"
New-Item -ItemType Directory -Force -Path $root\bin, $root\configs, $root\migrations, $root\logs, $root\tls | Out-Null

# Extrair artefatos
Expand-Archive -Path C:\Temp\ai-gateway-deploy.zip -DestinationPath $root\staging
Move-Item $root\staging\gateway.exe $root\bin\
Move-Item $root\staging\migrate-targets-to-kv.exe $root\bin\
Move-Item $root\staging\WinSW-x64.exe $root\bin\gateway-service.exe
Copy-Item $root\staging\configs\gateway.yaml $root\configs\
Copy-Item $root\staging\migrations\* $root\migrations\
Remove-Item -Recurse $root\staging
```

### 4.3 Permissões NTFS

```powershell
$account = "DOMAIN\gmsa_aigateway$"     # ou .\ai-gateway pra conta local

# Logs: leitura/escrita pra service account
icacls C:\AIGateway\logs /grant "${account}:(OI)(CI)M" /T

# Bin: leitura/execução
icacls C:\AIGateway\bin /grant "${account}:(RX)" /T

# Configs: leitura
icacls C:\AIGateway\configs /grant "${account}:(R)" /T
icacls C:\AIGateway\migrations /grant "${account}:(R)" /T

# TLS keys: leitura apenas pra service account (e Administrators)
icacls C:\AIGateway\tls /inheritance:r
icacls C:\AIGateway\tls /grant "Administrators:(F)" "${account}:(R)" /T

# Negar Everyone fora isso
icacls C:\AIGateway /deny "Users:(W)"
```

### 4.4 TLS cert

O cert da CA corporativa deve estar instalado em **LocalMachine\My** (Personal
Store da máquina). Pra importar `.pfx`:

```powershell
# PowerShell elevado
$pfxPass = Read-Host -AsSecureString -Prompt "Senha do PFX"
Import-PfxCertificate -FilePath C:\AIGateway\tls\gateway.pfx `
    -CertStoreLocation Cert:\LocalMachine\My `
    -Password $pfxPass

# Pegar o thumbprint (anota — usado no IIS binding)
Get-ChildItem Cert:\LocalMachine\My | `
    Where-Object { $_.Subject -match "gateway-prod" } | `
    Select-Object Thumbprint, Subject
```

Garantir que a service account tem permissão de **leitura** na chave privada:

```powershell
# Via snap-in: certlm.msc → Personal → Certificates → cert → All Tasks → Manage Private Keys → Add gmsa_aigateway$ → Read
# OU via PowerShell:
$cert = Get-ChildItem Cert:\LocalMachine\My\<THUMBPRINT>
$keyPath = "C:\ProgramData\Microsoft\Crypto\RSA\MachineKeys\$($cert.PrivateKey.CspKeyContainerInfo.UniqueKeyContainerName)"
icacls $keyPath /grant "$account:(R)"
```

---

## 5. Configuração — `gateway.yaml` + `gateway.env`

### 5.1 `gateway.yaml`

Mesmo conteúdo do manual Linux — host SQL corp, secrets via `${kv:...}`,
`raw_prompt_logging: false`, etc. Editar:

```powershell
notepad C:\AIGateway\configs\gateway.yaml
```

### 5.2 `gateway.env`

`C:\AIGateway\configs\gateway.env`:

```env
KEYVAULT_URI=https://prod-vault.vault.azure.net/
AZURE_OPENAI_ENDPOINT=https://corp-openai.cognitiveservices.azure.com
AZURE_LANGUAGE_ENDPOINT=https://corp-language.cognitiveservices.azure.com

LOG_LEVEL=info

# ADR-0025: prod usa MANUAL migration mode.
MIGRATIONS_AUTO_APPLY=false

PROVIDER=azure
CONFIG_PATH=C:\AIGateway\configs\gateway.yaml
```

Permissões:

```powershell
icacls C:\AIGateway\configs\gateway.env /inheritance:r
icacls C:\AIGateway\configs\gateway.env /grant "Administrators:(F)" "$account:(R)"
```

### 5.3 Setup secrets sem Azure Key Vault (ADR-0026)

Quando o servidor não tem KV disponível, o gateway opera em **modo `db`**
com dois mecanismos complementares:

- **DPAPI** protege o `gateway.env` cifrado em disco com os 4 secrets de
  **boot** (senha do banco, API keys do Azure, AES master key).
- **SQL Server Always Encrypted** protege os secrets de **runtime** —
  apenas os `gateway-target-{uuid}` da Onda 4.5 na V1 (boot secrets ficam
  em DPAPI nesta versão; migração pra AE planejada).

**Variáveis novas no serviço:**

| Env | Valor |
|---|---|
| `SECRET_PROVIDER` | `db` |
| `DPAPI_ENV_FILE` | `C:\AIGateway\configs\gateway.env.dpapi` |
| `KEYVAULT_URI` | (não setar) |

#### 5.3.1 Gerar cert auto-assinado pro CMK

PowerShell elevado, no servidor:

```powershell
$cert = New-SelfSignedCertificate -Subject "CN=AIGateway-CMK" `
    -CertStoreLocation Cert:\LocalMachine\My `
    -KeyExportPolicy Exportable `
    -KeySpec KeyExchange `
    -KeyUsage KeyEncipherment, DataEncipherment `
    -NotAfter (Get-Date).AddYears(5)

$cert | Format-List Thumbprint, Subject, NotAfter
# Anota o Thumbprint — usado no CMK abaixo.
```

#### 5.3.2 Backup do PFX

**Crítico** — perda = perda permanente dos secrets cifrados:

```powershell
$pfxPath = "\\fs-corp\backups\ai-gateway\cmk\AIGateway-CMK-$(Get-Date -Format 'yyyyMMdd').pfx"
$pfxPass = Read-Host -AsSecureString -Prompt "Senha forte pro backup PFX"
Export-PfxCertificate -Cert "Cert:\LocalMachine\My\$($cert.Thumbprint)" `
    -FilePath $pfxPath -Password $pfxPass

# Verificar hash do backup pra audit
Get-FileHash $pfxPath | Format-List Hash
```

#### 5.3.3 Conceder leitura da chave privada à service account

```powershell
$account = "DOMAIN\gmsa_aigateway$"   # ou .\ai-gateway pra conta local
$rsa = [System.Security.Cryptography.X509Certificates.RSACertificateExtensions]::GetRSAPrivateKey($cert)
$keyName = $rsa.Key.UniqueName
$keyPath = "$env:ALLUSERSPROFILE\Microsoft\Crypto\Keys\$keyName"
icacls $keyPath /grant "${account}:(R)"
```

#### 5.3.4 Criar Column Master Key e Column Encryption Key

`sqlcmd` ou SSMS no servidor:

```sql
-- Substitua <THUMBPRINT> pelo valor do passo 5.3.1
CREATE COLUMN MASTER KEY AIGateway_CMK
    WITH (
        KEY_STORE_PROVIDER_NAME = 'MSSQL_CERTIFICATE_STORE',
        KEY_PATH = 'LocalMachine/My/<THUMBPRINT>'
    );
```

A CEK precisa ser gerada via SSMS UI (Object Explorer → Security → Always
Encrypted Keys → New Column Encryption Key) com referência ao CMK acima,
OU via `New-SqlColumnEncryptionKey` no PowerShell. SSMS gera a `ENCRYPTED_VALUE`
e emite o `CREATE COLUMN ENCRYPTION KEY` final.

Depois, ALTER COLUMN com a CEK criada:

```sql
ALTER TABLE gogateway.secrets
    ALTER COLUMN value VARBINARY(MAX)
        ENCRYPTED WITH (
            ENCRYPTION_TYPE = Randomized,
            ALGORITHM = 'AEAD_AES_256_CBC_HMAC_SHA_256',
            COLUMN_ENCRYPTION_KEY = AIGateway_CEK
        ) NOT NULL;
```

#### 5.3.5 Cifrar `gateway.env` com DPAPI

```powershell
# Conteúdo do .env (boot secrets + endpoints + flags)
$envContent = @"
DATABASE_PASSWORD=<senha-do-sql>
AZURE_OPENAI_API_KEY=<key>
AZURE_LANGUAGE_API_KEY=<key>
DB_ENCRYPTION_KEY_HEX=<64-char-hex>
AZURE_OPENAI_ENDPOINT=https://corp-openai.cognitiveservices.azure.com
AZURE_LANGUAGE_ENDPOINT=https://corp-language.cognitiveservices.azure.com
LOG_LEVEL=info
MIGRATIONS_AUTO_APPLY=false
PROVIDER=azure
CONFIG_PATH=C:\AIGateway\configs\gateway.yaml
"@

$bytes = [System.Text.Encoding]::UTF8.GetBytes($envContent)
$cipher = [System.Security.Cryptography.ProtectedData]::Protect(
    $bytes, $null, [System.Security.Cryptography.DataProtectionScope]::LocalMachine)
[System.IO.File]::WriteAllBytes('C:\AIGateway\configs\gateway.env.dpapi', $cipher)

# Permissões: SYSTEM full, gmsa read, ninguém mais
icacls C:\AIGateway\configs\gateway.env.dpapi /inheritance:r
icacls C:\AIGateway\configs\gateway.env.dpapi /grant "SYSTEM:(F)" "${account}:(R)"
```

#### 5.3.6 Yaml em modo `db`

Em modo `db`, o yaml referencia env vars (preenchidas pelo DPAPI loader)
em vez de `${kv:...}`. Edite `C:\AIGateway\configs\gateway.yaml`:

```yaml
database:
  host: sql-corp
  port: 1433
  database: AIGateway_prod
  user: usr_app
  password: ${DATABASE_PASSWORD}                # env DPAPI
  schema: gogateway
  encrypt: true
  trust_server_certificate: false
  encryption_key_hex: ${DB_ENCRYPTION_KEY_HEX}  # env DPAPI

azure_openai:
  endpoint: ${AZURE_OPENAI_ENDPOINT}
  api_key: ${AZURE_OPENAI_API_KEY}              # env DPAPI

azure_language:
  endpoint: ${AZURE_LANGUAGE_ENDPOINT}
  api_key: ${AZURE_LANGUAGE_API_KEY}            # env DPAPI
  language: pt-BR
```

#### 5.3.7 Connection string com `ColumnEncryption=true`

Pra que o driver descriptografe AE em runtime (consumidores futuros da
`gogateway.secrets`), adicione `columnencryption=true` na connection string
construída internamente. Hoje o gateway monta a connection string a partir
de `cfg.Database.*`. O suporte AE no boot fica para uma iteração futura;
**na V1 atual, `gogateway.secrets` é usada apenas pelos `gateway-target-{uuid}`
da Onda 4.5**, lidos sob demanda pelo `CredentialResolver`.

#### 5.3.8 Popular secrets de target (Onda 4.5) via CLI

Após migrar targets pra modo `kv` ou `both` (ADR-0020), a credencial vai
parar em `gogateway.secrets`:

```powershell
# Configurar conexão (CLI roda no servidor)
$env:DATABASE_URL = "sqlserver://usr_app:<SENHA>@sql-corp:1433?database=AIGateway_prod&encrypt=true&columnencryption=true"

# Inspecionar o que já existe
C:\AIGateway\bin\secrets.exe list

# Adicionar secret manualmente (raro — UI/CLI da Onda 4.5 normalmente faz)
"my-secret-value" | C:\AIGateway\bin\secrets.exe set --name gateway-target-018d9-xxx

# Rotacionar
"new-value" | C:\AIGateway\bin\secrets.exe rotate --name gateway-target-018d9-xxx

# Visualizar (apenas em emergência — gated)
$env:GATEWAY_SECRETS_ALLOW_GET = "1"
C:\AIGateway\bin\secrets.exe get --name gateway-target-018d9-xxx
```

#### 5.3.9 Reconfigurar serviço pra modo `db`

Editar `C:\AIGateway\bin\gateway-service.xml`:

```xml
<env name="SECRET_PROVIDER" value="db"/>
<env name="DPAPI_ENV_FILE" value="C:\AIGateway\configs\gateway.env.dpapi"/>
<!-- NÃO setar KEYVAULT_URI em modo db -->
```

Reinstalar o serviço pra aplicar:

```powershell
C:\AIGateway\bin\gateway-service.exe stop
C:\AIGateway\bin\gateway-service.exe uninstall
C:\AIGateway\bin\gateway-service.exe install
C:\AIGateway\bin\gateway-service.exe start
```

#### 5.3.10 Renovação do cert do CMK (1×/ano)

Cert auto-assinado tem validade default de 5 anos no comando acima — antes
de expirar, processo é: gerar novo cert → criar novo CMK referenciando ele
→ adicionar a CEK existente com nova `ENCRYPTED_VALUE` cifrada pelo novo
CMK → revogar CMK antigo. SSMS UI orienta pelo wizard "Rotate Column
Master Keys". Documentar a data no calendário corporativo.

### 5.4 Auth pro Azure (Managed Identity em VMs Azure)

Em VMs Azure de produção, usar **System Assigned Managed Identity** atribuída
à VM. O `DefaultAzureCredential` no Go SDK detecta MI automaticamente — **sem
config no `.env`**.

Pré-requisito: time de Azure atribui MI à VM + concede `Key Vault Secrets User`
no KV de produção.

Pra VMs on-prem ou ambientes ainda sem MI: usar service principal via env vars
(`AZURE_TENANT_ID`, `AZURE_CLIENT_ID`, `AZURE_CLIENT_SECRET`).

---

## 6. Database — aplicação manual de migrations (ADR-0025)

Igual ao manual Linux. Aplicar do laptop/jumpbox (não do Windows Server de
produção, idealmente):

```powershell
$env:DATABASE_URL = "sqlserver://usr_app:$pwd@sql-corp:1433?database=AIGateway_prod&encrypt=true&trustServerCertificate=false"

# Aplicar tudo
migrate -database "$env:DATABASE_URL" -path migrations up

# Conferir
migrate -database "$env:DATABASE_URL" -path migrations version
```

Procedimentos de rollback e cleanup de `dirty=1` idênticos ao manual Linux §6.

---

## 7. WinSW — gateway como Windows Service

WinSW (Windows Service Wrapper) é um wrapper open-source mantido pelo time
do Jenkins, single-file `.exe`, configuração XML declarativa, log rotation
nativa, restart policy declarativa. Mais ergonômico que NSSM (legado, sem
rotação nativa) e que `sc.exe` puro (sem rotação, sem env file).

### 7.1 Config XML do serviço

`C:\AIGateway\bin\gateway-service.xml`:

```xml
<service>
  <id>AIGateway</id>
  <name>AI Gateway</name>
  <description>AI Gateway (Go) — proxy + governance pra Azure OpenAI</description>

  <!-- Binário a executar -->
  <executable>C:\AIGateway\bin\gateway.exe</executable>

  <!-- Working directory -->
  <workingdirectory>C:\AIGateway</workingdirectory>

  <!-- Conta de serviço — gMSA (recomendado em prod) -->
  <serviceaccount>
    <domain>DOMAIN</domain>
    <user>gmsa_aigateway$</user>
    <!-- Pra gMSA não há senha; pra conta local com senha, adicionar <password>...</password> -->
  </serviceaccount>

  <!-- Env vars carregadas do gateway.env. Suporta arquivo de propriedades
       no formato KEY=VALUE; comentários com #. -->
  <env name="KEYVAULT_URI" value="https://prod-vault.vault.azure.net/"/>
  <env name="AZURE_OPENAI_ENDPOINT" value="https://corp-openai.cognitiveservices.azure.com"/>
  <env name="AZURE_LANGUAGE_ENDPOINT" value="https://corp-language.cognitiveservices.azure.com"/>
  <env name="LOG_LEVEL" value="info"/>
  <env name="MIGRATIONS_AUTO_APPLY" value="false"/>
  <env name="PROVIDER" value="azure"/>
  <env name="CONFIG_PATH" value="C:\AIGateway\configs\gateway.yaml"/>

  <!-- Restart automático em falha. Tentativas 5 vezes com backoff de 5s. -->
  <onfailure action="restart" delay="5 sec"/>
  <onfailure action="restart" delay="10 sec"/>
  <onfailure action="restart" delay="30 sec"/>

  <!-- Logs — arquivo rotacionado por tamanho. Centralização vai pro banco
       conforme decisão do owner; aqui é só fallback local pra diagnóstico. -->
  <log mode="roll-by-size">
    <sizeThreshold>10240</sizeThreshold>   <!-- 10 MiB por arquivo -->
    <keepFiles>10</keepFiles>              <!-- Mantém 10 (100 MiB total) -->
  </log>
  <logpath>C:\AIGateway\logs</logpath>

  <!-- Tipo de log: separar stdout (info) e stderr (errors) -->
  <outfilepattern>.out.log</outfilepattern>
  <errfilepattern>.err.log</errfilepattern>

  <!-- Tempo máximo de shutdown gracioso antes do kill (segundos) -->
  <stoptimeout>30 sec</stoptimeout>

  <!-- Priority do processo -->
  <priority>Normal</priority>
</service>
```

> **Por que não colocar segredos no XML?** Em geral, env vars com segredos
> devem vir de uma fonte cifrada (KV). Os secrets do gateway já vem do KV
> via `${kv:...}` — só endpoints e flags entram no XML.

### 7.2 Instalar o serviço

```powershell
# PowerShell elevado, do diretório onde está gateway-service.exe e .xml
cd C:\AIGateway\bin

.\gateway-service.exe install
# saída: "Service installed."

# Iniciar
.\gateway-service.exe start
# OU
Start-Service AIGateway

# Status
Get-Service AIGateway
# esperado: Status = Running, StartType = Automatic
```

### 7.3 Comandos do dia-a-dia

```powershell
# Restart
.\gateway-service.exe restart
# OU
Restart-Service AIGateway

# Stop
Stop-Service AIGateway

# Uninstall (raro — usado em rollback ou upgrade major)
.\gateway-service.exe uninstall

# Ver logs ao vivo
Get-Content C:\AIGateway\logs\AIGateway.out.log -Wait -Tail 50
Get-Content C:\AIGateway\logs\AIGateway.err.log -Wait -Tail 50
```

### 7.4 Auto-start no boot

`gateway-service.exe install` já configura **Start Type: Automatic**. Conferir
no `services.msc`.

---

## 8. IIS como reverse proxy + TLS

IIS faz exatamente o que nginx faz no Linux: termina TLS, encaminha pra
`localhost:8080`. Os módulos `URL Rewrite` e `Application Request Routing
(ARR)` viabilizam reverse proxy nativo.

### 8.1 Instalar módulos

```powershell
# Pré-requisito: IIS feature já instalada
Install-WindowsFeature -Name Web-Server, Web-Mgmt-Console, Web-Asp-Net45

# URL Rewrite + ARR — baixar e instalar MSIs na workstation com internet,
# transportar e instalar no servidor:
#   https://www.iis.net/downloads/microsoft/url-rewrite
#   https://www.iis.net/downloads/microsoft/application-request-routing

# Após instalação dos MSIs:
Start-WebAppPool DefaultAppPool   # ou criar pool dedicado (recomendado)
```

### 8.2 App Pool dedicado (recomendado)

```powershell
Import-Module WebAdministration

# Pool dedicado, sem managed code (proxy puro)
New-WebAppPool -Name "AIGatewayProxy"
Set-ItemProperty IIS:\AppPools\AIGatewayProxy -Name managedRuntimeVersion -Value ""
Set-ItemProperty IIS:\AppPools\AIGatewayProxy -Name processModel.identityType -Value SpecificUser
# Se usar gMSA, configurar via app pool identity (UI Manager — IIS não aceita gMSA pra app pool diretamente em todas as versões; alternativa é NetworkService)
```

### 8.3 Site IIS com bindings

```powershell
# Remove site default se existir
Remove-WebSite -Name "Default Web Site" -ErrorAction SilentlyContinue

# Site novo bindando 443 + 80 com cert da CA corp
$thumbprint = "<COLE_O_THUMBPRINT_DO_§4.4>"
New-WebSite -Name "AIGatewayProxy" `
    -Port 80 `
    -ApplicationPool "AIGatewayProxy" `
    -PhysicalPath "C:\inetpub\wwwroot\AIGateway"

New-WebBinding -Name "AIGatewayProxy" -Protocol https -Port 443
Get-WebBinding -Name "AIGatewayProxy" -Protocol https | `
    Set-WebBinding -PropertyName SslFlags -Value 0
# Associar cert pela CLI:
$bindingHttps = (Get-WebBinding -Name "AIGatewayProxy" -Protocol https).bindingInformation
& netsh http add sslcert hostnameport=":443" certhash=$thumbprint appid="{00000000-0000-0000-0000-000000000000}"
# OU via IIS Manager UI: Site → Bindings → https → cert dropdown

New-Item -ItemType Directory -Path C:\inetpub\wwwroot\AIGateway -Force
```

### 8.4 `web.config` — proxy reverso pro gateway

`C:\inetpub\wwwroot\AIGateway\web.config`:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<configuration>
  <system.webServer>

    <!-- HSTS + redirect HTTP → HTTPS -->
    <rewrite>
      <rules>

        <!-- 1. Force HTTPS (HTTP requests recebem 301 pra https://) -->
        <rule name="HTTPS Redirect" stopProcessing="true">
          <match url="(.*)" />
          <conditions>
            <add input="{HTTPS}" pattern="^OFF$" />
          </conditions>
          <action type="Redirect" url="https://{HTTP_HOST}/{R:1}" redirectType="Permanent" />
        </rule>

        <!-- 2. Proxy reverso pra 127.0.0.1:8080 (gateway) -->
        <rule name="ReverseProxyToGateway" stopProcessing="true">
          <match url="(.*)" />
          <action type="Rewrite" url="http://127.0.0.1:8080/{R:1}" />
          <serverVariables>
            <set name="HTTP_X_FORWARDED_PROTO" value="https" />
            <set name="HTTP_X_FORWARDED_HOST" value="{HTTP_HOST}" />
            <set name="HTTP_X_REAL_IP" value="{REMOTE_ADDR}" />
          </serverVariables>
        </rule>

      </rules>

      <!-- Allowlist de server variables que o ARR pode escrever -->
      <allowedServerVariables>
        <add name="HTTP_X_FORWARDED_PROTO" />
        <add name="HTTP_X_FORWARDED_HOST" />
        <add name="HTTP_X_REAL_IP" />
      </allowedServerVariables>
    </rewrite>

    <!-- Headers de segurança -->
    <httpProtocol>
      <customHeaders>
        <add name="Strict-Transport-Security" value="max-age=31536000; includeSubDomains" />
        <add name="X-Content-Type-Options" value="nosniff" />
        <add name="X-Frame-Options" value="DENY" />
        <remove name="X-Powered-By" />
      </customHeaders>
    </httpProtocol>

    <!-- Body size — chat completions com prompt longo -->
    <security>
      <requestFiltering>
        <requestLimits maxAllowedContentLength="5242880" />  <!-- 5 MiB -->
      </requestFiltering>
    </security>

  </system.webServer>
</configuration>
```

### 8.5 Configurar ARR pra desabilitar buffering (SSE-friendly)

ARR por padrão faz buffering — quebra streaming SSE do `/v1/chat/completions`
com `stream=true`. Desabilitar **server-wide**:

```powershell
# Via PowerShell (mais reproduzível)
Set-WebConfigurationProperty `
    -PSPath "MACHINE/WEBROOT/APPHOST" `
    -Filter "system.webServer/proxy" `
    -Name "enabled" -Value "True"

Set-WebConfigurationProperty `
    -PSPath "MACHINE/WEBROOT/APPHOST" `
    -Filter "system.webServer/proxy" `
    -Name "responseBufferLimit" -Value "0"

# Reset IIS
iisreset
```

OU via IIS Manager: clicar no servidor → Application Request Routing Cache →
Server Proxy Settings → check "Enable proxy" + set "Response buffer limit" = 0.

### 8.6 Validar bindings

```powershell
# HTTPS responde?
Invoke-WebRequest -Uri https://localhost/healthz -SkipCertificateCheck
# Esperado: 200 + body do healthz

# HTTP redireciona?
Invoke-WebRequest -Uri http://localhost/healthz -MaximumRedirection 0
# Esperado: 301 com Location: https://localhost/healthz
```

---

## 9. Smoke test pós-deploy

```powershell
# 1. Liveness
Invoke-WebRequest -Uri https://gateway-prod/healthz -SkipCertificateCheck

# 2. Readiness
Invoke-WebRequest -Uri https://gateway-prod/readyz -SkipCertificateCheck

# 3. Login admin
$body = @{ username = "<admin>"; password = "<senha>" } | ConvertTo-Json
$login = Invoke-RestMethod `
    -Uri https://gateway-prod/admin/v1/auth/login `
    -Method POST -Body $body -ContentType "application/json" `
    -SkipCertificateCheck
$adminToken = $login.token

# 4. Chat completion
$chatBody = @{
    model = "gpt-4.1-nano"
    messages = @(@{ role = "user"; content = "diga oi" })
    max_tokens = 20
} | ConvertTo-Json
$headers = @{ Authorization = "Bearer <APP_TOKEN>"; "Content-Type" = "application/json" }
Invoke-RestMethod `
    -Uri https://gateway-prod/v1/chat/completions `
    -Method POST -Body $chatBody -Headers $headers `
    -SkipCertificateCheck

# 5. Conferir usage_event no banco
sqlcmd -S sql-corp -d AIGateway_prod -U usr_app -Q `
    "SELECT TOP 5 created_at, application_name, model, total_tokens, status_code FROM gogateway.usage_events ORDER BY created_at DESC"
```

---

## 10. Logs e troubleshooting

### Onde ficam

| Componente | Path |
|---|---|
| Gateway stdout | `C:\AIGateway\logs\AIGateway.out.log` (rotacionado por 10 MiB) |
| Gateway stderr | `C:\AIGateway\logs\AIGateway.err.log` |
| WinSW próprio | `C:\AIGateway\logs\AIGateway.wrapper.log` |
| IIS access | `C:\inetpub\logs\LogFiles\W3SVC*\u_ex*.log` |
| IIS error | `C:\inetpub\logs\HTTPERR\httperr*.log` |
| Event Log do Windows (start/stop do serviço) | Visualizador de Eventos → Windows Logs → Application |

### Cenários comuns

**Service não inicia — Event ID 7000:**
- Conta de serviço sem `Logon as a service`. Ajustar via `secpol.msc` → User Rights Assignment.

**`KEYVAULT_URI is empty` no `.err.log`:**
- Env vars do `<env>` no XML não carregaram. Verificar XML formatado corretamente.

**Erro `ErrSchemaOutOfDate` no boot:**
- DBA não aplicou migration. Rodar `migrate up` (§6) e reiniciar.

**IIS retorna 502.3 ou 502.5 Gateway:**
- Gateway não está rodando OU ARR proxy não está habilitado. Confirmar:
  ```powershell
  Get-Service AIGateway   # tem que estar Running
  Test-NetConnection -ComputerName localhost -Port 8080  # tem que ser TcpTestSucceeded
  ```

**Cert SSL não aceitando — IIS retorna 0x80092004:**
- App pool identity não tem permissão de leitura na chave privada. Voltar pro §4.4.

**Streaming SSE quebrando no browser/cliente:**
- ARR ainda está bufferizando. Re-verificar §8.5.

**Performance ruim:**
- App pool com `processModel.idleTimeout` curto faz cold start. Setar pra 0:
  ```powershell
  Set-ItemProperty IIS:\AppPools\AIGatewayProxy `
      -Name processModel.idleTimeout -Value "00:00:00"
  ```

---

## 11. Manutenção operacional

### 11.1 Rotação de credencial Azure OpenAI

Idêntico ao Linux: cache do KV no gateway expira em 5min (ADR-0018). Restart
imediato:

```powershell
Restart-Service AIGateway
```

### 11.2 Rotação de DB_ENCRYPTION_KEY

Mesma estratégia da Onda 4.5: mover todos targets pra mode=both ou kv antes
de rotacionar. CLI:

```powershell
& C:\AIGateway\bin\migrate-targets-to-kv.exe -target-id N -mode both
```

### 11.3 Backup / DR

- **DB**: cobertos pela política do SQL Server corporativo
- **Logs**: rotação WinSW mantém 100 MiB. Pra reter mais, configurar
  task agendada que copia `.log.{N}` pro storage corp:
  ```powershell
  # Exemplo: rsync-like via robocopy diariamente às 02:00
  Register-ScheduledTask -TaskName "AIGatewayLogArchive" `
      -Trigger (New-ScheduledTaskTrigger -Daily -At 2am) `
      -Action (New-ScheduledTaskAction -Execute robocopy `
          -Argument "C:\AIGateway\logs \\fs-corp\backups\ai-gateway\logs *.log.* /MOV /R:3 /W:5")
  ```

### 11.4 Rolling upgrade

1. **DBA** aplica migrations pendentes
2. **Operator** copia `gateway-novaversao.exe` pro servidor e renomeia
   `gateway.exe` atual pra `gateway.exe.previous`
3. `Stop-Service AIGateway`
4. Substitui binário
5. `Start-Service AIGateway`
6. Smoke test (§9)

---

## 12. Rollback procedure

**Manter binário N-1 ao lado**: `gateway.exe.previous` sempre presente.

```powershell
Stop-Service AIGateway
Move-Item C:\AIGateway\bin\gateway.exe C:\AIGateway\bin\gateway.exe.failed -Force
Move-Item C:\AIGateway\bin\gateway.exe.previous C:\AIGateway\bin\gateway.exe -Force
Start-Service AIGateway
```

Se a migration nova precisa ser revertida:

```powershell
migrate -database "$env:DATABASE_URL" -path migrations down 1
```

> Cuidado com `down` que faz DROP COLUMN (perda de dados). Sempre revisar
> o `.down.sql` antes.

---

## 13. Apêndices

### 13.1 Catálogo de env vars

Mesmo do manual Linux (§11.1). Setadas no `<env>` do WinSW XML em vez de
`/etc/...env`.

### 13.2 Portas

| Porta | Quem responde | TLS |
|---|---|---|
| 443 | IIS → gateway | Sim |
| 80 | IIS (redirect 301 → 443) | — |
| 8080 | Gateway (loopback 127.0.0.1 — não exposto externamente) | Não |

Firewall do Windows: 443 e 80 permitidos pelo IIS automaticamente. 8080 NÃO
deve ser permitido — gateway só fala com IIS na loopback.

### 13.3 Paths importantes

| Path | Conteúdo |
|---|---|
| `C:\AIGateway\bin\gateway.exe` | Binário |
| `C:\AIGateway\bin\gateway-service.exe` | WinSW wrapper |
| `C:\AIGateway\bin\gateway-service.xml` | Config do serviço |
| `C:\AIGateway\configs\gateway.yaml` | Config principal |
| `C:\AIGateway\logs\*.log` | Logs do app (rotacionados) |
| `C:\AIGateway\tls\` | Cert .pfx (chave privada no store) |
| `C:\inetpub\wwwroot\AIGateway\web.config` | Config do reverse proxy IIS |

### 13.4 Comandos úteis de runtime

```powershell
# Service
Get-Service AIGateway
Restart-Service AIGateway
Get-Content C:\AIGateway\logs\AIGateway.out.log -Wait -Tail 50

# IIS
iisreset
Get-WebSite
Get-Counter '\Web Service(_Total)\Current Connections'

# Schema migrations
sqlcmd -S sql-corp -d AIGateway_prod -U usr_app -Q `
    "SELECT version, dirty FROM dbo.schema_migrations"

# Últimas chamadas
sqlcmd -S sql-corp -d AIGateway_prod -U usr_app -Q `
    "SELECT TOP 10 created_at, application_name, model, latency_ms, status_code, total_tokens FROM gogateway.usage_events ORDER BY created_at DESC"
```

---

## 14. Referências

- ADR-0010 — generic HTTP proxy engine
- ADR-0018 — Key Vault provider
- ADR-0020 — credential storage mode per target
- ADR-0022 — troca PG → SQL Server
- ADR-0024 — usage tracking no proxy plane
- ADR-0025 — MIGRATIONS_AUTO_APPLY toggle
- `docs/deploy/linux.md` — manual equivalente Linux
- WinSW (Windows Service Wrapper): https://github.com/winsw/winsw
- IIS URL Rewrite: https://www.iis.net/downloads/microsoft/url-rewrite
- IIS Application Request Routing: https://www.iis.net/downloads/microsoft/application-request-routing
- CLAUDE.md §4.4 — versões pinadas
