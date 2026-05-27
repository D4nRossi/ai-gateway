// voicelive-spike — cliente Go isolado que conecta direto no Azure Voice Live
// para medir baseline de latência sem o gateway no meio. Ver README.md.
//
// Conectividade: WebSocket wss://<endpoint>/voice-live/realtime + header api-key.
// Audio: malgo (mini-audio bindings, CGO) — captura mic e toca speaker em
// PCM 16-bit / 24 kHz mono.
//
// Pasta começa com `_` para que as ferramentas Go (go build, go vet, go test
// no diretório raiz do gateway) IGNOREM esta pasta — o spike tem seu próprio
// go.mod e não polui o build principal.
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/coder/websocket"
)

// ── configuration ─────────────────────────────────────────────────────────

type config struct {
	Endpoint     string // https://<resource>.cognitiveservices.azure.com
	APIKey       string
	Model        string
	Voice        string
	Instructions string
	Language     string
}

func loadConfig() (config, error) {
	cfg := config{
		Endpoint:     os.Getenv("VOICE_LIVE_ENDPOINT"),
		APIKey:       os.Getenv("VOICE_LIVE_KEY"),
		Model:        getenv("VOICE_LIVE_MODEL", "gpt-realtime-mini"),
		// HD voice pt-BR (Dragon HD) — qualidade superior vs Francisca Neural standard.
		// Alternativas no .env.example. HD voices aceitam temperature/rate; ver
		// sendSessionUpdate pra como passar.
		Voice:        getenv("VOICE_LIVE_VOICE", "pt-BR-Thalita:DragonHDLatestNeural"),
		Instructions: getenv("VOICE_LIVE_INSTRUCTIONS", "Você é uma atendente de call center natural e acolhedora. Fale com tom humano, faça pausas naturais entre as ideias, evite repetir cumprimentos a cada turno. Use frases curtas e diretas."),
		Language:     getenv("VOICE_LIVE_LANGUAGE", "pt-BR"),
	}
	if cfg.Endpoint == "" {
		return cfg, errors.New("VOICE_LIVE_ENDPOINT is required (e.g. https://td-foundry-voiceflow-rsc-eus2.cognitiveservices.azure.com)")
	}
	if cfg.APIKey == "" {
		return cfg, errors.New("VOICE_LIVE_KEY is required (Azure Foundry resource API key)")
	}
	return cfg, nil
}

