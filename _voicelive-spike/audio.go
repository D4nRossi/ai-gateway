package main

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"log/slog"
	"sync"

	"github.com/gen2brain/malgo"
)

// audioIO bundles the microphone capture device and speaker playback device
// behind a single struct. Both share the same malgo context (mini-audio).
//
// Format: signed 16-bit PCM, mono, 24kHz (matches Voice Live default
// input_audio_sampling_rate=24000). Voice Live accepts 16k or 24k; we use
// 24k for higher fidelity at marginal bandwidth cost.
type audioIO struct {
	ctx      *malgo.AllocatedContext
	mic      *malgo.Device
	speaker  *malgo.Device
	micChunk chan []byte // microphone → WS sender (raw PCM16 LE bytes)
	pbBuf    *playbackBuf
	logger   *slog.Logger
}

const (
	// 16 kHz aligns with the POC interna que conecta no mesmo resource.
	// Voice Live aceita 16k ou 24k; 16k é suficiente pra speech humana
	// (cobre faixa <8 kHz onde a inteligibilidade vive) e custa metade da
	// banda de upload.
	sampleRate    = 16000
	channels      = 1
	bytesPerFrame = 2 // int16
)

// playbackBuf is a thread-safe ring buffer feeding the speaker callback.
// We use a slice + mutex (not a channel) because the callback runs in a
// real-time audio thread that MUST NOT block — channel send under load
// would risk starvation. Mutex-guarded slice with non-blocking reads is the
// idiomatic pattern (see malgo examples).
type playbackBuf struct {
	mu   sync.Mutex
	data []byte // PCM16 LE bytes pending playback
}

func (b *playbackBuf) write(p []byte) {
	b.mu.Lock()
	b.data = append(b.data, p...)
	b.mu.Unlock()
}

// read fills out and returns how many bytes were filled. If fewer than len(out),
// pads with silence (zeros). Audio callback must always return a full buffer.
func (b *playbackBuf) read(out []byte) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := copy(out, b.data)
	b.data = b.data[n:]
	// Pad remainder with silence so the speaker doesn't underrun.
	for i := n; i < len(out); i++ {
		out[i] = 0
	}
	return n
}

// initAudio sets up the malgo context, capture device, and playback device.
// Devices are not started yet — call start() to begin streaming.
//
// References:
//   - https://github.com/gen2brain/malgo
//   - malgo wraps miniaudio (https://miniaud.io/)
func initAudio(logger *slog.Logger) (*audioIO, error) {
	mctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, func(msg string) {
		logger.Debug("malgo", "msg", msg)
	})
	if err != nil {
		return nil, fmt.Errorf("init malgo context: %w", err)
	}

	a := &audioIO{
		ctx:      mctx,
		micChunk: make(chan []byte, 64), // buffer ~1.3s of 20ms chunks
		pbBuf:    &playbackBuf{},
		logger:   logger,
	}

	// ── capture device ───────────────────────────────────────────────────
	captureCfg := malgo.DefaultDeviceConfig(malgo.Capture)
	captureCfg.Capture.Format = malgo.FormatS16
	captureCfg.Capture.Channels = channels
	captureCfg.SampleRate = sampleRate
	captureCfg.Alsa.NoMMap = 1

	captureCallbacks := malgo.DeviceCallbacks{
		Data: func(_, input []byte, _ uint32) {
			// Copy: input slice is reused by malgo after the callback returns.
			chunk := make([]byte, len(input))
			copy(chunk, input)
			select {
			case a.micChunk <- chunk:
			default:
				// Drop if WS sender can't keep up. Logging in real-time
				// audio callback is risky (allocates), so we drop silently.
			}
		},
	}

	mic, err := malgo.InitDevice(mctx.Context, captureCfg, captureCallbacks)
	if err != nil {
		_ = mctx.Uninit()
		return nil, fmt.Errorf("init capture device: %w", err)
	}
	a.mic = mic

	// ── playback device ──────────────────────────────────────────────────
	playbackCfg := malgo.DefaultDeviceConfig(malgo.Playback)
	playbackCfg.Playback.Format = malgo.FormatS16
	playbackCfg.Playback.Channels = channels
	playbackCfg.SampleRate = sampleRate
	playbackCfg.Alsa.NoMMap = 1

	playbackCallbacks := malgo.DeviceCallbacks{
		Data: func(output, _ []byte, _ uint32) {
			a.pbBuf.read(output)
		},
	}

	spk, err := malgo.InitDevice(mctx.Context, playbackCfg, playbackCallbacks)
	if err != nil {
		mic.Uninit()
		_ = mctx.Uninit()
		return nil, fmt.Errorf("init playback device: %w", err)
	}
	a.speaker = spk

	return a, nil
}

// start begins both capture and playback. Audio frames will start flowing
// through a.micChunk (input) and a.pbBuf (output) immediately.
func (a *audioIO) start() error {
	if err := a.mic.Start(); err != nil {
		return fmt.Errorf("start capture: %w", err)
	}
	if err := a.speaker.Start(); err != nil {
		_ = a.mic.Stop()
		return fmt.Errorf("start playback: %w", err)
	}
	return nil
}

// close stops devices and frees resources. Idempotent.
func (a *audioIO) close() {
	if a.mic != nil {
		_ = a.mic.Stop()
		a.mic.Uninit()
	}
	if a.speaker != nil {
		_ = a.speaker.Stop()
		a.speaker.Uninit()
	}
	if a.ctx != nil {
		_ = a.ctx.Uninit()
		a.ctx.Free()
	}
	close(a.micChunk)
}

// pushPlayback decodes the base64 chunk from a response.audio.delta event
// and queues raw PCM16 bytes for the speaker callback.
//
// SECURITY/CORRECTNESS: caller is responsible for receiving these chunks
// in order — out-of-order would distort the playback. coder/websocket
// delivers messages in order on a single connection, so this is fine here.
func (a *audioIO) pushPlayback(b64 string) error {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return fmt.Errorf("decode audio chunk: %w", err)
	}
	a.pbBuf.write(raw)
	return nil
}

// debugSamplePeak inspects a PCM16 chunk and returns the peak amplitude
// magnitude (0..32767). Useful to confirm the mic is actually capturing
// signal (peak > ~500 = real speech; peak < 50 = likely silence/noise floor).
func debugSamplePeak(pcm []byte) int16 {
	var peak int16
	for i := 0; i+1 < len(pcm); i += 2 {
		sample := int16(binary.LittleEndian.Uint16(pcm[i : i+2]))
		if sample < 0 {
			sample = -sample
		}
		if sample > peak {
			peak = sample
		}
	}
	return peak
}
