package nomemo

import (
	"context"

	"github.com/cloudwego/eino/schema"
)

// NoMemoProvider no-op memory provider
// used when memory is not needed; all methods are no-ops
type NoMemoProvider struct{}

// Get returns the NoMemoProvider instance
func Get() *NoMemoProvider {
	return &NoMemoProvider{}
}

// AddMessage adds a message to memory (no-op)
func (n *NoMemoProvider) AddMessage(ctx context.Context, agentID string, msg schema.Message) error {
	// no-op
	return nil
}

// GetMessages returns user history (no-op)
func (n *NoMemoProvider) GetMessages(ctx context.Context, agentId string, count int) ([]*schema.Message, error) {
	// return an empty message list
	return []*schema.Message{}, nil
}

// GetContext returns user context (no-op)
func (n *NoMemoProvider) GetContext(ctx context.Context, agentId string, maxToken int) (string, error) {
	// return an empty string
	return "", nil
}

// Search searches user memory (no-op)
func (n *NoMemoProvider) Search(ctx context.Context, agentId string, query string, topK int, timeRangeDays int64) (string, error) {
	// return an empty string
	return "", nil
}

// Flush flushes user memory (no-op)
func (n *NoMemoProvider) Flush(ctx context.Context, agentId string) error {
	// no-op
	return nil
}

// ResetMemory resets user memory (no-op)
func (n *NoMemoProvider) ResetMemory(ctx context.Context, agentId string) error {
	// no-op
	return nil
}
