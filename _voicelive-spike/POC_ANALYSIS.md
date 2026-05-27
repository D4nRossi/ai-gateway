# Análise — Spike Voice Live vs POC TPCore.Modules.AgentFlow

Documento produzido em **2026-05-27** após análise da POC interna em
`E:\Teleperformance CRM SA\Arquitetura\Fontes\tpcoredesacoplacao\Modules\TPCore.Modules.AgentFlow`
solicitada pelo owner depois que a voz no spike soou "robótica" mesmo com
HD voice e temperature.

## TL;DR

| Pergunta | Resposta |
|---|---|
| A voz "robótica" do spike é bug de config? | **Não.** É **inerente ao modo Pure Voice Live** com TTS Azure standard. A POC tem o mesmo problema no modo Pure (`PureVoiceLiveSessionRuntime`). |
| Como a POC entrega voz natural então? | **Modo Hybrid** (`StreamingSessionRuntime`): Voice Live faz só **VAD + STT** → LLM próprio (Azure OpenAI) → **ElevenLabs TTS** entrega o áudio final. |
| ElevenLabs é o segredo do som natural? | Em larga medida sim. ElevenLabs é provedor especializado de TTS neural com fama de voz quase indistinguível de humano. Cartesia é alternativa similar. |
| Implementar o pipeline completo no gateway tem qual escala? | **2-3 meses** se replicar a sofisticação da POC (fillers semânticos, barge-in, containment, yield mode). MVP Pure = **~2 semanas**. |
| Existe caminho intermediário? | Sim — gateway só proxia o WebSocket (auth/audit/rate-limit) e deixa pipeline com a aplicação cliente. Cliente HOJE é a POC AgentFlow ou um app novo. Gateway não duplica orquestração. **Recomendado.** |

## A arquitetura da POC

`VoiceLiveRuntimeMode.cs` define 3 modos de execução baseados na config do
agente (campos `IaRuntime.Modelo` e `Voz.ProvedorTts`):

### Modo 1 — Pure Voice Live (`PureVoiceLiveSessionRuntime`)

- Voice Live faz **tudo**: VAD + STT + LLM + TTS Azure
- Modelo: `gpt-realtime` (PRO native realtime)
- Voz: `pt-BR-FranciscaNeural` (Azure standard)
- **Equivalente ao nosso spike atual**
- Trigger: `IsPureRealtimeModel(config)` && `UsesAzureTts(agente)`
- **Voz: robótica** (limitação Azure standard via Voice Live)

### Modo 2 — Hybrid (`StreamingSessionRuntime` — modo default)

- Voice Live faz **só VAD + STT**, depois desliga
- Orquestrador chama LLM próprio (Azure OpenAI `gpt-4.1`, etc.)
- Texto vai pra **ElevenLabs** (ou Cartesia) em streaming → áudio sintetizado
- Voz final é entregue ao cliente via mesmo WebSocket
- Inclui: fillers semânticos pré-cacheados, barge-in handling, containment/yield modes, chunk boundaries com soft/hard limits, persistência de histórico, memória de runtime
- Trigger: default quando `UsesGptModel(agente)` E não é pure
- **Voz: natural** (ElevenLabs entrega)

### Modo 3 — Provider Pipeline (genérico)

- Agente configura provedores externos (STT, LLM, TTS) arbitrariamente
- Trigger: `ShouldUseProviderPipeline(agente)` — quando modelo NÃO é GPT
- Pra modelos não-GPT (Anthropic, Phi, etc.)

## Configuração da POC (ref `appsettings.json`)

```json
"VoiceLive": {
  "Endpoint": "https://td-foundry-voiceflow-rsc-eus2.cognitiveservices.azure.com",
  "ModelHibrido": "gpt-4o-mini",                     // pro modo Hybrid (só usado se Pure desativado)
  "ModelVoicePuro": "gpt-realtime",                  // PRO native realtime
  "ModelVoicePuro2": "gpt-realtime-mini-2025-12-15", // BASIC
  "VoiceName": "pt-BR-FranciscaNeural",
  "InputSampleRate": 16000,
  "TurnDetection": {
    "Threshold": 0.5,
    "PrefixPaddingMs": 200,
    "SilenceDurationMs": 375
  }
},
"ElevenLabs": {
  "ApiKey": "...",
  "VoiceId": "S9OExsnatEsUeafEWzPS"  // ID específico da voz contratada
},
"Cartesia": { "ApiKey": "..." }
```

