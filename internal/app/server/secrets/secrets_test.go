package secrets

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestCheckRejectsUnsetAndKnownDefaults(t *testing.T) {
	good := func(t *testing.T) {
		viper.Reset()
		t.Setenv("ENDPOINT_AUTH_TOKEN", "mcp-token")
		viper.Set("websocket.token", "ws-token")
		viper.Set("manager.endpoint_auth_token", "from-yaml")
		viper.Set("mqtt_server.password", "mqtt-pass")
		viper.Set("mqtt_server.enable_auth", false)
		viper.Set("vision.token", "vision-token")
	}

	cases := []struct {
		name string
		mut  func(t *testing.T)
	}{
		{"websocket token empty", func(*testing.T) { viper.Set("websocket.token", "") }},
		{"mcp token empty", func(t *testing.T) { t.Setenv("ENDPOINT_AUTH_TOKEN", "") }},
		{"mcp token default", func(t *testing.T) { t.Setenv("ENDPOINT_AUTH_TOKEN", "xiaozhi_mcp_openclaw_secret_key") }},
		{"mqtt password empty", func(*testing.T) { viper.Set("mqtt_server.password", "") }},
		{"mqtt password default", func(*testing.T) { viper.Set("mqtt_server.password", "test!@#") }},
		{"vision token empty", func(*testing.T) { viper.Set("vision.token", "") }},
		{"vision token default", func(*testing.T) { viper.Set("vision.token", "1234567890") }},
		{"mqtt signature empty when auth on", func(*testing.T) {
			viper.Set("mqtt_server.enable_auth", true)
			viper.Set("mqtt_server.signature_key", "")
		}},
		{"mqtt signature default when auth on", func(*testing.T) {
			viper.Set("mqtt_server.enable_auth", true)
			viper.Set("mqtt_server.signature_key", "your_ota_signature_key_here")
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Cleanup(viper.Reset)
			good(t)
			tc.mut(t)
			if err := Check(); err == nil {
				t.Fatal("Check() = nil, want error")
			}
		})
	}
}

func TestCheckRejectsEndpointTokenThatExistsOnlyInConfig(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("ENDPOINT_AUTH_TOKEN", "")
	viper.Set("websocket.token", "ws-token")
	viper.Set("manager.endpoint_auth_token", "from-yaml")
	viper.Set("mqtt_server.password", "mqtt-pass")
	viper.Set("mqtt_server.enable_auth", false)
	viper.Set("vision.token", "vision-token")
	if err := Check(); err == nil {
		t.Fatal("Check() accepted an endpoint token that is not in the environment")
	}
}

func TestCheckAcceptsNonDefaultSecrets(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("ENDPOINT_AUTH_TOKEN", "mcp-token")
	viper.Set("websocket.token", "ws-token")
	viper.Set("manager.endpoint_auth_token", "from-yaml")
	viper.Set("mqtt_server.password", "mqtt-pass")
	viper.Set("mqtt_server.enable_auth", false)
	viper.Set("vision.token", "vision-token")
	if err := Check(); err != nil {
		t.Fatalf("Check() = %v", err)
	}
}

func TestApplyEnvOverridesEmptyYaml(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("WEBSOCKET_TOKEN", "env-ws")
	t.Setenv("ENDPOINT_AUTH_TOKEN", "env-mcp")
	t.Setenv("MQTT_SERVER_PASSWORD", "env-mqtt")
	t.Setenv("VISION_TOKEN", "env-vision")
	ApplyEnv()
	if err := Check(); err != nil {
		t.Fatalf("Check() = %v", err)
	}
	if got := viper.GetString("websocket.token"); got != "env-ws" {
		t.Fatalf("websocket.token %q", got)
	}
}

func TestShippedConfigHasNoKnownDefaultSecrets(t *testing.T) {
	banned := []string{
		"xiaozhi_mcp_openclaw_secret_key",
		"test!@#",
		"1234567890",
		"your_ota_signature_key_here",
	}
	_, this, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(this), "..", "..", "..", "..")
	files := []string{
		filepath.Join(root, "config/config.yaml"),
		filepath.Join(root, "build/common/main_config.yaml"),
		filepath.Join(root, "docker/test/config.yaml"),
	}
	for _, path := range files {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, s := range banned {
			if strings.Contains(string(body), s) {
				t.Errorf("%s still contains default secret %q", path, s)
			}
		}
	}
}
