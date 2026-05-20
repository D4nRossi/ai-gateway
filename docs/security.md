# Análise de Segurança — STRIDE

**Data:** 2026-05-20  
**Versão analisada:** commit `8f32a73` (branch `main`)  
**Metodologia:** STRIDE (Microsoft)  
**Escopo:** gateway HTTP + PostgreSQL, deploy OnPrem em Oracle Linux via Docker Compose  
**Analista:** Claude Sonnet 4.6 (revisão obrigatória por Danirek antes de go-live)

---

## 1. Superfície de ataque mapeada

```
Internet / Rede interna
        │
        ▼ (porta exposta pelo reverse proxy)
[nginx / haproxy]  ← TLS termina aqui
        │
        ▼ :8080 (rede Docker interna — nunca exposta diretamente)
[AI Gateway container]
        │                │
        ▼                ▼
[PostgreSQL :5432]   [Azure OpenAI HTTPS]
  (rede Docker)       (internet — TLS nativa)
```

Endpoints públicos (sem auth): `GET /healthz`, `GET /readyz`  
Endpoints autenticados: `GET /v1/models`, `POST /v1/chat/completions`

---

## 2. Tabela STRIDE

| ID | Categoria | Ameaça | Severidade | Estado |
|---|---|---|---|---|
| **S1** | Spoofing | Força bruta em bearer tokens sem lockout por IP | ALTA | ⚠️ Gap — mitigado por arquitetura (ver ação) |
| **S2** | Spoofing | Enumeração de prefixos (gwk_basic, gwk_pro visíveis no YAML) | BAIXA | Aceito |
| **S3** | Spoofing | MITM entre gateway e Azure OpenAI | BAIXA | ✅ Mitigado (HTTPS nativo Go valida TLS) |
| **T1** | Tampering | Alteração do `gateway.yaml` (key_hash das apps) | MÉDIA | ⚠️ Requer permissões de arquivo |
| **T2** | Tampering | Alteração de registros em `audit_events` / `usage_events` | MÉDIA | Aceito (log mutável — Fase 2 melhora) |
| **T3** | Tampering | Tráfego interno sem TLS (gateway ↔ apps consumidoras) | MÉDIA | ⚠️ Requer TLS no reverse proxy |
| **R1** | Repudiation | Audit log sem integridade criptográfica (registros mutáveis) | MÉDIA | Aceito (Fase 1) |
| **R2** | Repudiation | Rotação de token apaga prova de autoria histórica | BAIXA | Aceito |
| **I1** | Info Disclosure | `AZURE_OPENAI_API_KEY` no ambiente do processo | MÉDIA | ✅ Mitigado via env var + `.env` com permissão restrita |
| **I2** | Info Disclosure | Códigos de erro revelam configuração interna (model_not_allowed, budget_exceeded) | BAIXA | Aceito (necessário para client-side handling) |
| **I3** | Info Disclosure | `remote_addr` logado por request | BAIXA | Aceito |
| **I4** | Info Disclosure | Stack trace em panic logado (sem retornar ao cliente) | BAIXA | ✅ Mitigado (Recover middleware não expõe ao cliente) |
| **D1** | DoS | Sem rate limit em requisições não autenticadas | MÉDIA | ⚠️ Mitigado por arquitetura (nginx rate limit) |
| **D2** | DoS | `WriteTimeout: 0` permite SSE hold-open indefinido | MÉDIA | ⚠️ Aceito por design (ADR-0008) — requer limite de conexões |
| **D3** | DoS | `/readyz` faz HEAD ao Azure (5s timeout) — pode ser usado como amplificador | BAIXA | Aceito |
| **D4** | DoS | Budget fail-open sob pressão no banco | BAIXA | Aceito (SPEC §13.2) |
| **E1** | Elevation | Tier 1 não detecta injeção de prompt (por design) | MÉDIA | Aceito (SPEC §5.3) — documentar para apps Tier 1 |
| **E2** | Elevation | Sem controle de acesso ao banco PostgreSQL além do usuário único | MÉDIA | ⚠️ Requer usuário DB com privilégios mínimos |

---

## 3. Controles já implementados ✅

