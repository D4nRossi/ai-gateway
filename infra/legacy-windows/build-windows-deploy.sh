#!/usr/bin/env bash
set -euo pipefail
cd /home/daniel/projects/ai-gateway

HASH=$(git rev-parse --short HEAD)
OUT=dist
echo "→ buildando versão $HASH"

# 1. Limpar dist anterior
rm -rf "$OUT" ai-gateway-deploy-*.zip
mkdir -p "$OUT/bin" "$OUT/configs" "$OUT/migrations"

# 2. Frontend embed
echo "→ frontend build"
( cd web && npm ci --silent && npm run build )

# 3. Cross-compile 3 binários pra Windows
export CGO_ENABLED=0 GOOS=windows GOARCH=amd64
echo "→ gateway.exe"
go build -trimpath -ldflags="-s -w -X main.Version=$HASH" \
    -o "$OUT/bin/gateway.exe" ./cmd/gateway

echo "→ secrets.exe"
go build -trimpath -ldflags="-s -w" -o "$OUT/bin/secrets.exe" ./cmd/secrets

echo "→ migrate-targets-to-kv.exe"
go build -trimpath -ldflags="-s -w" -o "$OUT/bin/migrate-targets-to-kv.exe" \
    ./cmd/migrate-targets-to-kv

# 4. WinSW v3 (baixa 1× só; pula se já tem cached)
if [ ! -f /tmp/winsw-v3-x64.exe ]; then
    echo "→ baixando WinSW"
    wget -q -O /tmp/winsw-v3-x64.exe \
        https://github.com/winsw/winsw/releases/download/v3.0.0-alpha.11/WinSW-x64.exe
fi
cp /tmp/winsw-v3-x64.exe "$OUT/bin/gateway-service.exe"

# 5. Configs (gateway-service.xml + gateway.yaml — você já editou esses
#    com o conteúdo das seções 2 e 3 desta nota antes de rodar o script)
[ -f gateway-service.xml ] || { echo "ERRO: cria gateway-service.xml na raiz"; exit 1; }
[ -f gateway.yaml.deploy ]  || { echo "ERRO: cria gateway.yaml.deploy na raiz"; exit 1; }
cp gateway-service.xml "$OUT/bin/gateway-service.xml"
cp gateway.yaml.deploy "$OUT/configs/gateway.yaml"

# 6. Migrations (lidas do FS pelo gateway no boot)
cp migrations/*.sql "$OUT/migrations/"

# 7. Hash de auditoria
( cd "$OUT" && find bin configs migrations -type f -exec sha256sum {} \; | sort > SHA256SUMS )

# 8. Zipar
ZIP=ai-gateway-deploy-$HASH.zip
( cd "$OUT" && zip -rq "../$ZIP" . )
echo
echo "✓ pacote pronto: $ZIP ($(du -h "$ZIP" | cut -f1))"
sha256sum "$ZIP"
