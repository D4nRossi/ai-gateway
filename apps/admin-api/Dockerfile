# syntax=docker/dockerfile:1.7
#
# Multi-stage build da admin-api .NET 10 (ADR-0027).
#
#   1. mcr.microsoft.com/dotnet/sdk:10.0    → restore + publish
#   2. mcr.microsoft.com/dotnet/aspnet:10.0 → runtime mínimo
#
# Entrypoint default sobe o Kestrel (Minimal API). Pra rodar migrations
# (modo CLI da ADR-0025):
#
#   docker run --rm \
#     -e ConnectionStrings__Gateway="Server=...;..." \
#     api-gateway-admin:latest \
#     migrate up
#
# References:
#   - CLAUDE.md §4.5 — imagens pinadas pra Linux
#   - ADR-0025 — MIGRATIONS_AUTO_APPLY=false em prod
#   - ADR-0027 — admin plane .NET

# ── Stage 1: build ───────────────────────────────────────────────────────────
FROM mcr.microsoft.com/dotnet/sdk:10.0 AS builder
WORKDIR /src

# Cache de NuGet por csproj antes do source code completo.
COPY APIGateway.Admin.sln Directory.Build.props ./
COPY src/APIGateway.Admin.Api/APIGateway.Admin.Api.csproj src/APIGateway.Admin.Api/
RUN dotnet restore APIGateway.Admin.sln

COPY src/ src/
RUN dotnet publish src/APIGateway.Admin.Api/APIGateway.Admin.Api.csproj \
        -c Release \
        -o /out \
        --no-restore \
        -p:UseAppHost=false

# ── Stage 2: runtime ─────────────────────────────────────────────────────────
FROM mcr.microsoft.com/dotnet/aspnet:10.0

# tini não é necessário porque o entrypoint é o próprio dotnet (PID 1 sane).
# tzdata + ca-certificates já vem no aspnet base image.

WORKDIR /app
COPY --from=builder /out ./

# Usuário não-root (CLAUDE.md §4.5 / boas práticas de runtime)
RUN adduser --disabled-password --no-create-home --uid 10001 app && \
    chown -R app:app /app
USER app

EXPOSE 8080
ENV ASPNETCORE_URLS=http://+:8080 \
    DOTNET_RUNNING_IN_CONTAINER=true \
    DOTNET_NOLOGO=true

HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
    CMD wget -q -O /dev/null http://localhost:8080/healthz || exit 1

ENTRYPOINT ["dotnet", "APIGateway.Admin.Api.dll"]
