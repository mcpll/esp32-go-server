package secrets

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestCheckRejectsUnsetAndKnownDefaults(t *testing.T) {
	good := func() {
		viper.Reset()
		viper.Set("websocket.token", "ws-token")
		viper.Set("manager.endpoint_auth_token", "mcp-token")
		viper.Set("mqtt_server.password", "mqtt-pass")
		viper.Set("mqtt_server.enable_auth", false)
		viper.Set("vision.token", "vision-token")
	}

	cases := []struct {
		name string
		mut  func()
	}{
		{"websocket token empty", func() { viper.Set("websocket.token", "") }},
		{"mcp token empty", func() { viper.Set("manager.endpoint_auth_token", "") }},
		{"mcp token default", func() { viper.Set("manager.endpoint_auth_token", "xiaozhi_mcp_openclaw_secret_key") }},
		{"mqtt password empty", func() { viper.Set("mqtt_server.password", "") }},
		{"mqtt password default", func() { viper.Set("mqtt_server.password", "test!@#") }},
		{"vision token empty", func() { viper.Set("vision.token", "") }},
		{"vision token default", func() { viper.Set("vision.token", "1234567890") }},
		{"mqtt signature empty when auth on", func() {
			viper.Set("mqtt_server.enable_auth", true)
			viper.Set("mqtt_server.signature_key", "")
		}},
		{"mqtt signature default when auth on", func() {
			viper.Set("mqtt_server.enable_auth", true)
			viper.Set("mqtt_server.signature_key", "your_ota_signature_key_here")
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Cleanup(viper.Reset)
			good()
			tc.mut()
			if err := Check(); err == nil {
				t.Fatal("Check() = nil, want error")
			}
		})
	}
}

func TestCheckAcceptsNonDefaultSecrets(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("websocket.token", "ws-token")
	viper.Set("manager.endpoint_auth_token", "mcp-token")
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

func TestApplyEnvFillsItalianProviderKeysFromDashScope(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("asr.aliyun_qwen3.api_key", "")
	viper.Set("llm.aliyun.api_key", "")
	viper.Set("tts.aliyun_qwen.api_key", "")
	t.Setenv("DASHSCOPE_API_KEY", "dash-key")
	ApplyEnv()
	if got := viper.GetString("asr.aliyun_qwen3.api_key"); got != "dash-key" {
		t.Fatalf("asr api_key %q", got)
	}
	if got := viper.GetString("llm.aliyun.api_key"); got != "dash-key" {
		t.Fatalf("llm api_key %q", got)
	}
	if got := viper.GetString("tts.aliyun_qwen.api_key"); got != "dash-key" {
		t.Fatalf("tts api_key %q", got)
	}
}

func TestApplyEnvLeavesProviderKeysWhenDashScopeUnset(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("asr.aliyun_qwen3.api_key", "from-file")
	t.Setenv("DASHSCOPE_API_KEY", "")
	ApplyEnv()
	if got := viper.GetString("asr.aliyun_qwen3.api_key"); got != "from-file" {
		t.Fatalf("asr api_key %q", got)
	}
}

func TestApplyEnvFillsRedisAndMem0WhenSet(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("redis.enable", false)
	viper.Set("redis.host", "127.0.0.1")
	viper.Set("redis.port", 6379)
	viper.Set("redis.password", "ticket_dev")
	viper.Set("memory.mem0.api_key", "your_mem0_api_key_here")
	t.Setenv("REDIS_ENABLE", "true")
	t.Setenv("REDIS_HOST", "redis")
	t.Setenv("REDIS_PORT", "6380")
	t.Setenv("REDIS_PASSWORD", "redis-pass")
	t.Setenv("MEM0_API_KEY", "mem-key")
	ApplyEnv()
	if !viper.GetBool("redis.enable") {
		t.Fatal("redis.enable = false")
	}
	if got := viper.GetString("redis.host"); got != "redis" {
		t.Fatalf("redis.host %q", got)
	}
	if got := viper.GetInt("redis.port"); got != 6380 {
		t.Fatalf("redis.port %d", got)
	}
	if got := viper.GetString("redis.password"); got != "redis-pass" {
		t.Fatalf("redis.password %q", got)
	}
	if got := viper.GetString("memory.mem0.api_key"); got != "mem-key" {
		t.Fatalf("mem0 api_key %q", got)
	}
}

func TestApplyEnvFillsKeysLoadedFromYAML(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.SetConfigType("yaml")
	body := []byte(`
asr:
  aliyun_qwen3:
    api_key: ""
llm:
  aliyun:
    api_key: ""
tts:
  aliyun_qwen:
    api_key: ""
redis:
  enable: false
  host: "127.0.0.1"
  port: 6379
  password: "ticket_dev"
memory:
  mem0:
    api_key: "your_mem0_api_key_here"
`)
	if err := viper.ReadConfig(bytes.NewReader(body)); err != nil {
		t.Fatalf("ReadConfig: %v", err)
	}
	t.Setenv("DASHSCOPE_API_KEY", "dash-key")
	t.Setenv("REDIS_ENABLE", "true")
	t.Setenv("REDIS_HOST", "redis")
	t.Setenv("MEM0_API_KEY", "mem-key")
	ApplyEnv()
	if got := viper.GetString("asr.aliyun_qwen3.api_key"); got != "dash-key" {
		t.Fatalf("asr api_key %q", got)
	}
	if got := viper.GetString("llm.aliyun.api_key"); got != "dash-key" {
		t.Fatalf("llm api_key %q", got)
	}
	if got := viper.GetString("tts.aliyun_qwen.api_key"); got != "dash-key" {
		t.Fatalf("tts api_key %q", got)
	}
	if !viper.GetBool("redis.enable") || viper.GetString("redis.host") != "redis" {
		t.Fatalf("redis enable=%v host=%q", viper.GetBool("redis.enable"), viper.GetString("redis.host"))
	}
	if got := viper.GetString("memory.mem0.api_key"); got != "mem-key" {
		t.Fatalf("mem0 api_key %q", got)
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
