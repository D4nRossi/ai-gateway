# ADR-0023: Streaming bidirecional de áudio (pipeline híbrido)

- **Status**: proposed
- **Date**: 2026-05-27
- **Decision makers**: Daniel (owner)
- **Consulted**: Claude Opus 4.7
- **Supersedes**: nenhum
- **Sub-onda mapping**: 8.0 (este documento) → 8.1 → 8.2 → 8.3 → 8.4 → 8.5

## Context

Onda 8 do roadmap entrega capacidade de **conversação de voz em tempo real**
através do gateway, alvo de mercado-chave da Teleperformance Brasil (call
centers, atendimento automatizado, IVR moderno).

Existe uma POC interna funcional em `.NET` (`TPCore.Modules.AgentFlow`) que
serve como referência arquitetural. A POC opera em 3 modos (`Pure`, `Hybrid`,
`Provider Pipeline`) selecionados por config de agente. O modo Hybrid usa
**Voice Live (VAD+STT) → Azure OpenAI (LLM streaming) → ElevenLabs (TTS
streaming)** e entrega voz natural. O modo Pure usa só Voice Live (TTS Azure
standard) e entrega voz reconhecidamente sintética.

**Restrições e drivers da decisão:**

- **Latência percebida ≤ 1s** é meta de UX para call center (medida desde o
  silêncio do usuário até o primeiro áudio audível do agente).
- **Latência real** pode passar de 1s sem prejuízo se houver **filler verbal
  ágil** ("Certo, vou verificar...") mascarando o intervalo. A POC chama
  isso de "latência percebida".
- **Voz humanizada é requisito não-funcional**: owner declarou que a voz do
  modo Pure (Azure standard) "ainda está bizarra".
- **Spike** em `_voicelive-spike/` mediu latência sub-segundo (404ms média,
  571ms p95) no modo Pure — Voice Live entrega bem nessa camada.
- **POC media** 1365-3375ms reais no modo Hybrid hoje, com alvo
  1260-2080ms pós-otimização e <1s percebido com filler.
- **Gateway é proxy autenticado, multi-tenant, com audit**. Adicionar áudio
  deve preservar a filosofia: auth no upgrade, rate-limit por aplicação,
  audit de sessão/turn, observability de latência.
- **Custo de implementação** é variável crítica: replicar a POC inteira em
  Go = 2-3 meses focado. Não dá pra empacar; precisa ser
  decomposto e cada sub-entrega tem valor próprio.

A escolha que esta ADR endereça é: **o gateway absorve a orquestração
completa do pipeline híbrido (replicando a sofisticação da POC em Go) OU
delega pra aplicação cliente OU se restringe a proxy passthrough**.

## Decision

**Replicar o pipeline híbrido completo no gateway**, com dois modos
selecionáveis por aplicação:

- **`pure`**: Voice Live faz tudo (VAD + STT + LLM + TTS Azure). Voz Azure
  standard. Latência baixa (~400ms percebida). Apropriado para casos onde
  qualidade de voz é secundária ou onde Voice Live cobre necessidade.
- **`hybrid`** (default recomendado): Voice Live faz **só** VAD + STT;
  gateway orquestra LLM (Azure OpenAI streaming) + TTS (ElevenLabs
  streaming); fillers pré-cacheados mascarando latência.

O gateway abre WebSocket bidirecional **stateful** com o cliente, mantém
conexões upstream com Voice Live (e Azure OpenAI + ElevenLabs no modo
hybrid), audita cada sessão e cada turno em tabelas dedicadas
(`gogateway.audio_sessions` + `gogateway.audio_session_turns`).

A escolha de **TTS provider default** é **ElevenLabs** (espelha a POC), com
interface `TTSProvider` abstraindo o backend pra permitir troca futura
(Cartesia, Azure standard, custom). Decisão final de provider pode ser
revisitada via ADR-0024 dedicado durante sub-onda 8.2.

Pra absorver o custo de implementação, a Onda 8 é decomposta em **5
sub-ondas** (8.1–8.5) commitáveis independentemente — escopo na seção
"Sub-onda mapping" abaixo.

## Options considered

