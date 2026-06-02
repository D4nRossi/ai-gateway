# syntax=docker/dockerfile:1.7
#
# Multi-stage build:
#   1. node:20-alpine    → compila o SPA em web/dist
#   2. golang:1.25-alpine → compila o binário Go (embed dist)
#   3. alpine:3.21       → runtime mínimo, non-root, com ca-certificates
#
# Reasoning: incluir o build do frontend no Dockerfile elimina a dependência
# de "rodar pnpm/npm antes de docker build". O resultado é determinístico: um
# único `docker build` produz a imagem completa. Bonus: a primeira camada de
# cada stage é cacheada por package.json/go.mod, então rebuilds incrementais
# (só mudou código fonte) custam ~10s no total.
#
# References:
#   - SPEC.md §14.6 — container security
#   - CLAUDE.md §4.4 — pinned image versions
#   - ADR-0014 — frontend embedado no binário Go

# ── Stage 1: build do SPA ────────────────────────────────────────────────────
FROM node:20-alpine AS web-builder
WORKDIR /web

# Cacheia layer de deps pela mudança de manifest.
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund

COPY web/ ./
RUN npm run build

# ── Stage 2: build do Go ─────────────────────────────────────────────────────
FROM golang:1.25-alpine AS builder
WORKDIR /app

# Cacheia layer de deps pelo go.mod.
COPY go.mod go.sum ./
RUN go mod download

# Copia o resto do código, sobrescrevendo web/dist com o bundle do stage 1
# para que //go:embed all:dist puxe os assets reais.
COPY . .
COPY --from=web-builder /web/dist ./web/dist

# CGO_ENABLED=0 produz um binário totalmente estático — sem libc no runtime.
# -ldflags="-s -w" remove tabela de símbolos e debug info, reduzindo ~30%.
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w" \
    -o /app/bin/ai-gateway \
    ./cmd/gateway

# ── Stage 3: runtime ─────────────────────────────────────────────────────────
FROM alpine:3.21

# ca-certificates é obrigatório para fazer TLS handshake contra Azure OpenAI /
# Content Safety. Sem ele, todo upstream call falha com x509 unknown authority.
# tzdata permite timestamps locais corretos em logs estruturados.
RUN apk --no-cache add ca-certificates tzdata && \
    addgroup -S app && \
    adduser -S app -G app

WORKDIR /app

COPY --from=builder /app/bin/ai-gateway .
COPY configs/ configs/
COPY migrations/ migrations/

USER app
EXPOSE 8080

# Healthcheck nativo do Docker — o orchestrator usa pra restart automático.
# /healthz é uma rota pública (não consome DB).
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
    CMD wget -q -O /dev/null http://localhost:8080/healthz || exit 1

ENTRYPOINT ["./ai-gateway"]
