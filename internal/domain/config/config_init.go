package user_config

import (
	"context"
	"fmt"
	"strings"
	"sync"

	log "xiaozhi-esp32-server-golang/logger"

	"github.com/spf13/viper"
)

var (
	systemConfigMu       sync.RWMutex
	systemConfigHandlers []func(map[string]interface{})
)

// RegisterSystemConfigHandler registers a callback for system config (the settings blocks) that
// arrives after startup, for example when PocketBase answers late. Several handlers may be registered.
func RegisterSystemConfigHandler(fn func(map[string]interface{})) {
	systemConfigMu.Lock()
	defer systemConfigMu.Unlock()
	systemConfigHandlers = append(systemConfigHandlers, fn)
}

// DispatchSystemConfig hands system config to the registered handlers.
// It reports false when there is none, so the caller can merge the config itself.
func DispatchSystemConfig(data map[string]interface{}) bool {
	systemConfigMu.RLock()
	handlers := append([]func(map[string]interface{}){}, systemConfigHandlers...)
	systemConfigMu.RUnlock()
	for _, h := range handlers {
		h(data)
	}
	return len(handlers) > 0
}

// InitConfigSystem starts the config provider selected by config_provider.type.
// onSystemConfig receives the system config: before this returns when PocketBase answers,
// otherwise from a background retry once it does. The server starts either way.
func InitConfigSystem(ctx context.Context, onSystemConfig func(map[string]interface{})) error {
	providerType := strings.TrimSpace(viper.GetString("config_provider.type"))
	if providerType == "" {
		providerType = ProviderPocketBase
		log.Infof("config_provider.type not set, using default: %s", providerType)
	}
	log.Infof("Initializing config system with provider: %s", providerType)

	switch providerType {
	case ProviderPocketBase:
		provider, err := sharedPocketBase()
		if err != nil {
			return err
		}
		provider.Start(ctx, onSystemConfig)
		return nil
	default:
		return fmt.Errorf("unsupported config provider type: %s", providerType)
	}
}