### Option 1: Pure proxy só

Gateway só passa frames binários pro Voice Live, sem orquestração de
pipeline. Aplicação cliente fica responsável por LLM + TTS se quiser modo
hybrid.

- **Pros:**
  - Escopo enxuto (~2 semanas de Go).
  - Preserva filosofia do gateway como proxy autenticado.
  - Cliente decide arquitetura interna sem amarração.
- **Cons:**
  - Voz robótica permanente (Voice Live com TTS Azure standard).
  - Sem fillers, sem barge-in inteligente, sem schema de fillers reutilizável
    multi-tenant.
  - Owner rejeitou explicitamente em 2026-05-27 ("voz ainda está bizarra").
  - Cada cliente que quiser hybrid duplica a lógica.

### Option 2: Adapter da POC AgentFlow

Gateway proxia o WebSocket `/ws/realtime-stream` da POC existente. Gateway
adiciona auth/audit/rate-limit; POC continua orquestrando.

- **Pros:**
  - Mais rápido (~1 semana). Reaproveita 100% da POC.
  - Voz natural via ElevenLabs imediata.
  - Não duplica lógica.
- **Cons:**
  - Acoplamento operacional: gateway depende da POC viva pra fluxo de
    áudio. Falha cascateia.
  - Gateway perde controle de provider switching, observability fica
    distribuída.
  - Sobrecarrega a POC com responsabilidade de ser "core de áudio" sem
    ela ter sido desenhada pra multi-tenant.
  - Esforço de "consolidar pipeline no gateway" eventualmente vira
    inevitável; adiar não cancela.

### Option 3: Hybrid completo no gateway (CHOSEN)

Reescrever a sofisticação do `StreamingSessionRuntime` em Go nativo, com
providers integrados via interfaces, dois modos (`pure`/`hybrid`),
schema completo de sessões/turnos/fillers.

- **Pros:**
  - Voz natural (ElevenLabs via gateway).
  - Multi-tenant nativo (tier-aware, audit-aware, rate-limit-aware).
  - Gateway segue sendo single source of truth pra auth/audit/observability.
  - Provider switching futuro fica trivial (interfaces abstraem).
  - Independência operacional: gateway não precisa da POC pra operar.
- **Cons:**
  - **Escopo grande**: 2-3 meses focado.
  - Risco de bugs sutis durante portabilidade C# → Go (semantics de cancelation,
    state machines, race conditions em hot paths).
  - Duplicação temporária com a POC enquanto ela continuar em produção.
  - Custo ElevenLabs persiste e cresce com adoção.
- **Why chosen:** preserva a filosofia arquitetural do gateway (proxy
  autenticado multi-tenant), entrega valor próprio (auth/audit/multi-tenancy
  + voz natural), e é a única que combina **escala** + **voz humana** +
  **gateway-first**. Os 2-3 meses são absorvíveis via decomposição em
  sub-ondas commitáveis.

### Option 4: WebRTC end-to-end com TURN server

Frontend conecta direto no Voice Live via WebRTC (suportado pela Microsoft,
ver `voice-live-webrtc`). Gateway só intermedia auth/token issuance no
início.

- **Pros:**
  - Latência mínima — sem hop adicional do gateway no caminho do áudio.
  - Aproveita stack WebRTC mainstream do browser (codec negotiation,
    NAT traversal, jitter buffer, FEC).
  - Gateway-light (zero stateful WS).
- **Cons:**
  - Gateway perde controle de provider switching mid-sessão.
  - Observability vira tarefa do cliente (gateway não vê os frames).
  - Multi-tenancy fica precária: como medir uso por aplicação se o cliente
    fala direto com Azure?
  - Mobile/desktop nativos precisam de stack WebRTC própria — complica
    SDK do cliente.
  - Não cobre modo Hybrid (que precisa de orquestração ELO LLM+TTS no
    meio — não tem como fazer isso WebRTC-direto).
- **Why not:** Hybrid é o modo que entrega voz natural; WebRTC direto
  com Voice Live só cobre Pure. Adotar WebRTC obrigaria abdicar do modo
  Hybrid OU manter dois pipelines paralelos. Complexidade desnecessária.

