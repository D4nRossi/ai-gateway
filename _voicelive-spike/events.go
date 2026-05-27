package main

// Voice Live realtime event schemas — subset, just what the spike needs.
//
// Voice Live is event-compatible with Azure OpenAI Realtime API:
// https://learn.microsoft.com/en-us/azure/ai-foundry/openai/realtime-audio-reference

// clientEvent is any event the client sends to the server. Unused fields are
// omitempty so each call site only fills what's relevant.
type clientEvent struct {
	Type    string         `json:"type"`
	EventID string         `json:"event_id,omitempty"`
	Session map[string]any `json:"session,omitempty"` // session.update
	Audio   string         `json:"audio,omitempty"`   // input_audio_buffer.append (base64 PCM16)
}

// serverEvent captures the common fields across server→client events.
// The "type" field is the discriminator; downstream code switches on it.
type serverEvent struct {
	Type    string `json:"type"`
	EventID string `json:"event_id,omitempty"`

	// response.audio.delta — base64 PCM16 audio chunk from the model.
	Delta string `json:"delta,omitempty"`

	// response.audio_transcript.delta — text transcript of model's audio.
	Transcript string `json:"transcript,omitempty"`

	// error events
	Error *struct {
		Type    string `json:"type"`
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`

	// session.updated includes a Session block; for the spike we just log
	// receipt — we don't need to parse it deeply.
	Session map[string]any `json:"session,omitempty"`
}

// Known event types we care about (server → client). String constants reduce
// typos when switching.
const (
	evtSessionUpdated         = "session.updated"
	evtInputBufSpeechStarted  = "input_audio_buffer.speech_started"
	evtInputBufSpeechStopped  = "input_audio_buffer.speech_stopped"
	evtInputBufCommitted      = "input_audio_buffer.committed"
	evtResponseAudioDelta     = "response.audio.delta"
	evtResponseAudioDone      = "response.audio.done"
	evtResponseAudioTransDelt = "response.audio_transcript.delta"
	evtResponseAudioTransDone = "response.audio_transcript.done"
	evtResponseCreated        = "response.created"
	evtResponseDone           = "response.done"
	evtError                  = "error"
)

// Known event types we send (client → server).
const (
	evtSessionUpdate       = "session.update"
	evtInputAudioBufAppend = "input_audio_buffer.append"
	evtInputAudioBufCommit = "input_audio_buffer.commit"
	evtResponseCreate      = "response.create"
)
