package memory

import (
	"context"
	"fmt"
	"sync"

	"xiaozhi-esp32-server-golang/internal/domain/config/types"
	log "xiaozhi-esp32-server-golang/logger"
)

// MemoryUserConfigProvider in-memory user config provider
// Implements UserConfigProvider; stores config in memory
// Note: data is lost on restart; for tests or temporary storage
type MemoryUserConfigProvider struct {
	mu         sync.RWMutex
	configs    map[string]types.UConfig
	maxEntries int
}

// MemoryConfig in-memory config struct
type MemoryConfig struct {
	MaxEntries int `json:"max_entries"` // max stored entries
}

// NewMemoryUserConfigProvider creates an in-memory user config provider
// config: parameter map including max_entries etc.
func NewMemoryUserConfigProvider(config map[string]interface{}) (*MemoryUserConfigProvider, error) {
	// Parse config parameters
	memoryConfig := &MemoryConfig{
		MaxEntries: 1000, // default max 1000 configs
	}

	if maxEntries, ok := config["max_entries"].(int); ok && maxEntries > 0 {
		memoryConfig.MaxEntries = maxEntries
	} else if maxEntriesFloat, ok := config["max_entries"].(float64); ok && maxEntriesFloat > 0 {
		memoryConfig.MaxEntries = int(maxEntriesFloat)
	}

	provider := &MemoryUserConfigProvider{
		configs:    make(map[string]types.UConfig),
		maxEntries: memoryConfig.MaxEntries,
	}

	log.Log().Infof("memory user config provider initialized, max entries: %d", memoryConfig.MaxEntries)
	return provider, nil
}

// GetUserConfig returns user config
func (m *MemoryUserConfigProvider) GetUserConfig(ctx context.Context, userID string) (types.UConfig, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	config, exists := m.configs[userID]
	if !exists {
		log.Log().Debugf("user %s config not found, returning empty config", userID)
		return types.UConfig{}, nil
	}

	return config, nil
}

// SetUserConfig sets user config
func (m *MemoryUserConfigProvider) SetUserConfig(ctx context.Context, userID string, config types.UConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check whether max entries exceeded
	if len(m.configs) >= m.maxEntries && !m.configExists(userID) {
		return fmt.Errorf("已达到最大存储条目数 %d，无法添加新配置", m.maxEntries)
	}

	m.configs[userID] = config
	log.Log().Infof("user %s config set successfully (memory)", userID)
	return nil
}

// DeleteUserConfig deletes user config
func (m *MemoryUserConfigProvider) DeleteUserConfig(ctx context.Context, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.configs[userID]; !exists {
		log.Log().Warnf("user %s config not found, nothing to delete", userID)
		return nil
	}

	delete(m.configs, userID)
	log.Log().Infof("user %s config deleted successfully (memory)", userID)
	return nil
}

// Close closes the provider (in-memory needs no special cleanup)
func (m *MemoryUserConfigProvider) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Clear all configs
	m.configs = make(map[string]types.UConfig)
	log.Log().Info("memory user config provider closed, all configs cleared")
	return nil
}

// configExists checks whether config exists (caller must hold lock)
func (m *MemoryUserConfigProvider) configExists(userID string) bool {
	_, exists := m.configs[userID]
	return exists
}

// GetStats returns storage stats (extra helper)
func (m *MemoryUserConfigProvider) GetStats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return map[string]interface{}{
		"total_configs": len(m.configs),
		"max_entries":   m.maxEntries,
		"usage_percent": float64(len(m.configs)) / float64(m.maxEntries) * 100,
	}
}

// ListUserIDs lists all user IDs (extra helper)
func (m *MemoryUserConfigProvider) ListUserIDs() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	userIDs := make([]string, 0, len(m.configs))
	for userID := range m.configs {
		userIDs = append(userIDs, userID)
	}
	return userIDs
}

// GetSystemConfig returns system config
func (m *MemoryUserConfigProvider) GetSystemConfig(ctx context.Context) (string, error) {
	// In-memory provider does not supply system config
	return "", nil
}

// Init initializes the Memory config provider
func Init(ctx context.Context) error {
	log.Log().Info("Memory config provider initialized successfully")
	return nil
}

// Close shuts down the Memory config provider
func Close() error {
	log.Log().Info("Memory config provider closed")
	return nil
}

// IsConnected reports whether the Memory provider is connected
func IsConnected() bool {
	// In-memory provider is always "connected"
	return true
}