func getenv(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

// ── latency metrics ───────────────────────────────────────────────────────
//
// All timestamps are nanos since process start (monotonic), stored atomically
// so the audio callback thread + WS read loop + WS write loop can update them
// without locking. Negative/zero means "not yet observed".

// metrics — versão consertada (2026-05-27):
//
// Bug original: timer base era `speech_started`, mas VAD do Voice Live
// dispara speech_started várias vezes em hesitações/pausas curtas. Cada
// disparo zerava o timer, dando números absurdos como first_audio_ms=22.
//
// Versão atual: timer base é o ÚLTIMO `speech_stopped` antes de uma
// response.audio.delta. Métrica reflete UX real: "user calou → primeiro som
// do agente". E o reset acontece em `response.done` (fim de turno completo),
// não em speech_started.
type metrics struct {
	wsConnectMs     int64 // observed at end of websocket.Dial
	sessionUpdateMs int64 // observed when session.updated received

	// Per-turn timers (reset on response.done).
	turnSpeechStoppedAt   atomic.Int64 // nanos — last speech_stopped (UX-perceived base)
	turnFirstAudioOutAt   atomic.Int64 // nanos — first audio.delta received in this turn
	turnLastAudioDeltaAt  atomic.Int64
	turnAudioDeltaCount   atomic.Int64
	turnAudioBytesEmitted atomic.Int64
}

// recordSpeechStop captures the latest "you stopped talking" timestamp.
// Multiple speech_started events between turns are now ignored by metrics
// (VAD jitter / hesitations don't corrupt measurements).
func (m *metrics) recordSpeechStop() {
	m.turnSpeechStoppedAt.Store(time.Now().UnixNano())
}

func (m *metrics) recordAudioDelta(deltaBytes int) {
	now := time.Now().UnixNano()
	if m.turnFirstAudioOutAt.Load() == 0 {
		m.turnFirstAudioOutAt.Store(now)
	}
	m.turnLastAudioDeltaAt.Store(now)
	m.turnAudioDeltaCount.Add(1)
	m.turnAudioBytesEmitted.Add(int64(deltaBytes))
}

func (m *metrics) summarizeTurn(logger *slog.Logger) {
	stopped := m.turnSpeechStoppedAt.Load()
	first := m.turnFirstAudioOutAt.Load()
	last := m.turnLastAudioDeltaAt.Load()
	count := m.turnAudioDeltaCount.Load()
	bytes := m.turnAudioBytesEmitted.Load()

	// Reset before logging so the next turn starts clean. Done regardless of
	// whether this turn produced usable measurements.
	defer m.resetTurn()

	if stopped == 0 || first == 0 || count == 0 {
		// VAD didn't emit speech_stopped (could be initial response triggered
		// by something other than voice) or we never saw audio. Skip the log
		// to avoid noise.
		return
	}

	firstAudioMs := (first - stopped) / 1_000_000
	durationMs := (last - first) / 1_000_000

	avgChunkMs := int64(0)
	if count > 1 {
		avgChunkMs = durationMs / count
	}

	// sampleRate (16000) × 2 bytes (PCM16) × 1 ch = 32000 B/s of audio
	audioSeconds := float64(bytes) / float64(sampleRate*bytesPerFrame*channels)

	logger.Info("turn summary",
		"first_audio_ms", firstAudioMs, // ← speech_stopped → first audio.delta (UX-perceived)
		"response_duration_ms", durationMs,
		"chunks", count,
		"audio_seconds", fmt.Sprintf("%.2f", audioSeconds),
		"avg_chunk_interval_ms", avgChunkMs,
	)
}

func (m *metrics) resetTurn() {
	m.turnSpeechStoppedAt.Store(0)
	m.turnFirstAudioOutAt.Store(0)
	m.turnLastAudioDeltaAt.Store(0)
	m.turnAudioDeltaCount.Store(0)
	m.turnAudioBytesEmitted.Store(0)
}

// ── WebSocket connect + handshake ────────────────────────────────────────

func connectWS(ctx context.Context, cfg config, logger *slog.Logger) (*websocket.Conn, time.Duration, error) {
	parsed, err := url.Parse(cfg.Endpoint)
	if err != nil {
		return nil, 0, fmt.Errorf("parsing endpoint URL %q: %w", cfg.Endpoint, err)
	}

	// Build wss:// URL with required query parameters.
	wsURL := url.URL{
		Scheme: "wss",
		Host:   parsed.Host,
		Path:   "/voice-live/realtime",
	}
	q := wsURL.Query()
	q.Set("api-version", "2025-10-01")
	q.Set("model", cfg.Model)
	wsURL.RawQuery = q.Encode()

	logger.Info("connecting", "url", wsURL.Host+wsURL.Path, "model", cfg.Model)

	headers := http.Header{}
	headers.Set("api-key", cfg.APIKey)

	start := time.Now()
	conn, resp, err := websocket.Dial(ctx, wsURL.String(), &websocket.DialOptions{
		HTTPHeader: headers,
	})
	elapsed := time.Since(start)
	if err != nil {
		// Surface the HTTP status if dial failed with a response (e.g. 401/404)
		// — this is the #1 source of confusion when secrets/URLs are wrong.
		status := "no-response"
		if resp != nil {
			status = resp.Status
		}
		return nil, elapsed, fmt.Errorf("websocket dial (http %s): %w", status, err)
	}

	// Default read limit is 32 KiB; Voice Live emits response.audio.delta
	// frames that can comfortably exceed that when chunks are large or when
	// transcripts come bundled. 4 MiB é folga ampla — protege ainda contra
	// frames malformed que viriam num atacante.
	conn.SetReadLimit(4 << 20)

	logger.Info("WebSocket connected", "ms", elapsed.Milliseconds())
	return conn, elapsed, nil
}

func sendSessionUpdate(ctx context.Context, conn *websocket.Conn, cfg config) error {
	// Config alinhada com a POC interna do owner que comprovadamente conecta
	// no mesmo resource. Diferenças vs primeiro draft:
	//   - VAD: azure_semantic_vad (sem _multilingual) + threshold/prefix/silence
	//     com valores conservadores que a POC usa em PT-BR.
	//   - Sample rate: 16000 (POC). Voice Live aceita 16k ou 24k; 16k é
	//     suficiente pra speech e custa menos banda.
	//   - "modalities" e os enhancers Microsoft (noise reduction + echo
	//     cancellation) mantidos.
	evt := clientEvent{
		Type: evtSessionUpdate,
		Session: map[string]any{
			"modalities":   []string{"text", "audio"},
			"instructions": cfg.Instructions,
			"voice": map[string]any{
				"name":        cfg.Voice,
				"type":        "azure-standard",
				"temperature": 0.85, // 0.0-1.0; ignorado em vozes standard, ativo em HD (Dragon HD).
				// "rate":     "1.05", // descomentar pra fala um pouco mais ágil; faixa 0.5-1.5
			},
			"turn_detection": map[string]any{
				"type":                "azure_semantic_vad",
				"threshold":           0.5,
				"prefix_padding_ms":   200,
				"silence_duration_ms": 375,
			},
			"input_audio_noise_reduction":   map[string]any{"type": "azure_deep_noise_suppression"},
			"input_audio_echo_cancellation": map[string]any{"type": "server_echo_cancellation"},
			"input_audio_sampling_rate":     sampleRate,
		},
	}
	data, err := json.Marshal(evt)
	if err != nil {
		return fmt.Errorf("marshal session.update: %w", err)
	}
	return conn.Write(ctx, websocket.MessageText, data)
}

// ── pipelines: mic → WS, WS → speaker ────────────────────────────────────

// runMicSender pulls chunks from a.micChunk and sends each as a base64
// input_audio_buffer.append event to the server. With Voice Live's
// azure_semantic_vad turn detection, the server decides when the user
// finished speaking — we don't manually commit/create-response.
func runMicSender(ctx context.Context, a *audioIO, conn *websocket.Conn, logger *slog.Logger) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case chunk, ok := <-a.micChunk:
			if !ok {
				return nil
			}
			// Sanity: only log a peak meter occasionally to avoid spamming.
			// (Every 50th chunk ~= every second at 20ms chunks.)
			//
			// Removed for clarity; uncomment when debugging mic capture.
			// _ = debugSamplePeak(chunk)

			evt := clientEvent{
				Type:  evtInputAudioBufAppend,
				Audio: base64.StdEncoding.EncodeToString(chunk),
			}
			data, err := json.Marshal(evt)
			if err != nil {
				return fmt.Errorf("marshal audio.append: %w", err)
			}
			if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
				return fmt.Errorf("ws write audio.append: %w", err)
			}
		}
	}
}