Mesmo endpoint e mesma key que estamos usando no spike. Configs de VAD são as
que já alinhamos no spike (375ms silence, 200ms prefix, threshold 0.5).

## O que diverge entre spike e POC

| Aspecto | Spike (atual) | POC modo Pure | POC modo Hybrid |
|---|---|---|---|
| Modelo Voice Live | `gpt-realtime-mini` | `gpt-realtime` (PRO) | `gpt-4o-mini` (só pra STT) |
| Voz final entregue ao cliente | Azure standard (Francisca/Thalita HD) | Azure standard (Francisca) | **ElevenLabs** (voz S9OExsnat...) |
| Quem faz TTS | Voice Live | Voice Live | **ElevenLabs** |
| Quem chama LLM | Voice Live | Voice Live | Azure OpenAI direto |
| Frame de áudio do cliente | binary | binary (`buffer 4096`) | binary |
| Latência percebida típica | 404ms (medida) | desconhecida (similar?) | 350-800ms (com filler 100-300ms) |
| Latência real típica | 404ms (medida) | desconhecida | 1260-2080ms (após otimização) |
| Recursos avançados | nenhum | nenhum | fillers semânticos, barge-in, containment, memória |
| Naturalidade da voz | "robótica" | "robótica" | "natural" |

## A documentação de latência da POC

O arquivo `LATENCIA_REALTIME_STREAM.md` da POC mapeia 24 pontos de latência
no fluxo híbrido. Resumo dos pontos críticos:

| Bloco | Caso comum |
|---|---|
| VAD/fim de fala | 375ms |
| Transcript final Voice Live | 100-500ms |
| LLM gpt-4.1 (primeiro token) | 200-900ms |
| Agrupamento do primeiro chunk | 100-700ms |
| Request ElevenLabs (primeiro byte) | 150-700ms |
| **Buffer inicial ElevenLabs (5 frames × 100ms)** | **500ms — maior ganho identificado pelos otimizadores** |
| Envio WebSocket | 5-30ms |
| **Total real** | **1365-3375ms** |
| **Total percebido (com filler)** | **350-1000ms** |

Eles consideram **viável <1s percebido** mas não real consistente. ADR-0023
do nosso gateway pode/deve adotar a mesma estratégia (filler pré-cacheado +
streaming agressivo) se for replicar o modo Hybrid.

## Spike confirma latência sub-segundo no MODO PURE

Os números do spike (sessão de 2026-05-27 16:08):

| Métrica | Valor medido | Target |
|---|---|---|
| `ws_connect_ms` | 612ms | <800ms (one-time) |
| `session_updated_ms` | 155ms | <300ms |
| `first_audio_ms` média (8 turnos) | **404ms** | <800ms |
| `first_audio_ms` p95 | **571ms** | <1000ms |
| `avg_chunk_interval_ms` | 54-85ms | 30-100ms |

**Voice Live entrega sub-segundo consistente em PT-BR realtime modo Pure.**
A latência não é o problema — qualidade da voz é.

## Bug do `message too big` (resolvido em 2026-05-27)

`coder/websocket` Go tem `ReadLimit` default de 32 KiB. Frames de
`response.audio.delta` com chunks grandes (ou transcripts bundlded)
estouram. Fix: `conn.SetReadLimit(4 << 20)` (4 MiB). Aplicado em `main.go`
após o erro `failed to read: websocket: message too big`.

## Caminhos pro gateway (Onda 8)

Três opções, ordenadas por escopo:

### Opção A — Gateway proxy genérico WebSocket (MVP, ~2 semanas)

**Filosofia:** gateway segue sendo "proxy genérico" como pra Azure OpenAI,
Anthropic etc. Não duplica lógica de aplicação.

- Cliente se autentica no gateway no upgrade WebSocket (bearer)
- Gateway abre WS upstream com Voice Live (ou outro provider) passando
  payload binário intacto
