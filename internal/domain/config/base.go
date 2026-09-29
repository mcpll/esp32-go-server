package user_config

import (
	"fmt"

	"xiaozhi-esp32-server-golang/internal/domain/config/manager"
	userconfig_redis "xiaozhi-esp32-server-golang/internal/domain/config/redis"
	"xiaozhi-esp32-server-golang/internal/util"
)

// Config user config provider settings
type Config struct {
	Type       string                 `json:"type"`       // storage type: "redis", "memory", "file"
	Parameters map[string]interface{} `json:"parameters"` // storage-related config params
}

func GetProvider(sType string) (UserConfigProvider, error) {
	config := make(map[string]interface{})
	if sType == "manager" {
		// prefer backend address from env; fall back to config
		backendUrl := util.GetBackendURL()
		config = map[string]interface{}{
			"backend_url": backendUrl,
			"auth_token":  util.GetManagerAuthToken(),
		}
	}

	provider, err := GetUserConfigProvider(sType, config)
	if err != nil {
		return nil, err
	}
	return provider, nil
}

// GetUserConfigProvider creates a user config provider
// creates the provider for the given storage type and config
// providerType: provider type; supports "redis", "memory", "file"
// config: provider config params
// returns a UserConfigProvider with full CRUD
func GetUserConfigProvider(providerType string, config map[string]interface{}) (UserConfigProvider, error) {
	if config == nil {
		config = make(map[string]interface{})
	}

	switch providerType {
	case "redis":
		// create a Redis user config provider
		provider, err := userconfig_redis.NewRedisUserConfigProvider(config)
		if err != nil {
			return nil, fmt.Errorf("创建Redis用户配置提供者失败: %v", err)
		}
		return provider, nil
	case "manager":
		// create a backend-manager user config provider
		provider, err := manager.NewManagerUserConfigProvider(config)
		if err != nil {
			return nil, fmt.Errorf("创建后端管理系统用户配置提供者失败: %v", err)
		}
		return provider, nil
	default:
		return nil, fmt.Errorf("不支持的用户配置提供者: %s", providerType)
	}
}
