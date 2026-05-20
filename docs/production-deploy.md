# Deploy em produção

Este guia cobre o build da imagem Docker, configuração de ambiente, setup do Azure e checklist de segurança para deploy.

---

## 1. Configurar Azure OpenAI

### 1.1 Criar recurso Azure OpenAI

1. Azure Portal → **Create a resource** → pesquise "Azure OpenAI"
2. Crie o recurso em uma região com suporte ao modelo desejado
3. Em **Keys and Endpoint** anote:
   - `Endpoint` → `AZURE_OPENAI_ENDPOINT`
   - `Key 1` ou `Key 2` → `AZURE_OPENAI_API_KEY`

### 1.2 Criar deployments (modelos)

1. Azure AI Foundry → **Deployments** → **Create new deployment**
2. Para cada modelo no `gateway.yaml`:

   | `models[].public_name` | Tipo de modelo | `models[].deployment` |
   |---|---|---|
   | `gpt-4.1-mini` | `gpt-4.1` | nome que você escolher (ex: `gpt-4.1-mini-deploy`) |
   | `gpt-4.1-nano` | `gpt-4.1-nano` | nome que você escolher |

3. Certifique-se que o `deployment` no YAML corresponde exatamente ao nome criado no Foundry.

### 1.3 (Opcional) Azure Content Safety para Tier 3

1. Azure Portal → **Create a resource** → "Content Safety"
2. Em **Keys and Endpoint** anote:
   - `Endpoint` → `AZURE_CS_ENDPOINT`
   - `Key 1` → `AZURE_CS_API_KEY`
3. Verifique que **Prompt Shield** está disponível na região escolhida.

Se o recurso Content Safety não for configurado, Tier 3 usa heurística local (keywords) e falha-fechado. Para uma demo, isso é aceitável.

---

## 2. Preparar as aplicações (tokens)

Para cada aplicação consumidora, gere um token e seu hash:

```bash
# Token format: gwk_<prefix>_<random_secret>
# Recomendado: usar openssl para gerar segredo forte

TOKEN="gwk_appnome_$(openssl rand -hex 24)"
echo "Token: $TOKEN"
echo "Hash:  $(echo -n "$TOKEN" | sha256sum | cut -d' ' -f1)"
```

Distribua o **token completo** para a aplicação consumidora (ela usará no header `Authorization: Bearer`).
Armazene o **hash** no `configs/gateway.yaml` no campo `key_hash`.

**Nunca armazene o token completo em nenhum sistema. Apenas o hash fica no gateway.**

---

## 3. Configurar `configs/gateway.yaml`

Edite o arquivo para sua aplicação real. Pontos de atenção:

```yaml
azure_openai:
  api_version: "2024-10-21"   # confirmar versão disponível na sua região
                               # https://learn.microsoft.com/en-us/azure/ai-services/openai/api-version-deprecation

models:
  - public_name: gpt-4.1-mini
    deployment: NOME-EXATO-DO-DEPLOYMENT-NO-AZURE  # deve bater com o criado no Foundry

applications:
  - name: MinhaApp
    key_prefix: gwk_minhaapp   # deve começar com "gwk_" + prefixo único
    key_hash: "64charactershex"  # sha256 do token completo
    tier: tier_1                 # tier_1, tier_2 ou tier_3
    allowed_models: [gpt-4.1-nano]
    streaming_allowed: true
    max_rpm: 60
    max_tpm: 50000
    monthly_budget_brl: 100.00
```

---

## 4. Build da imagem Docker

```bash
# Build
docker build -t ai-gateway:1.0.0 .

# Verificar
docker run --rm ai-gateway:1.0.0 ./ai-gateway --help 2>&1 || true
```

### Tag e push para registro (exemplo com ACR)

```bash
ACR_NAME=seuregistro.azurecr.io

az acr login --name seuregistro

docker tag ai-gateway:1.0.0 $ACR_NAME/ai-gateway:1.0.0
docker push $ACR_NAME/ai-gateway:1.0.0
```

---

## 5. Variáveis de ambiente obrigatórias em produção

Nunca passe segredos em `configs/gateway.yaml` diretamente. O YAML usa `${VAR}` expandido no boot.

| Variável | Descrição |
|---|---|
| `DATABASE_URL` | `postgres://user:pass@host:5432/dbname?sslmode=require` |
| `AZURE_OPENAI_ENDPOINT` | URL do recurso Azure OpenAI |
| `AZURE_OPENAI_API_KEY` | Chave da API Azure OpenAI |
| `AZURE_CS_ENDPOINT` | (Tier 3) URL do Content Safety |
| `AZURE_CS_API_KEY` | (Tier 3) Chave do Content Safety |

Em produção, use `sslmode=require` na `DATABASE_URL` para criptografar a conexão com o banco.

---

## 6. Docker Compose para produção simples

Para ambientes de demo/staging em VM única:

```bash
# Criar arquivo .env de produção (NUNCA commitar)
cat > .env.prod << 'EOF'
AZURE_OPENAI_ENDPOINT=https://seu-recurso.openai.azure.com
AZURE_OPENAI_API_KEY=sua-chave-producao
AZURE_CS_ENDPOINT=https://seu-cs.cognitiveservices.azure.com
AZURE_CS_API_KEY=sua-chave-cs
EOF

# Subir
docker compose --env-file .env.prod up -d

# Ver logs
docker compose logs -f gateway
```

Para produção real (multi-instância), substitua Docker Compose por Kubernetes ou Azure Container Apps.

---

## 7. Checklist de segurança antes do go-live

### Configuração
- [ ] `key_hash` de todas as aplicações são SHA-256 reais (64 chars hex), não `<sha256 hex>`
- [ ] `monthly_budget_brl` definido para cada aplicação
- [ ] `logging.raw_prompt_logging: false` (nunca mudar em produção)
- [ ] `azure_openai.api_key` e `database.url` nunca escritos diretamente no YAML — apenas `${VAR}`
- [ ] `configs/gateway.yaml` não contém valores de chaves reais

### Rede
- [ ] Gateway não exposto diretamente na internet — está atrás de NGINX, API Gateway ou firewall
- [ ] TLS terminado no load balancer / edge (o gateway não faz TLS por design — SPEC §14.5)
- [ ] `DATABASE_URL` usa `sslmode=require`
- [ ] Porta 5432 do PostgreSQL não está exposta externamente

### Container
- [ ] Imagem usa `alpine:3.21` (não `latest`)
- [ ] Container roda como usuário não-root `app` (definido no Dockerfile)
- [ ] Não há shell ou ferramentas desnecessárias na imagem final

### Segredos
- [ ] `.env` não está em repositórios de código
- [ ] Segredos gerenciados via vault (Key Vault, AWS Secrets Manager, etc.) em prod real
- [ ] Chaves Azure têm o mínimo de permissões necessárias (Cognitive Services User)

### Operacional
- [ ] `/readyz` configurado como health check no load balancer
- [ ] Alertas configurados para logs `"level":"ERROR"` e `event_type=panic_recovered`
- [ ] Retenção de `audit_events` e `usage_events` definida (ex: particionar por mês)

---

## 8. Rotação de segredos Azure

Quando rotacionar a `AZURE_OPENAI_API_KEY`:

1. No Azure Portal, regenere a Key 2 (mantendo Key 1 ativa)
2. Atualize a variável de ambiente do gateway para a Key 2
3. Reinicie o gateway (`docker compose restart gateway`)
4. Confirme que os logs não mostram erros de auth com Azure
5. Regenere a Key 1 para invalidar a chave antiga
