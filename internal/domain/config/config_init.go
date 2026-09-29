package user_config

import (
	"context"
	"fmt"
	log "xiaozhi-esp32-server-golang/logger"

	"xiaozhi-esp32-server-golang/internal/domain/config/manager"
	"xiaozhi-esp32-server-golang/internal/domain/config/memory"
	redis_config "xiaozhi-esp32-server-golang/internal/domain/config/redis"

	"github.com/spf13/viper"
)

var (
	// managerSystemConfigHandlers callbacks on WebSocket system_config push; main may register multiple (merge to viper, hot-reload services)
	managerSystemConfigHandlers []func(map[string]interface{})
)

// RegisterManagerSystemConfigHandler registers system-config push callbacks in manager mode; call before InitConfigSystem; may append multiple
func RegisterManagerSystemConfigHandler(fn func(map[string]interface{})) {
	managerSystemConfigHandlers = append(managerSystemConfigHandlers, fn)
}

// InitConfigSystem initializes the config system
// Calls Init on the config package matching config_provider.type
func InitConfigSystem(ctx context.Context) error {
	// Get config provider type
	providerType := viper.GetString("config_provider.type")
	if providerType == "" {
		providerType = "redis" // default to redis
		log.Infof("config_provider.type not set, using default: redis")
	}

	log.Infof("Initializing config system with provider: %s", providerType)

	// Call Init for the selected provider type
	switch providerType {
	case "manager":
		manager.SetSystemConfigPushHandler(func(data map[string]interface{}) {
			for _, h := range managerSystemConfigHandlers {
				h(data)
			}
		})
		return manager.Init(ctx)
	case "redis":
		return redis_config.Init(ctx)
	case "memory":
		return memory.Init(ctx)
	default:
		return fmt.Errorf("unsupported config provider type: %s", providerType)
	}
}
