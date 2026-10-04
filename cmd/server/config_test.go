package main

import (
	"testing"

	"xiaozhi-esp32-server-golang/internal/app/server/secrets"

	"github.com/spf13/viper"
)

func TestSettingsCannotOverrideEnvSecret(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("MQTT_SERVER_PASSWORD", "from-env")
	t.Setenv("VISION_TOKEN", "from-env-vision")
	secrets.ApplyEnv()

	ApplySystemConfigToViper(map[string]interface{}{
		"mqtt_server": map[string]interface{}{
			"password":    "from-settings",
			"listen_port": 9,
		},
		"vision": map[string]interface{}{
			"token":      "from-settings",
			"vision_url": "http://new/vision",
		},
	})

	if got := viper.GetString("mqtt_server.password"); got != "from-env" {
		t.Fatalf("mqtt_server.password = %q", got)
	}
	if got := viper.GetInt("mqtt_server.listen_port"); got != 9 {
		t.Fatalf("listen_port = %d", got)
	}
	if got := viper.GetString("vision.token"); got != "from-env-vision" {
		t.Fatalf("vision.token = %q", got)
	}
	if got := viper.GetString("vision.vision_url"); got != "http://new/vision" {
		t.Fatalf("vision_url = %q", got)
	}
}
