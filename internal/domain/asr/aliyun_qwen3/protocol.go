package aliyun_qwen3

import (
	"encoding/base64"
	"encoding/json"

	log "xiaozhi-esp32-server-golang/logger"
)

// ClientEvent base struct for client-sent events
type ClientEvent struct {
	EventID string   `json:"event_id,omitempty"`
	Type    string   `json:"type"`
	Session *Session `json:"session,omitempty"`
	Audio   string   `json:"audio,omitempty"` // Base64-encoded audio data
}

// Session config inside session.update
type Session struct {
	Modalities              []string                 `json:"modalities"`
	InputAudioFormat        string                   `json:"input_audio_format,omitempty"`
	SampleRate              int                      `json:"sample_rate,omitempty"`
	InputAudioTranscription *InputAudioTranscription `json:"input_audio_transcription,omitempty"`
	TurnDetection           *TurnDetection           `json:"turn_detection"`
}

// InputAudioTranscription audio transcription config
type InputAudioTranscription struct {
	Language string `json:"language,omitempty"`
}

// TurnDetection VAD config
type TurnDetection struct {
	Type              string  `json:"type,omitempty"`                // "server_vad" or unset
	Threshold         float64 `json:"threshold,omitempty"`           // VAD threshold
	SilenceDurationMs int     `json:"silence_duration_ms,omitempty"` // silence duration (ms)
}

// ServerEvent base struct for server response events
type ServerEvent struct {
	Type            string     `json:"type"`
	EventID         string     `json:"event_id,omitempty"`
	PreviousEventID string     `json:"previous_event_id,omitempty"`
	Session         *Session   `json:"session,omitempty"`
	Item            *Item      `json:"item,omitempty"`
	Text            string     `json:"text,omitempty"`
	Stash           string     `json:"stash,omitempty"`
	Transcript      string     `json:"transcript,omitempty"`
	Error           *ErrorInfo `json:"error,omitempty"`
}

// Item session item (e.g. input audio transcription)
type Item struct {
	ID            string         `json:"id,omitempty"`
	Type          string         `json:"type,omitempty"`
	Status        string         `json:"status,omitempty"`
	Transcription *Transcription `json:"transcription,omitempty"`
}

// Transcription result
type Transcription struct {
	Text     string `json:"text,omitempty"`
	Language string `json:"language,omitempty"`
}

// ErrorInfo error details
type ErrorInfo struct {
	Message string `json:"message,omitempty"`
	Code    string `json:"code,omitempty"`
}

// NewSessionUpdateEvent creates a session.update event
func NewSessionUpdateEvent(config Config) *ClientEvent {
	session := &Session{
		Modalities:              []string{"text"},
		InputAudioFormat:        config.Format,
		SampleRate:              config.SampleRate,
		InputAudioTranscription: &InputAudioTranscription{Language: config.Language},
	}

	if config.AutoEnd {
		session.TurnDetection = &TurnDetection{
			Type:              "server_vad",
			Threshold:         config.VADThreshold,
			SilenceDurationMs: config.VADSilenceMs,
		}
	} else {
		session.TurnDetection = nil
	}

	event := &ClientEvent{
		EventID: "session_update",
		Type:    "session.update",
		Session: session,
	}

	// Debug: print session.update event
	if jsonBytes, err := json.Marshal(event); err == nil {
		log.Debugf("[aliyun_qwen3] session.update JSON: %s", string(jsonBytes))
	}

	return event
}

// NewAudioAppendEvent creates an input_audio_buffer.append event
func NewAudioAppendEvent(audioData []byte) *ClientEvent {
	encoded := base64.StdEncoding.EncodeToString(audioData)
	return &ClientEvent{
		Type:  "input_audio_buffer.append",
		Audio: encoded,
	}
}

// NewAudioCommitEvent creates an input_audio_buffer.commit event
func NewAudioCommitEvent() *ClientEvent {
	return &ClientEvent{
		EventID: "audio_commit",
		Type:    "input_audio_buffer.commit",
	}
}

// NewSessionFinishEvent creates a session.finish event
func NewSessionFinishEvent() *ClientEvent {
	return &ClientEvent{
		EventID: "session_finish",
		Type:    "session.finish",
	}
}

// IsTranscriptionEvent reports whether it is a transcription event
func IsTranscriptionEvent(event *ServerEvent) bool {
	return event.Type == "conversation.item.input_audio_transcription.text" ||
		event.Type == "conversation.item.input_audio_transcription.completed"
}

// IsFinalTranscription reports whether it is a final transcription
func IsFinalTranscription(event *ServerEvent) bool {
	return event.Type == "conversation.item.input_audio_transcription.completed"
}

// GetTranscriptionText returns transcription text
func GetTranscriptionText(event *ServerEvent) string {
	if event == nil {
		return ""
	}
	if event.Item != nil && event.Item.Transcription != nil && event.Item.Transcription.Text != "" {
		return event.Item.Transcription.Text
	}
	if event.Transcript != "" {
		return event.Transcript
	}
	if event.Text != "" {
		return event.Text
	}
	if event.Stash != "" {
		return event.Stash
	}
	return ""
}