### Option 5: gRPC bidirecional em vez de WebSocket

Transport entre cliente↔gateway via gRPC streaming (`bidirectional`).

- **Pros:**
  - Schema fortemente tipado via Protocol Buffers — menos bugs em produção.
  - Melhor pra clientes mobile nativos (HTTP/2 + flow control + interceptors).
- **Cons:**
  - **Browsers não falam gRPC nativo**. Precisa gRPC-Web proxy ou
    Connect/Buf — adiciona camada e quebra a "transport unification".
  - Voice Live e ElevenLabs falam WebSocket — gateway teria que traduzir
    gRPC↔WS no upstream também.
- **Why not:** complexidade desproporcional ao ganho. WebSocket é
  mainstream pra cliente browser+mobile e o que a POC e a doc do Voice
  Live assumem.

## Consequences

### Positive

- **Voz natural** via ElevenLabs no modo Hybrid; gateway entrega o que
  a POC já valida em produção.
- **Centralização de auth/audit/rate-limit** pra áudio — mesma filosofia
  que pra texto.
- **Multi-tenancy nativo**: cada aplicação tem `mode`, `allowed_voices`,
  `allowed_llm_models`, `monthly_audio_minutes_budget` etc.
- **Provider switching futuro indolor**: interface `TTSProvider` permite
  trocar ElevenLabs por Cartesia ou Azure HD voices sem afetar cliente.
- **Observability unificada**: dashboards de áudio nascem na mesma
  fundação (`audio_sessions`, `audio_session_turns`) que dashboards de
  texto (`usage_events`).
- **Independência operacional**: gateway não depende da POC AgentFlow estar
  viva. POC pode evoluir/morrer em ritmo próprio.
- **Reaproveita ondas anteriores**: KV (ADR-0018) pra secrets ElevenLabs/
  Azure OpenAI, RBAC (ADR-0011) pra admins de áudio, schema gogateway
  (ADR-0022) pra novas tabelas.

### Negative / Trade-offs

- **Implementação extensa**: 2-3 meses focado. Janela de execução longa
  exige disciplina de manter sub-ondas commitáveis.
- **Dependência de ElevenLabs**: custo de inferência por minuto +
  risco de provider externo (downtime, mudança de pricing, deprecation
  de API).
- **Complexidade de stateful WebSocket bidirecional em Go**: cancelation
  semantics, channel orchestration, goroutine leak prevention — fácil
  errar. Code review extra é necessidade.
- **Pipeline tem 5+ providers integrados** (Voice Live STT, Azure OpenAI
  LLM, ElevenLabs TTS + fallbacks futuros) — superfície de teste integração
  é grande.
- **Duplicação temporária com POC**: enquanto ambos os sistemas
  coexistirem, há esforço de manter feature parity ou marcar diferenças
  intencionais.

### Mitigations

- **Sub-ondas independentes**: 8.1 (Pure) sozinha já entrega proxy
  auditado de Voice Live — valor próprio. Não somos forçados a esperar
  3 meses pra demonstrar.
- **Interfaces de provider** (`STTProvider`/`LLMProvider`/`TTSProvider`)
  desacoplam pipeline de implementação concreta. Trocar ElevenLabs por
  Cartesia = nova struct que satisfaz interface, zero mudança no
  orchestrator.
- **ADR-0024 dedicado a TTS provider abstraction**: revisa ElevenLabs vs
  Cartesia vs Azure HD durante sub-onda 8.2 com dados reais.
- **Testes unit por provider + integration por modo + smoke E2E em
  homolog**: pirâmide de testes obrigatória antes de cada sub-onda
  fechar.
- **Load test (k6/vegeta) na sub-onda 8.5** simulando múltiplas sessões
  simultâneas — descobre vazamentos de goroutines e contenção de DB antes
  de produção.
- **Code review obrigatório por sub-onda** com foco em cancelation
  semantics e race conditions.
- **POC AgentFlow não é descontinuada na implementação**; coexistência
  permite comparação A/B em produção real.

## Implementation sketch

### Schema do banco

