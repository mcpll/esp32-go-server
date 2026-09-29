package eventbus

import (
	"time"

	. "xiaozhi-esp32-server-golang/internal/data/client"
)

// ExitChatEvent exit-chat event
type ExitChatEvent struct {
	// Client state
	ClientState *ClientState

	// Exit reason
	Reason string // "user exited", "tool-call exit", "timeout exit", etc.

	// Exit trigger type
	TriggerType string // "exit_words" (exit-word detect), "tool_call", "timeout", etc.

	// Raw user text if any
	UserText string

	// Timestamp
	Timestamp time.Time
}
