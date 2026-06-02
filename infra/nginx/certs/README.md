# infra/nginx/certs

> Pasta de certificados TLS pro nginx terminator. **Conteudo real fica em
> `.gitignore`** — esta pasta no repo so contem instrucoes.

## Estrutura esperada

```
infra/nginx/certs/
├── README.md       (versionado)
├── .gitignore      (versionado)
├── tls.crt         (NAO versionado — server cert da CA corp)
├── tls.key         (NAO versionado — private key)
└── ca.crt          (NAO versionado — chain da CA corp)
```

## Como popular no servidor

Em rede corporativa Teleperformance, o cert TLS eh emitido pela CA interna.
Fluxo tipico:

1. **Gerar CSR no servidor** (one-time):
   ```bash
   sudo openssl req -new -newkey rsa:4096 -nodes \
       -keyout /etc/api-gateway/certs/tls.key \
       -out /etc/api-gateway/certs/api-gateway.csr \
       -subj "/CN=api-gateway.tpb.corp/O=Teleperformance Brasil/C=BR" \
       -addext "subjectAltName=DNS:api-gateway.tpb.corp,DNS:api-gateway-hom.tpb.corp"
   sudo chmod 600 /etc/api-gateway/certs/tls.key
   ```

2. **Submeter CSR pra time de PKI corp** — eles devolvem `tls.crt` (+ cadeia
   intermediaria `ca.crt`).

3. **Copiar certificados pro host**:
   ```bash
   sudo cp tls.crt /etc/api-gateway/certs/
   sudo cp ca.crt  /etc/api-gateway/certs/
   sudo chown root:root /etc/api-gateway/certs/*
   sudo chmod 644 /etc/api-gateway/certs/tls.crt /etc/api-gateway/certs/ca.crt
   sudo chmod 600 /etc/api-gateway/certs/tls.key
   ```

4. **Bind-mount no Compose** — `infra/docker/docker-compose.yml` mapeia
   `/etc/api-gateway/certs:/etc/nginx/certs:ro` no servico `nginx`.

## Renovacao

Certs corp tem validade 1 ano. Calendarize renovacao 30 dias antes:

```bash
# Conferir expiracao
openssl x509 -enddate -noout -in /etc/api-gateway/certs/tls.crt
```

Renovacao = repetir passos 1-3 com novo CSR. Reload sem downtime:

```bash
docker compose -f infra/docker/docker-compose.yml exec nginx nginx -t \
    && docker compose -f infra/docker/docker-compose.yml exec nginx nginx -s reload
```

## Self-signed pra dev/staging

Apenas pra ambiente isolado de teste:

```bash
mkdir -p infra/nginx/certs
openssl req -x509 -newkey rsa:2048 -nodes -days 365 \
    -keyout infra/nginx/certs/tls.key \
    -out    infra/nginx/certs/tls.crt \
    -subj "/CN=localhost"
cp infra/nginx/certs/tls.crt infra/nginx/certs/ca.crt
```

⚠️ Browser vai mostrar warning. Nao usar em prod.