### Autenticação
- SHA-256 do token completo armazenado (nunca o token em si) — `auth/middleware/auth.go:60`
- Comparação em tempo constante: `subtle.ConstantTimeCompare` — `auth.go:76`
- Prefixo `gwk_` obrigatório + lookup O(1) por prefixo (falha rápida sem revelar razão)
- Token nunca logado — somente `key_prefix` aparece em logs (`CLAUDE.md §1.4`)
- Toda falha de auth gera `audit.EventAuthFailed` com `reason` e `request_id`

### Integridade de dados
- Todas as queries SQL parametrizadas com pgx (`$1, $2, ...`) — sem concatenação de string
- Sem ORM — SQL explícito e auditável
- `MaxBytesReader(1 MiB)` no body de cada request — `chat.go:65`
- `MaxHeaderBytes: 1 MiB` na configuração do servidor HTTP

### Proteção de PII/PCI
- Masking antes do envio ao provider: CPF (mod-11), CNPJ, cartão+Luhn, e-mail, telefone, CEP
- `raw_prompt_logging: false` por padrão — validado no boot
- Prompt nunca logado (conteúdo de `messages[].content`)

### Resiliência
- Recover middleware captura todos os panics → 500 genérico (sem stack trace ao cliente)
- Graceful shutdown: drena canais de audit/usage antes de fechar
- Per-request context timeout para chamadas ao provider (504 vs 502)
- Contexto `ctx.Done()` checado no loop SSE para detectar disconnect do cliente

### Container
- Build multi-stage: binário estático (sem shell, sem libc) em `alpine:3.21`
- Processo roda como usuário não-root `app` — `Dockerfile:8-9`
- Sem secrets hardcoded — tudo via env var

---

## 4. Gaps e ações para produção

### ⚠️ S1 — Força bruta em bearer tokens (ALTA)

**Ameaça:** Um atacante com acesso à rede interna pode tentar milhões de combinações em `gwk_<prefix>_<secret>`. O gateway não tem lockout por IP nem throttle na rota de auth failure.

**Por que não é crítico na prática:**
- Auth path é apenas `SHA-256 + map lookup` — não há query ao banco na falha
- O atacante precisa conhecer um `key_prefix` válido (YAML não é público)
- O secret tem entropia ≥ 48 bytes hex (192 bits) — inviável por força bruta direta

**Ação obrigatória antes de go-live:**
```nginx
# nginx.conf — rate limit por IP na camada de edge
limit_req_zone $binary_remote_addr zone=gateway_api:10m rate=30r/s;

server {
    location /v1/ {
        limit_req zone=gateway_api burst=50 nodelay;
        proxy_pass http://gateway:8080;
    }
}
```
Isso limita a 30 req/s por IP antes de chegar ao gateway, tornando brute force inviável.

---

### ⚠️ T1 — Permissões do gateway.yaml (MÉDIA)

**Ameaça:** Se o arquivo `configs/gateway.yaml` for legível por outros usuários no host, os `key_hash` das aplicações ficam expostos. Com o hash + o prefixo, um atacante pode tentar um ataque de dicionário offline.

**Ação:**
```bash
# No host, após deploy
chmod 640 /caminho/para/configs/gateway.yaml
chown root:docker /caminho/para/configs/gateway.yaml
```

No Docker Compose, use bind mount com permissões restritas:
```yaml
volumes:
  - type: bind
    source: ./configs/gateway.yaml
    target: /app/configs/gateway.yaml
    read_only: true
```

---

### ⚠️ T3 — TLS entre apps consumidoras e o gateway (MÉDIA)

**Ameaça:** O gateway não faz TLS (por design — ADR-0008 e SPEC §14.5). Se as apps consumidoras chamam `http://gateway:8080`, o Bearer token trafega em claro na rede interna.

**Ação:** Obrigatório colocar nginx/HAProxy com TLS na frente:
```
App consumidora → HTTPS → nginx (TLS termina) → HTTP → gateway:8080
```
Ver seção 6 deste documento para configuração completa.

---

### ⚠️ D2 — SSE hold-open sem limite de conexões (MÉDIA)

**Ameaça:** `WriteTimeout: 0` é necessário para SSE longo (ADR-0008). Um atacante com token válido pode abrir muitas conexões SSE simultâneas e segurar, esgotando file descriptors e goroutines.

