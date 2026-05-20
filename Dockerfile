# Multi-stage build: compile in golang:1.25-alpine, run in alpine:3.21.
# Note: go 1.25 is required by pgx v5.9.x (the current stable pgx release).
# References: SPEC.md §14.6 — container security
# CLAUDE.md §4.4 — pinned image versions (updated from 1.24 due to pgx requirement)

# ── Stage 1: build ───────────────────────────────────────────────────────────
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Copy module files first for layer-cached dependency download.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO_ENABLED=0 produces a fully static binary; no libc needed in the runtime image.
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/bin/ai-gateway ./cmd/gateway

# ── Stage 2: runtime ─────────────────────────────────────────────────────────
FROM alpine:3.21

# Run as non-root user per SPEC §14.6.
RUN addgroup -S app && adduser -S app -G app

WORKDIR /app

COPY --from=builder /app/bin/ai-gateway .
COPY configs/ configs/
COPY migrations/ migrations/

USER app

EXPOSE 8080

ENTRYPOINT ["./ai-gateway"]
