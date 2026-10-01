package chat

import (
	"context"

	config_types "xiaozhi-esp32-server-golang/internal/domain/config/types"
)

// ChatSessionOperator ChatSession ops interface for local MCP tools
// Decouples LLMManager from ChatSession to avoid import cycles
type ChatSessionOperator interface {
	// LocalMcpCloseChat close the chat session
	LocalMcpCloseChat() error

	// LocalMcpClearHistory Clear conversation history
	LocalMcpClearHistory() error

	// LocalMcpPlayMusic Play music
	LocalMcpPlayMusic(ctx context.Context, params *PlayMusicParams) error

	// LocalMcpSwitchDeviceRole switch device role by name (fuzzy match)
	LocalMcpSwitchDeviceRole(ctx context.Context, roleName string) (string, error)

	// LocalMcpRestoreDeviceDefaultRole restore the device default role
	LocalMcpRestoreDeviceDefaultRole(ctx context.Context) error

	// LocalMcpSearchKnowledge search knowledge bases linked to the current agent
	LocalMcpSearchKnowledge(ctx context.Context, query string, topK int, knowledgeBaseIDs []uint) ([]config_types.KnowledgeSearchHit, error)

	// LocalMcpControlMusicPlayback control session-scoped media playback
	LocalMcpControlMusicPlayback(ctx context.Context, params *MusicPlaybackControlParams) (*MusicPlaybackControlResult, error)

	// More ops can be added later
	// GetDeviceID() string
	// IsActive() bool
}