**Ação:**
```nginx
# nginx.conf — limite de conexões simultâneas por IP
limit_conn_zone $binary_remote_addr zone=gateway_conn:10m;

location /v1/chat/completions {
    limit_conn gateway_conn 10;       # máx 10 SSE simultâneos por IP
    proxy_read_timeout 120s;          # timeout total de 2 minutos no nginx
    proxy_pass http://gateway:8080;
}
```

---

### ⚠️ D1 — Rate limit em endpoints públicos (MÉDIA)

**Ameaça:** `/healthz` e `/readyz` são públicos e sem rate limit. `/readyz` faz um HEAD ao Azure a cada chamada.

**Ação:** No nginx, restringir `/readyz` à rede de monitoramento:
```nginx
location /readyz {
    allow 10.0.0.0/8;    # apenas rede interna / monitoramento
    deny all;
    proxy_pass http://gateway:8080;
}
```

---

### ⚠️ E2 — Usuário do banco com privilégios mínimos (MÉDIA)

**Ameaça:** Se `DATABASE_URL` usa um superusuário PostgreSQL e o gateway for comprometido, o atacante tem acesso irrestrito ao banco.

**Ação:** Criar usuário com apenas os privilégios necessários:
```sql
-- Criar usuário de aplicação
CREATE USER gateway_app WITH PASSWORD 'senha-forte-aqui';

-- Grants mínimos
GRANT CONNECT ON DATABASE gateway TO gateway_app;
GRANT USAGE ON SCHEMA public TO gateway_app;
GRANT SELECT, INSERT, UPDATE ON usage_events, audit_events, budget_counters TO gateway_app;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO gateway_app;

-- Negar privilégios administrativos explicitamente
REVOKE CREATE ON SCHEMA public FROM gateway_app;
```

Atualizar `DATABASE_URL` para usar `gateway_app`.

---

## 5. Sobre o uso de Tier 1 sem detecção de injeção (E1)

Por design (SPEC §5.3), Tier 1 **não detecta prompt injection**. Apenas mascara CPF e cartão. Isso significa que uma aplicação Tier 1 pode (inadvertidamente) repassar conteúdo de injeção ao modelo.

**Postura correta:** Tier 1 é para aplicações com conteúdo controlado e de baixo risco (ex: chatbot interno com prompts estruturados). Para qualquer aplicação que aceita input livre de usuários finais, use Tier 2 no mínimo.

**Documentar para as aplicações consumidoras:** se AppBasico aceitar input do usuário final sem sanitização prévia, deve ser migrada para Tier 2.

---

## 6. Configuração nginx de referência (OnPrem)

```nginx
# /etc/nginx/conf.d/ai-gateway.conf

limit_req_zone $binary_remote_addr zone=gateway_api:10m rate=30r/s;
limit_conn_zone $binary_remote_addr zone=gateway_conn:10m;

upstream gateway_backend {
    server gateway:8080;    # nome do serviço no Docker Compose
    keepalive 32;
}

server {
    listen 443 ssl http2;
    server_name gateway.interno.teleperformance.com;

    ssl_certificate     /etc/ssl/certs/gateway.crt;
    ssl_certificate_key /etc/ssl/private/gateway.key;
    ssl_protocols       TLSv1.2 TLSv1.3;
    ssl_ciphers         ECDHE-ECDSA-AES128-GCM-SHA256:ECDHE-RSA-AES128-GCM-SHA256;

    # Endpoints da API
    location /v1/ {
        limit_req  zone=gateway_api burst=50 nodelay;
        limit_conn gateway_conn 20;

        proxy_pass         http://gateway_backend;
        proxy_http_version 1.1;
        proxy_set_header   Connection "";            # keepalive
        proxy_set_header   Host $host;
        proxy_set_header   X-Real-IP $remote_addr;
        proxy_set_header   X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header   X-Forwarded-Proto $scheme;

        # SSE streaming
        proxy_buffering    off;
        proxy_cache        off;
        proxy_read_timeout 120s;    # timeout máximo por request (streaming)
    }

    # Liveness — liberado para load balancer
    location /healthz {
        proxy_pass http://gateway_backend;
        access_log off;
    }

    # Readiness — restrito à rede de monitoramento
    location /readyz {
        allow 10.0.0.0/8;
        deny  all;
        proxy_pass http://gateway_backend;
        access_log off;
    }

    # Bloquear qualquer outra rota
    location / {
        return 404;
    }
}

# Redirecionar HTTP → HTTPS
server {
    listen 80;
    server_name gateway.interno.teleperformance.com;
    return 301 https://$host$request_uri;
}
```

