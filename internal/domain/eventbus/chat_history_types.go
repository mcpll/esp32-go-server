package eventbus

import (
	"context"
	"time"
)

// UserMessageEvent user message event
// Deprecated: use AddMessageEvent instead; unify on TopicAddMessage events
type UserMessageEvent struct {
	Ctx       context.Context
	SessionID string
	DeviceID  string
	AgentID   string

	// ASR result
	Text      string
	AudioData []byte // raw audio (PCM float32 as bytes)
	AudioSize int    // audio sample count

	// audio format info (for WAV conversion)
	SampleRate int // sample rate
	Channels   int // channels

	// metadata
	Timestamp time.Time
}

// AssistantMessageEvent assistant reply event
// Deprecated: use AddMessageEvent instead; unify on TopicAddMessage events
type AssistantMessageEvent struct {
	Ctx       context.Context
	SessionID string
	DeviceID  string
	AgentID   string

	// LLM result
	Text string

	// TTS result
	AudioData [][]byte // synthesized audio (Opus frame slices)
	AudioSize int      // audio size (bytes)

	// audio format info (for WAV conversion)
	SampleRate int // sample rate
	Channels   int // channels

	// metadata
	TTSDuration int // milliseconds
	Timestamp   time.Time
}