Três migrations novas (010 já está ocupada — admin root user; próximas
são 011-013).

#### Migration 011 — `gogateway.audio_sessions`

```sql
-- 011_audio_sessions.up.sql
CREATE TABLE gogateway.audio_sessions (
    id                    BIGINT          IDENTITY(1,1) PRIMARY KEY,
    session_id            NVARCHAR(64)    NOT NULL UNIQUE,
    application_id        BIGINT          NOT NULL,
    mode                  NVARCHAR(20)    NOT NULL
                              CONSTRAINT ck_audio_sessions_mode CHECK (mode IN ('pure', 'hybrid')),
    voice_live_model      NVARCHAR(64)    NOT NULL,
    llm_model             NVARCHAR(64)    NULL,    -- só em hybrid
    tts_provider          NVARCHAR(32)    NULL,    -- 'elevenlabs', 'cartesia', 'azure-standard'
    tts_voice_id          NVARCHAR(128)   NULL,
    started_at            DATETIMEOFFSET  NOT NULL DEFAULT SYSUTCDATETIME(),
    ended_at              DATETIMEOFFSET  NULL,
    disconnect_reason     NVARCHAR(64)    NULL,    -- 'client_close', 'idle_timeout', 'server_error', 'provider_error'
    audio_seconds_in      DECIMAL(10,3)   NOT NULL DEFAULT 0,
    audio_seconds_out     DECIMAL(10,3)   NOT NULL DEFAULT 0,
    turns_completed       INT             NOT NULL DEFAULT 0,
    ws_connect_ms         INT             NULL,
    session_init_ms       INT             NULL,
    avg_first_audio_ms    INT             NULL,
    p95_first_audio_ms    INT             NULL,
    estimated_cost_brl    DECIMAL(14,6)   NULL,
    CONSTRAINT fk_audio_sessions_app FOREIGN KEY (application_id) REFERENCES gogateway.applications(id) ON DELETE CASCADE
);
CREATE INDEX idx_audio_sessions_app_started ON gogateway.audio_sessions(application_id, started_at DESC);
```

#### Migration 012 — `gogateway.audio_session_turns`

```sql
-- 012_audio_session_turns.up.sql
CREATE TABLE gogateway.audio_session_turns (
    id                          BIGINT          IDENTITY(1,1) PRIMARY KEY,
    audio_session_id            BIGINT          NOT NULL,
    turn_index                  INT             NOT NULL,
    user_speech_started_at      DATETIMEOFFSET  NULL,
    user_speech_stopped_at      DATETIMEOFFSET  NULL,
    user_transcript             NVARCHAR(MAX)   NULL,
    user_transcript_at          DATETIMEOFFSET  NULL,
    llm_request_started_at      DATETIMEOFFSET  NULL,
    llm_first_token_at          DATETIMEOFFSET  NULL,
    tts_request_started_at      DATETIMEOFFSET  NULL,
    tts_first_audio_at          DATETIMEOFFSET  NULL,
    client_first_audio_sent_at  DATETIMEOFFSET  NULL,
    assistant_done_at           DATETIMEOFFSET  NULL,
    assistant_text              NVARCHAR(MAX)   NULL,
    filler_scenario             NVARCHAR(64)    NULL,
    filler_sent_at              DATETIMEOFFSET  NULL,
    input_tokens                INT             NULL,
    output_tokens               INT             NULL,
    CONSTRAINT fk_audio_session_turns FOREIGN KEY (audio_session_id) REFERENCES gogateway.audio_sessions(id) ON DELETE CASCADE
);
CREATE INDEX idx_audio_session_turns_session ON gogateway.audio_session_turns(audio_session_id, turn_index);
```

#### Migration 013 — `gogateway.audio_fillers`