---

## 7. Gestão de secrets no OnPrem (sem Azure Key Vault)

Sem Key Vault, a opção mais segura para OnPrem é Docker Secrets ou arquivo `.env` com permissão restrita:

### Opção A — Arquivo `.env` restrito (mais simples)
```bash
# Criar arquivo com permissão 600 (somente o dono lê)
chmod 600 /opt/ai-gateway/.env
chown gateway_service_user /opt/ai-gateway/.env

# Conteúdo
AZURE_OPENAI_API_KEY=chave-real-aqui
DATABASE_URL=postgres://gateway_app:senha@postgres:5432/gateway?sslmode=require
```

No `docker-compose.yml` de produção:
```yaml
env_file:
  - /opt/ai-gateway/.env
```

### Opção B — Docker Secrets (mais seguro, mas requer Docker Swarm ou orquestrador)
```yaml
secrets:
  azure_api_key:
    external: true
services:
  gateway:
    secrets:
      - azure_api_key
```

### O que NUNCA fazer
- Commitar `.env` com valores reais no Git ✅ (já está no `.gitignore`)
- Passar segredos como variáveis de ambiente em linha de comando (`docker run -e KEY=valor`) — fica visível em `ps aux`
- Usar o usuário `gateway` (mesmo do banco) com senha no YAML

---

## 8. Checklist de segurança para go-live OnPrem

### Obrigatório antes de receber tráfego real

- [ ] Nginx com TLS configurado na frente do gateway (seção 6)
- [ ] Rate limiting no nginx: 30 req/s por IP em `/v1/`
- [ ] Porta 8080 do gateway **NÃO** exposta fora da rede Docker (`ports:` removido ou restrito a `127.0.0.1:8080`)
- [ ] Arquivo `.env` com permissão 600, não commitado
- [ ] `configs/gateway.yaml` com permissão 640, bind mount read-only
- [ ] Usuário PostgreSQL com grants mínimos (seção 4, E2)
- [ ] `DATABASE_URL` com `sslmode=require` e usuário de baixo privilégio
- [ ] `raw_prompt_logging: false` no YAML (já é o padrão)
- [ ] Tokens de produção gerados com `openssl rand -hex 24` (não os tokens de dev)
- [ ] Chaves Azure com role **Cognitive Services User** apenas (não Contributor)

### Recomendado (não bloqueante para go-live)

- [ ] `/readyz` restrito à rede de monitoramento
- [ ] Logs enviados para SIEM (ELK, Splunk, Azure Monitor)
- [ ] Alerta configurado para `event_type=panic_recovered` e `level=ERROR`
- [ ] Rotação semestral de tokens de aplicações
- [ ] Revisão mensal de consumo por app (`budget_counters`)

### Fase 2 (melhoria planejada)

- [ ] Integridade criptográfica do audit log (hash encadeado ou Azure Event Hub)
- [ ] Redis rate limit (multi-instância)
- [ ] DB-backed policies (sem restart para onboarding)

---

## 9. Resumo executivo

O gateway está em nível adequado para produção OnPrem com os controles já implementados, desde que as ações obrigatórias da seção 4 sejam aplicadas antes do go-live. Os pontos mais críticos são:

1. **TLS obrigatório no nginx** — sem isso, Bearer tokens trafegam em claro
2. **Rate limit no nginx** — sem isso, brute force de tokens é viável da rede interna
3. **Permissões de arquivo** — `gateway.yaml` e `.env` com 640/600
4. **Usuário PostgreSQL com grants mínimos** — reduz impacto de eventual comprometimento

Nenhum dos gaps identificados é uma vulnerabilidade explorável sem acesso físico ou lógico à rede interna da infraestrutura. Para um deploy OnPrem atrás de firewall com segmentação de rede adequada, o risco residual é baixo.