- Audita: nova tabela `gogateway.audio_sessions` (started_at, ended_at,
  application_name, model, voice, audio_seconds_in/out, disconnect_reason,
  estimated_cost_brl)
- Política de tier: novo campo `allowed_realtime_models` por aplicação
- Sem orquestração de pipeline (LLM, TTS, fillers) — isso é responsabilidade
  do cliente (POC AgentFlow ou outros)
- Frontend de áudio: trabalho separado, em outro repo/projeto

**Escopo:** ~2 semanas de Go (proxy stateful + goroutines + schema + auth).

### Opção B — Adapter do gateway pra POC AgentFlow

- Gateway proxia o WebSocket pra POC AgentFlow existente (`/ws/realtime-stream`)
- Gateway adiciona auth/audit/rate-limit no upgrade
- POC continua orquestrando pipeline (LLM + ElevenLabs + fillers)
- Cliente fala com `wss://gateway/v1/audio/realtime` em vez de Voice Live direto
- **Não duplica nada do pipeline**

**Escopo:** ~1 semana (similar à Opção A mas com upstream sendo a POC).

**Trade-off:** gateway sempre depende da POC AgentFlow estar viva pra
fluxo de áudio funcionar. Pode ser bom (centralização) ou ruim (acopla
dois sistemas).

### Opção C — Replicar pipeline completo no gateway (~2-3 meses)

- Reescrever `StreamingSessionRuntime` em Go
- 6+ provider integrations (Voice Live STT, Azure OpenAI streaming, ElevenLabs,
  Cartesia, helpers compostos)
- Fillers semânticos pré-cacheados (CRUD de configuração)
- Barge-in, containment, yield modes
- Schema de agentes/versões/personalidade no DB
- Frontend WebRTC ou WebSocket client

**Escopo:** 2-3 meses + risco de bugs sutis durante portabilidade C#→Go.

## Recomendação técnica

**Opção A.** Razões:

1. **Filosofia do gateway preservada.** Gateway é proxy autenticado, não
   orquestrador. Outras providers (Azure OpenAI, Anthropic via proxy plane)
   seguem essa lógica.
2. **Escopo de Onda 8 fica honesto.** 2 semanas vs 2-3 meses é a diferença
   entre entregar e empacar.
3. **POC AgentFlow continua sendo a aplicação principal de áudio.** Gateway
   adiciona valor (auth/audit/multi-tenancy/rate-limit) sem competir.
4. **Frontend fica desacoplado.** Web app, mobile app ou a POC mesmo podem
   ser clientes do gateway. Cada um decide pipeline.

Opção B é alternativa viável se o time decide centralizar tudo no gateway
**e** a POC AgentFlow vira o "núcleo de áudio". Trade-off de acoplamento.

Opção C é "if I had a year". Não recomendo pra MVP.

## Próximos passos sugeridos

1. **(crítico, agora)** Rotacionar **todas** as chaves do `appsettings.json` da
   POC que vazaram nesse chat. Lista detalhada em meu alerta anterior.
2. **(decisão owner)** Escolher Opção A, B ou C pro escopo da Onda 8.
3. **(eu redijo)** ADR-0023 baseado na opção escolhida.
4. **(implementação)** Onda 8 vira execução de fato.

## Referências

- POC: `E:\Teleperformance CRM SA\Arquitetura\Fontes\tpcoredesacoplacao\Modules\TPCore.Modules.AgentFlow`
- LATENCIA_REALTIME_STREAM.md (mapa de 24 pontos de latência)
- VoiceLiveRuntimeMode.cs (decisão dos 3 modos)
- PureVoiceLiveSessionRuntime.cs (~1000 LOC, modo equivalente ao spike)
- StreamingSessionRuntime.cs (~3000 LOC, modo Hybrid completo)
- Spike: `_voicelive-spike/` (3 arquivos Go, ~500 LOC total)
- Doc Voice Live: https://learn.microsoft.com/en-us/azure/ai-services/speech-service/voice-live
- Doc HD voices: `pt-BR-Thalita:DragonHDLatestNeural`, `pt-BR-Macerio:DragonHDLatestNeural`
