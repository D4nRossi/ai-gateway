# infra/legacy-windows — artefatos do deploy Windows (deprecado)

> ⚠️ **Deprecado em 2026-06-02.** Deploy Windows (IIS + WinSW + DPAPI) foi
> suspenso quando o alvo passou a ser Linux + Docker + nginx (ADR-0030).
> Estes arquivos ficam aqui como **referência histórica** — não devem
> ser usados pra novos deploys e provavelmente têm paths/imports obsoletos
> em relação ao monorepo `apps/`.

## Conteúdo

| Arquivo | O que era | Status atual |
|---|---|---|
| `build-windows-deploy.sh` | Cross-compila `gateway.exe` + utilities + frontend; empacota `dist/` num zip pra transporte pro servidor Windows | **Quebrado** — assume layout antigo (`cmd/`, `internal/`, `web/` na raiz). Não funciona com `apps/gateway/` |
| `gateway-service.xml` | Template do WinSW (Windows Service Wrapper) com `<serviceaccount>`, env vars de runtime, log roll-by-size | **Não é mais usado** — Linux+Docker usa `docker compose` |
| `gateway.yaml.deploy` | Template do `configs/gateway.yaml` específico pro Windows (paths `E:\AIGateway\...`, providers comentados) | **Superseded** pelo `apps/gateway/configs/gateway.yaml` + override via env vars do Compose |

## Por que ficam preservados (e não deletados)

- Servem de referência caso o caminho Windows volte a ser ativo (ADR-0026
  DPAPI+AE descreve o flow completo de secrets)
- A sessão `KB/AI-Gateway/Deploy/Deploy-Windows-Sessao-2026-05-28.md` no
  vault Obsidian referencia esses arquivos como "pacote pronto"
- Custo de manter ≈ zero; benefício de auditoria histórica > 0

## Não restaurar sem revisar

Se um dia o deploy Windows for retomado:

1. Rever ADR-0026 (DPAPI+AE) — ainda válida em conceito
2. Adaptar `build-windows-deploy.sh` pro novo layout `apps/gateway/`
3. Reabrir `docs/deploy/windows.md` (mantida como referência também)
4. Possivelmente reverter parte da ADR-0030 ou abrir ADR nova que reabilita
   o caminho Windows em paralelo ao Linux

## Referências

- ADR-0026 — Secrets Windows DPAPI + Always Encrypted
- ADR-0030 — Monorepo `apps/` + decisão de deploy alvo Linux
- `docs/deploy/windows.md` — manual operacional Windows (referência)
- Memória `deploy_windows_suspended.md` no diretório de memórias do Claude
