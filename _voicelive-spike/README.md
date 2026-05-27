# voicelive-spike

Cliente Go isolado que conecta direto no **Azure Voice Live API** para medir
baseline de latência antes da Onda 8 (proxy de áudio bidirecional via gateway).

**Diretório começa com `_`** → ferramentas Go (`go build ./...` no projeto
principal, `go test ./...`, etc.) **ignoram** este folder. Só `go build` /
`go run` rodados de dentro de `_voicelive-spike/` veem este código.

## Pré-requisitos

| Item | Como verificar |
|---|---|
| Go 1.25+ | `go version` |
| Compilador C (CGO requer) | `gcc --version` (no Windows: TDM-GCC, MSYS2 ou o gcc que vem com Git for Windows costuma servir) |
| Permissão de microfone do Windows 11 | Primeira execução vai disparar o prompt. Configurações → Privacidade → Microfone → permitir Apps Desktop |
| Acesso de rede ao resource Azure | Sem VPN — Voice Live é endpoint público |
| `az` CLI autenticada no tenant correto (se for puxar a key do KV) | `az account show` |

`malgo` (mini-audio bindings) usa CGO para a captura/playback. Se o build
falhar com `"gcc": executable file not found`, instalar MSYS2 ou TDM-GCC e
garantir que `gcc` está no `PATH`.

## Configurar

```pwsh
cd _voicelive-spike

# Pega a chave do Key Vault e injeta direto na env (não persiste no shell history)
$env:VOICE_LIVE_KEY = az keyvault secret show --vault-name danieldev --name TpVoiceLiveApi --query value -o tsv

# Os outros podem vir de .env (copiando de .env.example) OU direto:
$env:VOICE_LIVE_ENDPOINT = "https://td-foundry-voiceflow-rsc-eus2.cognitiveservices.azure.com"
$env:VOICE_LIVE_MODEL = "gpt-4o-mini-realtime-preview"
$env:VOICE_LIVE_VOICE = "pt-BR-FranciscaNeural"
$env:VOICE_LIVE_INSTRUCTIONS = "Você é um atendente de call center educado e objetivo."
$env:VOICE_LIVE_LANGUAGE = "pt-BR"
```

## Rodar

```pwsh
go run .
```

(ou `go build -o spike.exe . && .\spike.exe` se preferir um binário).

## O que esperar no log

Ordem normal de eventos durante uma sessão saudável:

```
INFO connecting url=td-foundry-voiceflow-rsc-eus2.../voice-live/realtime model=gpt-4o-mini-realtime-preview
INFO WebSocket connected ms=234
INFO session updated ms=187
INFO session updated, opening mic session_updated_ms=187 ws_connect_ms=234
INFO 🎤 fale algo. Ctrl-C para sair.
INFO speech started (you)                    ← falou no mic
INFO speech stopped (you)                    ← parou de falar (VAD detectou)
INFO assistant transcript text="Olá, sou..."  ← transcript do que o modelo respondeu
INFO response audio complete
INFO turn summary first_audio_ms=687 response_duration_ms=2840 chunks=72 audio_seconds=2.88 avg_chunk_interval_ms=39
```

### Métricas que importam

| Métrica | O que mede | Target |
|---|---|---|
| `ws_connect_ms` | TLS handshake + WebSocket upgrade | < 500 ms |
| `session_updated_ms` | `session.update` → `session.updated` | < 300 ms |
| `first_audio_ms` (no `turn summary`) | Você parou de falar → primeiro chunk de áudio do agente. **Métrica crítica de UX.** | < 800 ms |
| `avg_chunk_interval_ms` | Espaço entre `response.audio.delta` consecutivos | 30–80 ms (corresponde aos chunks de PCM streaming) |

Pra fechar a Onda 8 com tranquilidade, o **first_audio_ms** precisa ser
consistentemente bom no Voice Live direto (sem gateway no meio). Se aqui já
estiver alto (> 1500 ms), nenhum gateway no caminho vai melhorar — o problema
é upstream.

## Troubleshooting

| Sintoma | Causa provável | Fix |
|---|---|---|
| `websocket dial (http 401)` | API key inválida ou expirada | Refresh do KV: `az keyvault secret show --vault-name danieldev --name TpVoiceLiveApi` |
| `websocket dial (http 404)` | Endpoint errado ou path inválido | Confirmar `VOICE_LIVE_ENDPOINT` aponta pro resource Foundry correto |
| `websocket dial: ... model not found` (em algum evento de erro) | Modelo no query param não existe no resource | Trocar `VOICE_LIVE_MODEL` pra `gpt-4o-mini` ou `gpt-realtime-mini` |
| `timed out waiting for session.updated within 5s` | Server não respondeu — provavelmente erro silencioso | Habilitar `slog.LevelDebug` em `main.go` pra ver eventos crus |
| Áudio sai cortado / glitch | Buffer da `playbackBuf` se acumula vs drena com diferença grande | Ver `audio.go` `playbackBuf` — pode precisar ajustar tamanho da ring buffer |
| Áudio capturado é só silêncio | Microfone errado selecionado pelo malgo (pega o default do SO) | No Windows: Painel de Controle → Som → Gravação → definir o mic correto como Default |
| Build falha com `"gcc": executable file not found` | GCC não no PATH | Instalar MSYS2 ou TDM-GCC; garantir `gcc.exe` em `%PATH%` |

## Encerrar

`Ctrl-C` no terminal. O spike fecha o WebSocket gracefully, para os devices
de áudio do malgo, e sai. Sem leaks.

## Próximo passo (depois deste spike funcionar)

Com latency baseline na mão, **ADR-0023** vai decidir como o gateway proxy
trata WebSocket bidirecional. Esse spike é jogado fora **OU** vira ponto de
referência. O `internal/proxy/audio/` futuro vai herdar o schema de eventos
(`events.go`) e a lib `coder/websocket`.
