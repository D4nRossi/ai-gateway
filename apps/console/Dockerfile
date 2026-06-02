# syntax=docker/dockerfile:1.7
#
# Multi-stage build do console SPA — ADR-0028.
#
#   1. node:20-alpine    → compila o SPA via Vite, gera dist/
#   2. nginx:1.27-alpine → serve dist/ estático com fallback SPA pra index.html
#
# O frontend usa paths relativos (api.ts faz fetch direto pra /admin/v1/...),
# então CORS é não-issue: o nginx terminator externo serve console em /ui/* e
# encaminha /admin/v1/* + /v1/* pros outros containers (mesma origin no browser).
#
# References:
#   - CLAUDE.md §4.5 — pinned images
#   - ADR-0028 — frontend extraído do binário (supersedes ADR-0014)
#   - ADR-0030 — monorepo apps/

# ── Stage 1: build do SPA ────────────────────────────────────────────────────
FROM node:20-alpine AS builder
WORKDIR /app

# Cacheia layer de deps por mudança de manifest.
COPY package.json package-lock.json ./
RUN npm ci --no-audit --no-fund

COPY . .
RUN npm run build

# ── Stage 2: runtime ─────────────────────────────────────────────────────────
FROM nginx:1.27-alpine

# vite.config.ts define base="/ui/", então o bundle gera URLs /ui/assets/*.
# O nginx interno deste container serve qualquer path → dist/. O terminator
# externo (infra/nginx/) é quem despacha /ui/ pra cá; rota raiz (/) volta ao
# index.html via try_files pra deixar o React Router resolver navegações de
# refresh.
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=builder /app/dist /usr/share/nginx/html

EXPOSE 80

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -q -O /dev/null http://localhost/ || exit 1
