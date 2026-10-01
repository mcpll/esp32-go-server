package eventbus

import (
	"time"
	. "xiaozhi-esp32-server-golang/internal/data/client"

	"github.com/cloudwego/eino/schema"
)

// AddMessageEvent unified message-add event
type AddMessageEvent struct {
	// Client state
	ClientState *ClientState

	// Message content (always schema.Message)
	// schema.Message is the standard LLM message format, including:
	// - Role: message role (User/Assistant/System/Tool)
	// - Content: message text
	// - ToolCalls: tool call list (optional)
	// - ToolCallID: tool call ID (for Tool role)
	Msg schema.Message

	// Message ID (links the two save stages)
	MessageID string

	// Audio data (optional; not part of schema.Message)
	// Stage 1: AudioData = nil (text only)
	// Stage 2: AudioData != nil (update audio)
	AudioData [][]byte // TTS/ASR audio frame array (Opus or PCM)
	AudioSize int      // Audio size in bytes

	// Audio format (not part of schema.Message)
	SampleRate int // Sample rate
	Channels   int // Channel count

	// Metadata (not part of schema.Message)
	Timestamp   time.Time
	TTSDuration int // TTS duration in milliseconds

	// Stage flag
	IsUpdate bool // true=update audio, false=add message
}