```sql
-- 013_audio_fillers.up.sql
CREATE TABLE gogateway.audio_fillers (
    id                  BIGINT          IDENTITY(1,1) PRIMARY KEY,
    scenario            NVARCHAR(64)    NOT NULL,    -- 'acceptance', 'hold_request', 'price_objection', etc.
    phrase              NVARCHAR(MAX)   NOT NULL,
    language            NVARCHAR(10)    NOT NULL DEFAULT 'pt-BR',
    cached_pcm_base64   NVARCHAR(MAX)   NULL,        -- pré-sintetizado; se NULL, gateway sintetiza on-demand
    tts_provider        NVARCHAR(32)    NULL,
    tts_voice_id        NVARCHAR(128)   NULL,
    active              BIT             NOT NULL DEFAULT 1,
    created_at          DATETIMEOFFSET  NOT NULL DEFAULT SYSUTCDATETIME()
);
CREATE INDEX idx_audio_fillers_scenario ON gogateway.audio_fillers(scenario) WHERE active = 1;
```

#### Migration 014 — `gogateway.applications` ganha campos pra áudio

```sql
-- 014_applications_audio_fields.up.sql
ALTER TABLE gogateway.applications
    ADD audio_mode                  NVARCHAR(20) NULL,        -- 'pure', 'hybrid', NULL = bloqueado
        allowed_voice_models        NVARCHAR(MAX) NOT NULL
            CONSTRAINT df_apps_allowed_voice_models DEFAULT N'[]'
            CONSTRAINT ck_apps_allowed_voice_models CHECK (ISJSON(allowed_voice_models) = 1),
        allowed_tts_providers       NVARCHAR(MAX) NOT NULL
            CONSTRAINT df_apps_allowed_tts_providers DEFAULT N'[]'
            CONSTRAINT ck_apps_allowed_tts_providers CHECK (ISJSON(allowed_tts_providers) = 1),
        monthly_audio_minutes_budget DECIMAL(10,2) NOT NULL DEFAULT 0;
```

### Schema de eventos cliente↔gateway

Cliente fala JSON exceto frames de áudio (binary). Inspirado no schema da
POC (`StreamingSessionRuntime` linha 117 emite `audio.format`, `assistant.thinking`,
etc.) mas refinado.

**Cliente → Gateway:**

| Evento | Tipo | Payload |
|---|---|---|
| `session.config` | JSON | `{"mode":"hybrid","voice":"...","llm_model":"...","instructions":"..."}` (primeiro evento após upgrade) |
| `audio.frame` | binary | PCM 16-bit 24 kHz mono LE (ou 16 kHz, definido no `session.config`) |
| `interrupt` | JSON | `{"type":"interrupt"}` — cancela turno corrente, libera mic |
| `session.end` | JSON | `{"type":"session.end"}` — encerra graceful |

**Gateway → Cliente:**

| Evento | Tipo | Payload |
|---|---|---|
| `session.ready` | JSON | `{"session_id":"...","audio_format":{"encoding":"pcm_s16le","sample_rate":24000,"channels":1}}` |
| `speech.started` | JSON | `{"type":"speech.started"}` (VAD detectou) |
| `speech.stopped` | JSON | `{"type":"speech.stopped"}` |
| `transcript.user` | JSON | `{"type":"transcript.user","text":"...","final":true}` |
| `assistant.thinking` | JSON | `{"type":"assistant.thinking"}` (LLM em processamento) |
| `assistant.filler` | JSON | `{"type":"assistant.filler","phrase":"...","scenario":"acceptance"}` |
| `assistant.text.delta` | JSON | `{"type":"assistant.text.delta","text":"..."}` (streaming LLM) |
| `audio.frame` | binary | PCM da resposta (chunks contínuos) |
| `assistant.done` | JSON | `{"type":"assistant.done"}` |
| `error` | JSON | `{"type":"error","code":"upstream_error","message":"..."}` |

### Tipos Go (esqueleto)

Sob `internal/realtime/`:

```go
package realtime

import (
    "context"
    "log/slog"
    "sync"
    "time"

    "github.com/coder/websocket"
)

type SessionMode string

const (
    ModePure   SessionMode = "pure"
    ModeHybrid SessionMode = "hybrid"
)

type SessionConfig struct {
    Mode           SessionMode
    VoiceLiveModel string
    LLMModel       string // hybrid only
    TTSProvider    string // hybrid only — "elevenlabs"/"cartesia"/"azure-standard"
    TTSVoiceID     string // hybrid only
    VoiceName      string // pure only — Azure voice name
    Instructions   string
    SampleRate     int // 16000 ou 24000
    Language       string
}

type Session struct {
    ID            string
    ApplicationID int64

    Config SessionConfig

    // Resolved providers; nil indicates "not used in this mode".
    STT STTProvider
    LLM LLMProvider // hybrid only
    TTS TTSProvider // hybrid only

    Client   *websocket.Conn
    Upstream *websocket.Conn // Voice Live

    // Current turn state, replaced each turn.
    TurnMu sync.Mutex
    Turn   *TurnState

    Logger *slog.Logger

    // Lifecycle
    Ctx    context.Context
    Cancel context.CancelFunc
}

type TurnState struct {
    Index    int
    Ctx      context.Context
    Cancel   context.CancelFunc

    UserSpeechStartedAt time.Time
    UserSpeechStoppedAt time.Time
    UserTranscript      string
    UserTranscriptAt    time.Time

    LLMRequestStartedAt   time.Time
    LLMFirstTokenAt       time.Time
    AssistantTextBuffer   string

    TTSRequestStartedAt    time.Time
    TTSFirstAudioAt        time.Time
    ClientFirstAudioSentAt time.Time
    AssistantDoneAt        time.Time

    FillerScenario string
    FillerSentAt   time.Time
}

// STTProvider abstracts Voice Live (or any STT engine that produces
// transcripts via stream).
type STTProvider interface {
    Start(ctx context.Context, cfg STTConfig) (<-chan STTEvent, error)
    SendAudio(ctx context.Context, frame []byte) error
    Close() error
}

type STTConfig struct {
    Endpoint           string
    APIKey             string // sourced from KV
    Model              string
    InputSampleRate    int
    TurnDetectionParams map[string]any
}

type STTEvent struct {
    Type       string // "speech_started", "speech_stopped", "transcript", "audio_delta", "error"
    Transcript string
    Final      bool
    Audio      []byte // only when Type=audio_delta (pure mode)
    Err        error
}

// LLMProvider abstracts the LLM (Azure OpenAI streaming, etc.).
type LLMProvider interface {
    Stream(ctx context.Context, prompt LLMPrompt) (<-chan LLMChunk, error)
}

type LLMPrompt struct {
    System   string
    History  []LLMMessage
    UserText string
    Model    string
}

type LLMChunk struct {
    Text  string
    Done  bool
    Usage *LLMUsage
    Err   error
}

// TTSProvider abstracts TTS (ElevenLabs, Cartesia, Azure standard).
type TTSProvider interface {
    Stream(ctx context.Context, req TTSRequest) (<-chan TTSChunk, error)
}

type TTSRequest struct {
    Text       string
    VoiceID    string
    SampleRate int
}

type TTSChunk struct {
    Audio []byte // PCM 16-bit mono LE in the requested SampleRate
    Done  bool
    Err   error
}
```

### Goroutines per session

```
goroutine 1: clientReader     — reads audio.frame + control events from client WS
goroutine 2: clientWriter     — writes events + audio.frame to client WS (channels)
goroutine 3: sttEventLoop     — consumes STTProvider events, orchestrates turns
goroutine 4: turnLLMStream    — runs per-turn: feeds prompt to LLMProvider, accumulates text
goroutine 5: turnTTSStream    — runs per-turn: feeds text chunks to TTSProvider, emits audio
goroutine 6: fillerWatchdog   — after threshold, emits assistant.filler if LLM still processing
goroutine 7: idleTimeout      — closes session after N minutes inactivity
```

Cancelation tree:
- `session.Ctx` cancela tudo
- `session.Turn.Ctx` (descendant) cancela só o turno corrente
- `interrupt` event do cliente: `session.Turn.Cancel()` → goroutines 4/5/6 abortam
- Nova fala do usuário (`speech.started`) também: `session.Turn.Cancel()` antes de iniciar novo turno

### State machine de turno

