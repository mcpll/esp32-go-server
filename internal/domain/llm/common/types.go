package common

import (
	"github.com/cloudwego/eino/schema"
)

// Request and response structs
// Message is a chat message

// Response type constants
const (
	ResponseTypeContent   = "content"
	ResponseTypeToolCalls = "tool_calls"
)

type LLMResponseStruct struct {
	Text      string            `json:"text,omitempty"`
	IsStart   bool              `json:"is_start"`
	IsEnd     bool              `json:"is_end"`
	ToolCalls []schema.ToolCall `json:"tool_calls,omitempty"`
}