// runServerReadLoop reads server events and routes them: audio chunks go to
// the speaker buffer; transcript deltas are echoed to the console; metric
// events are tracked.
func runServerReadLoop(ctx context.Context, conn *websocket.Conn, a *audioIO, m *metrics, logger *slog.Logger) error {
	var transcriptBuf strings.Builder
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}
			return fmt.Errorf("ws read: %w", err)
		}

		var evt serverEvent
		if err := json.Unmarshal(data, &evt); err != nil {
			logger.Warn("unparseable server event", "err", err, "preview", truncate(string(data), 200))
			continue
		}

		switch evt.Type {
		case evtSessionUpdated:
			logger.Info("session updated")

		case evtInputBufSpeechStarted:
			logger.Info("speech started (you)")

		case evtInputBufSpeechStopped:
			m.recordSpeechStop()
			logger.Info("speech stopped (you) — waiting for response")

		case evtInputBufCommitted:
			logger.Debug("audio buffer committed by server")

		case evtResponseCreated:
			logger.Debug("response created", "event_id", evt.EventID)

		case evtResponseAudioDelta:
			// Push to playback buffer ASAP; metrics measure receipt time.
			rawLen := base64.StdEncoding.DecodedLen(len(evt.Delta))
			m.recordAudioDelta(rawLen)
			if err := a.pushPlayback(evt.Delta); err != nil {
				logger.Warn("push playback failed", "err", err)
			}

		case evtResponseAudioTransDelt:
			transcriptBuf.WriteString(evt.Delta)

		case evtResponseAudioTransDone:
			if transcriptBuf.Len() > 0 {
				logger.Info("assistant transcript", "text", transcriptBuf.String())
				transcriptBuf.Reset()
			}

		case evtResponseAudioDone:
			logger.Info("response audio complete")

		case evtResponseDone:
			m.summarizeTurn(logger)

		case evtError:
			if evt.Error != nil {
				logger.Error("server error",
					"type", evt.Error.Type,
					"code", evt.Error.Code,
					"message", evt.Error.Message,
				)
			} else {
				logger.Error("server error event with no detail", "preview", truncate(string(data), 200))
			}

		default:
			// Print unknown events at debug level so we can spot new ones
			// without spamming the console at info.
			logger.Debug("server event", "type", evt.Type)
		}
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// ── orchestration ─────────────────────────────────────────────────────────

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	cfg, err := loadConfig()
	if err != nil {
		logger.Error("config", "err", err)
		os.Exit(1)
	}

	if err := run(logger, cfg); err != nil {
		logger.Error("spike failed", "err", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger, cfg config) error {
	// Root context cancelled on SIGINT/SIGTERM.
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// 1) WebSocket
	conn, wsDuration, err := connectWS(ctx, cfg, logger)
	if err != nil {
		return err
	}
	defer func() {
		_ = conn.Close(websocket.StatusNormalClosure, "spike ended")
	}()
	m := &metrics{wsConnectMs: wsDuration.Milliseconds()}

	// 2) Audio devices
	a, err := initAudio(logger)
	if err != nil {
		return fmt.Errorf("init audio: %w", err)
	}
	defer a.close()

	// 3) Send session.update; the server will reply session.updated.
	sessionStart := time.Now()
	if err := sendSessionUpdate(ctx, conn, cfg); err != nil {
		return fmt.Errorf("send session.update: %w", err)
	}

	// 4) Start audio devices AFTER session.updated arrives, so we don't
	// dump silence into a server that isn't ready.
	//
	// sessionUpdatedCh é fechado pelo read loop na primeira ocorrência de
	// session.updated (via sync.Once para garantir exatamente uma vez).
	// Channel close é race-safe pra múltiplos waiters; aqui só temos um.
	sessionUpdatedCh := make(chan struct{})
	var sessionOnce sync.Once

	readErr := make(chan error, 1)
	go func() {
		readErr <- runServerReadLoopWithSessionHook(ctx, conn, a, m, logger, sessionStart, sessionUpdatedCh, &sessionOnce)
	}()

	// Wait briefly for session.updated before opening the mic. If não chega
	// em 5s, aborta — alguma coisa está mal configurada (endpoint, modelo,
	// chave, permissão).
	select {
	case <-sessionUpdatedCh:
		// session.updated visto.
	case err := <-readErr:
		return fmt.Errorf("read loop died before session.updated: %w", err)
	case <-time.After(5 * time.Second):
		return errors.New("timed out waiting for session.updated within 5s — check endpoint/model/key")
	case <-ctx.Done():
		return ctx.Err()
	}
	logger.Info("session updated, opening mic",
		"session_updated_ms", m.sessionUpdateMs,
		"ws_connect_ms", m.wsConnectMs,
	)

	if err := a.start(); err != nil {
		return fmt.Errorf("start audio devices: %w", err)
	}

	// 5) Start mic → WS sender.
	sendErr := make(chan error, 1)
	go func() {
		sendErr <- runMicSender(ctx, a, conn, logger)
	}()

	logger.Info("🎤 fale algo. Ctrl-C para sair.")

	// Wait for either pipeline to finish (error, EOF, or ctx cancel).
	select {
	case err := <-readErr:
		if err != nil && !errors.Is(err, context.Canceled) {
			return fmt.Errorf("server read loop: %w", err)
		}
	case err := <-sendErr:
		if err != nil && !errors.Is(err, context.Canceled) {
			return fmt.Errorf("mic sender: %w", err)
		}
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	}

	return nil
}

// runServerReadLoopWithSessionHook é o read loop principal usado pelo spike.
// Recebe um channel + once que são usados para sinalizar exatamente uma vez
// quando session.updated chega — assim o main pode esperar antes de abrir
// o microfone.
//
// A versão "limpa" runServerReadLoop acima é mantida para referência (e para
// uma eventual reutilização sem o hook de sincronização); aceitar a
// duplicação do switch é menos invasivo que parametrizar a versão pública
// com um callback genérico.
func runServerReadLoopWithSessionHook(ctx context.Context, conn *websocket.Conn, a *audioIO, m *metrics, logger *slog.Logger, sessionStart time.Time, sessionUpdatedCh chan<- struct{}, sessionOnce *sync.Once) error {
	var transcriptBuf strings.Builder
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}
			return fmt.Errorf("ws read: %w", err)
		}

		var evt serverEvent
		if err := json.Unmarshal(data, &evt); err != nil {
			logger.Warn("unparseable server event", "err", err, "preview", truncate(string(data), 200))
			continue
		}

		switch evt.Type {
		case evtSessionUpdated:
			sessionOnce.Do(func() {
				m.sessionUpdateMs = time.Since(sessionStart).Milliseconds()
				close(sessionUpdatedCh)
			})
			logger.Info("session updated", "ms", m.sessionUpdateMs)

		case evtInputBufSpeechStarted:
			// Don't reset metrics on speech_started — VAD can fire it
			// multiple times during hesitations/pauses, and we want the
			// timer anchored on the LAST speech_stopped. Just log.
			logger.Info("speech started (you)")

		case evtInputBufSpeechStopped:
			m.recordSpeechStop()
			logger.Info("speech stopped (you)")

		case evtResponseAudioDelta:
			rawLen := base64.StdEncoding.DecodedLen(len(evt.Delta))
			m.recordAudioDelta(rawLen)
			if err := a.pushPlayback(evt.Delta); err != nil {
				logger.Warn("push playback failed", "err", err)
			}

		case evtResponseAudioTransDelt:
			transcriptBuf.WriteString(evt.Delta)

		case evtResponseAudioTransDone:
			if transcriptBuf.Len() > 0 {
				logger.Info("assistant transcript", "text", transcriptBuf.String())
				transcriptBuf.Reset()
			}

		case evtResponseAudioDone:
			logger.Info("response audio complete")

		case evtResponseDone:
			m.summarizeTurn(logger)

		case evtError:
			if evt.Error != nil {
				logger.Error("server error",
					"type", evt.Error.Type,
					"code", evt.Error.Code,
					"message", evt.Error.Message,
				)
			} else {
				logger.Error("server error event with no detail", "preview", truncate(string(data), 200))
			}

		default:
			logger.Debug("server event", "type", evt.Type)
		}
	}
}

