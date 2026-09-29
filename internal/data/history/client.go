package history

import (
	"context"
	"fmt"
	"time"

	"xiaozhi-esp32-server-golang/internal/components/http"

	"github.com/cloudwego/eino/schema"
)

// MessageType message type
type MessageType string

const (
	MessageTypeUser      MessageType = "user"
	MessageTypeAssistant MessageType = "assistant"
	MessageTypeTool      MessageType = "tool"   // tool call result
	MessageTypeSystem    MessageType = "system" // system message (if used)
)

// HistoryClientConfig client config
type HistoryClientConfig struct {
	BaseURL   string        // Manager backend URL
	AuthToken string        // auth token
	Timeout   time.Duration // request timeout
	Enabled   bool          // whether enabled
}

// HistoryClient chat-history HTTP client
type HistoryClient struct {
	client  *http.ManagerClient
	enabled bool
}

// NewHistoryClient creates a chat-history client
func NewHistoryClient(cfg HistoryClientConfig) *HistoryClient {
	managerClient := http.NewManagerClient(http.ManagerClientConfig{
		BaseURL:    cfg.BaseURL,
		AuthToken:  cfg.AuthToken,
		Timeout:    cfg.Timeout,
		MaxRetries: 3, // default 3 retries
	})

	return &HistoryClient{
		client:  managerClient,
		enabled: cfg.Enabled,
	}
}

// SaveMessageRequest save-message request
type SaveMessageRequest struct {
	MessageID     string                 `json:"message_id"`
	DeviceID      string                 `json:"device_id"`
	AgentID       string                 `json:"agent_id"`
	SessionID     string                 `json:"session_id,omitempty"`
	Role          MessageType            `json:"role"`
	Content       string                 `json:"content"`
	ToolCallID    string                 `json:"tool_call_id,omitempty"`    // tool call ID (Tool role)
	ToolCallsJSON *string                `json:"tool_calls_json,omitempty"` // tool-calls JSON (Assistant role); nil means NULL
	AudioData     string                 `json:"audio_data,omitempty"`      // base64-encoded
	AudioFormat   string                 `json:"audio_format,omitempty"`
	AudioDuration int                    `json:"audio_duration,omitempty"`
	AudioSize     int                    `json:"audio_size,omitempty"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
}

// SaveMessage saves a message
func (c *HistoryClient) SaveMessage(ctx context.Context, req *SaveMessageRequest) error {
	if !c.enabled {
		return nil
	}
	return c.client.DoRequest(ctx, http.RequestOptions{
		Method: "POST",
		Path:   "/api/internal/history/messages",
		Body:   req,
	})
}

// UpdateMessageAudioRequest update-message-audio request
type UpdateMessageAudioRequest struct {
	MessageID   string                 `json:"message_id"`
	AudioData   string                 `json:"audio_data"` // base64-encoded
	AudioFormat string                 `json:"audio_format"`
	AudioSize   int                    `json:"audio_size"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// UpdateMessageAudio updates message audio
func (c *HistoryClient) UpdateMessageAudio(ctx context.Context, req *UpdateMessageAudioRequest) error {
	if !c.enabled {
		return nil
	}
	return c.client.DoRequest(ctx, http.RequestOptions{
		Method: "PUT",
		Path:   "/api/internal/history/messages/" + req.MessageID + "/audio",
		Body:   req,
	})
}

// GetMessagesRequest get-messages request
type GetMessagesRequest struct {
	DeviceID  string `json:"device_id"`
	AgentID   string `json:"agent_id"`
	SessionID string `json:"session_id,omitempty"`
	Limit     int    `json:"limit"` // limit count
}

// GetMessagesResponse get-messages response
type GetMessagesResponse struct {
	Messages []MessageItem `json:"messages"`
}

// MessageItem message item (for init load; no audio)
type MessageItem struct {
	MessageID  string            `json:"message_id"`
	Role       string            `json:"role"` // user/assistant/tool/system
	Content    string            `json:"content"`
	ToolCallID string            `json:"tool_call_id,omitempty"` // used by Tool role
	ToolCalls  []schema.ToolCall `json:"tool_calls,omitempty"`   // used by Assistant role
	CreatedAt  string            `json:"created_at"`
}

// GetMessages loads messages from the Manager DB (for init)
func (c *HistoryClient) GetMessages(ctx context.Context, req *GetMessagesRequest) (*GetMessagesResponse, error) {
	if !c.enabled {
		return nil, fmt.Errorf("history client is disabled")
	}

	// build query parameters
	queryParams := map[string]string{
		"device_id": req.DeviceID,
		"agent_id":  req.AgentID,
		"limit":     fmt.Sprintf("%d", req.Limit),
	}
	if req.SessionID != "" {
		queryParams["session_id"] = req.SessionID
	}

	var resp GetMessagesResponse
	err := c.client.DoRequest(ctx, http.RequestOptions{
		Method:      "GET",
		Path:        "/api/internal/history/messages",
		QueryParams: queryParams,
		Response:    &resp,
	})
	if err != nil {
		return nil, err
	}
	return &resp, nil
}
