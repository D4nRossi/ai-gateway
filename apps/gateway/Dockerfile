# syntax=docker/dockerfile:1.7
#
# Multi-stage build (data plane Go puro, sem SPA — ADR-0028):
#   1. golang:1.25-alpine → compila o binário Go (sem go:embed do frontend)
#   2. alpine:3.21        → runtime mínimo, non-root, com ca-certificates
#
# O console React+Vite vive em apps/console/ e tem o próprio Dockerfile
# (nginx servindo dist/). O nginx terminator roteia /ui/* pro container do
# console e /admin/v1/*, /v1/* pra este binário.
#
# References:
#   - SPEC.md §14.6 — container security
#   - CLAUDE.md §4.4 — pinned image versions
#   - ADR-0028 — frontend extraído do binário (supersedes ADR-0014)

# ── Stage 1: build do Go ─────────────────────────────────────────────────────
FROM golang:1.25-alpine AS builder
WORKDIR /app

# Cacheia layer de deps pelo go.mod.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO_ENABLED=0 produz um binário totalmente estático — sem libc no runtime.
# -ldflags="-s -w" remove tabela de símbolos e debug info, reduzindo ~30%.
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w" \
    -o /app/bin/ai-gateway \
    ./cmd/gateway

# ── Stage 2: runtime ─────────────────────────────────────────────────────────
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