```
[Idle]
   │
   │ STT emit speech_started
   ▼
[UserSpeaking]
   │
   │ STT emit speech_stopped + transcript final
   ▼
[Thinking]              ─── filler watchdog dispara ───►  [Thinking + Filler]
   │                                                              │
   │ LLM first token                                               │ same
   ▼                                                              ▼
[AssistantTextStreaming]                            [AssistantTextStreaming]
   │
   │ chunk boundary atingido (frase completa ou N chars)
   ▼
[TTSStreaming]
   │
   │ TTS emit first audio chunk
   ▼
[ClientPlayback] — chunks continuam fluindo
   │
   │ STT emit speech_started (user interrupts) ──► [Cancel + back to UserSpeaking]
   │
   │ TTS done + LLM done
   ▼
[TurnComplete] ── back to [Idle]
```

Cada transição é persistida em `gogateway.audio_session_turns` (campos
`*_at` correspondentes).

## Sub-onda mapping

Cada sub-onda fecha uma etapa do pipeline e é commit\ável independentemente.

### 8.0 — ADR-0023 redigido + aprovado (este documento)

- **Estimativa:** 1-2 dias (escrita + revisão)
- **Entrega:** documento aprovado, status `accepted` quando owner sign-off
- **Bloqueia:** 8.1

### 8.1 — Proxy Pure Voice Live no gateway

- **Estimativa:** 2 semanas
- **Entregas:**
  - `internal/realtime/` pacote novo (Session, TurnState, interfaces)
  - `STTProvider` Voice Live implementado (WebSocket cliente, schema de eventos)
  - WS server no gateway: `/v1/realtime` (auth no upgrade, mode=pure)
  - Migration 011 + 014 aplicadas (audio_sessions + applications.audio_mode)
  - Auth/audit/rate-limit no upgrade WebSocket
  - Modo Pure 100% funcional via gateway (voz Azure standard, latência baixa)
- **Não inclui:** LLM, TTS, fillers, frontend
- **Bloqueia:** 8.2

### 8.2 — Modo Hybrid básico

- **Estimativa:** 3-4 semanas
- **Entregas:**
  - `LLMProvider` Azure OpenAI streaming (HTTP/SSE)
  - `TTSProvider` ElevenLabs streaming (HTTP/SSE — endpoint `/stream`)
  - Orchestrator combina STT → LLM → TTS por turno
  - Migration 012 + 013 (audio_session_turns + audio_fillers)
  - ADR-0024 dedicado a TTS provider abstraction (durante sub-onda)
  - Modo Hybrid funcional, voz natural via ElevenLabs
- **Não inclui:** fillers semânticos sofisticados, barge-in completo
- **Bloqueia:** 8.3

### 8.3 — Fillers semânticos + barge-in + containment

- **Estimativa:** 1-2 semanas
- **Entregas:**
  - CRUD admin de `gogateway.audio_fillers`
  - Classificação de cenário do transcript do usuário (regex/keywords;
    LLM classifier futuro)
  - Watchdog goroutine que emite filler se LLM demora >X ms
  - Cache de áudio pré-sintetizado por scenario+voice
  - Barge-in handling: novo speech.started cancela turno corrente
  - Containment mode: se usuário interrompe N vezes seguidas, aumenta
    threshold de barge-in temporariamente
  - Yield mode: detecta longa fala do usuário e segura resposta
- **Não inclui:** frontend dedicado, load tests
- **Bloqueia:** 8.4

### 8.4 — Frontend cliente

- **Estimativa:** 1-2 semanas
- **Entregas:**
  - Decisão ADR-0026: WebSocket cliente direto vs WebRTC
  - SPA React em `web/audio/` que captura mic via getUserMedia,
    audio worklet, abre WS com gateway, toca áudio recebido
  - OU cliente Go isolado em `cmd/audio-client/` pra mobile/desktop
  - Permissão de mic, indicador visual de speech_started/stopped,
    transcrição em tempo real
- **Não inclui:** load tests, hardening
- **Bloqueia:** 8.5

### 8.5 — Polimento + observability + load tests

- **Estimativa:** 1 semana
- **Entregas:**
  - Endpoints admin pra dashboards (`/admin/v1/audio/sessions`, `/turns`,
    `/fillers`)
  - Dashboard nativo no Console (timeseries de latência, sessões ativas,
    custo BRL acumulado)
  - Alertas: erro rate, latência p95 acima de threshold
  - Load test k6/vegeta com N sessões simultâneas (target 100? 500?
    pendente)
  - Profiling: descobrir gargalos (alloc/sec, gc pauses)
  - Documentação operacional final

