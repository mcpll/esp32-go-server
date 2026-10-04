package secrets

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/viper"
)

const (
	defaultMCPToken         = "xiaozhi_mcp_openclaw_secret_key"
	defaultMQTTPassword     = "test!@#"
	defaultMQTTSignatureKey = "your_ota_signature_key_here"
	defaultVisionToken      = "1234567890"
)

func ApplyEnv() {
	setFromEnv("WEBSOCKET_TOKEN", "websocket.token")
	setFromEnv("ENDPOINT_AUTH_TOKEN", "manager.endpoint_auth_token")
	setFromEnv("MQTT_SERVER_PASSWORD", "mqtt_server.password")
	setFromEnv("MQTT_SERVER_SIGNATURE_KEY", "mqtt_server.signature_key")
	setFromEnv("VISION_TOKEN", "vision.token")
}

func setFromEnv(env, key string) {
	if v := strings.TrimSpace(os.Getenv(env)); v != "" {
		viper.Set(key, v)
	}
}

func Check() error {
	if err := requireSecret("websocket.token"); err != nil {
		return err
	}
	if err := requireSecret("manager.endpoint_auth_token", defaultMCPToken); err != nil {
		return err
	}
	if err := requireSecret("mqtt_server.password", defaultMQTTPassword); err != nil {
		return err
	}
	if err := requireSecret("vision.token", defaultVisionToken); err != nil {
		return err
	}
	if viper.GetBool("mqtt_server.enable_auth") {
		if err := requireSecret("mqtt_server.signature_key", defaultMQTTSignatureKey); err != nil {
			return err
		}
	}
	return nil
}

func requireSecret(key string, banned ...string) error {
	v := strings.TrimSpace(viper.GetString(key))
	if v == "" {
		return fmt.Errorf("%s is unset", key)
	}
	for _, b := range banned {
		if v == b {
			return fmt.Errorf("%s uses a known default secret", key)
		}
	}
	return nil
}