**Total**: ~8-13 semanas (~2-3 meses).

## Open questions

1. **Custo ElevenLabs por minuto** vs Cartesia vs Azure HD: dados reais
   pendentes. ADR-0024 (subordinado a 8.2) decide com números.
2. **Cartesia como fallback automático**: se ElevenLabs cair, gateway
   tenta Cartesia transparente? Adiciona complexidade — decidir.
3. **Multi-tenancy de Voice Live**: 1 conta Azure suporta quantas sessões
   simultâneas? Existem quotas por modelo? Pesquisar antes de 8.5.
4. **Schema de agentes/personalidade**: precisa de tabela `gogateway.agents`
   (instructions, fillers config) ou continua tudo dentro de
   `gogateway.applications`? Provavelmente sim, mas ADR-0025 endereça
   se demanda surgir em 8.3.
5. **Frontend transport**: WebSocket nativo do browser ou WebRTC com
   audio worklet? Influencia 8.4 — ADR-0026 dedicado.
6. **Filler classifier futuro com LLM dedicado**: faz sentido em 8.3
   inicial (regex/keywords) e evolui depois?
7. **Política de retenção de transcripts**: `audio_session_turns.user_transcript`
   contém fala literal do usuário — LGPD implica retenção limitada.
   Endereçar em §3.6 do roadmap quando frente entrar.
8. **Fallback Voice Live → outro STT**: Whisper deployment? Azure Speech
   Services normal? Pendente.

## References

- ADR-0021 — Latency breakdown observável (não aplica a audio sessions; schema diferente porém filosofia de instrumentação por sessão é replicada)
- ADR-0022 — Troca PG → SQL Server (schema gogateway preserva)
- POC TPCore.Modules.AgentFlow `LATENCIA_REALTIME_STREAM.md` (mapa de 24 pontos de latência da arquitetura híbrida)
- POC TPCore.Modules.AgentFlow `Realtime/StreamingSessionRuntime.cs` (referência do modo Hybrid, ~3000 LOC C#)
- POC TPCore.Modules.AgentFlow `Realtime/PureVoiceLiveSessionRuntime.cs` (referência do modo Pure, ~1000 LOC C#)
- POC TPCore.Modules.AgentFlow `Realtime/VoiceLiveRuntimeMode.cs` (lógica de seleção de modo)
- POC TPCore.Modules.AgentFlow `Providers/AzureVoiceLiveProvider.cs`, `ElevenLabsTtsProvider.cs`, `AzureOpenAiStreamingProvider.cs`
- Spike Go: `_voicelive-spike/` (3 arquivos, ~500 LOC) — proxy WS minimal pra Voice Live, mediu baseline
- Spike analysis: `_voicelive-spike/POC_ANALYSIS.md` (comparação detalhada com a POC)
- Doc Voice Live API: https://learn.microsoft.com/en-us/azure/ai-services/speech-service/voice-live
- Doc Voice Live how-to: https://learn.microsoft.com/en-us/azure/ai-services/speech-service/voice-live-how-to
- Doc Voice Live customize: https://learn.microsoft.com/en-us/azure/ai-services/speech-service/voice-live-how-to-customize
- Doc Realtime API events (compatible schema): https://learn.microsoft.com/en-us/azure/ai-foundry/openai/realtime-audio-reference
- Doc Azure OpenAI streaming: https://learn.microsoft.com/en-us/azure/ai-services/openai/reference
- ElevenLabs streaming API: https://elevenlabs.io/docs/api-reference/text-to-speech-stream
- Cartesia API: https://docs.cartesia.ai/
- coder/websocket Go library: https://github.com/coder/websocket
- gen2brain/malgo (audio I/O CGO): https://github.com/gen2brain/malgo
- Roadmap §3.1 (Desempenho) entry pra Onda 8
- Roadmap §6 — decomposição 8.0-8.5
