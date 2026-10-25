package secrets

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"xiaozhi-esp32-server-golang/internal/util"

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
	// Always replace the config value, including with empty, so a file or a settings
	// record cannot keep a token the server will not use.
	viper.Set("manager.endpoint_auth_token", util.GetManagerEndpointAuthToken())
	setFromEnv("MQTT_SERVER_PASSWORD", "mqtt_server.password")
	setFromEnv("MQTT_SERVER_SIGNATURE_KEY", "mqtt_server.signature_key")
	setFromEnv("VISION_TOKEN", "vision.token")
	applyDashScope()
	applyOpenRouter()
	applyGroq()
	applyMem0()
	applyRedis()
}

// applyDashScope copies DASHSCOPE_API_KEY onto the seeded Italian providers
// (ASR, the aliyun LLM, and aliyun_qwen TTS). An empty variable leaves the file values.
func applyDashScope() {
	key := strings.TrimSpace(os.Getenv("DASHSCOPE_API_KEY"))
	if key == "" {
		return
	}
	viper.Set("asr.aliyun_qwen3.api_key", key)
	viper.Set("llm.aliyun.api_key", key)
	viper.Set("tts.aliyun_qwen.api_key", key)
}

// applyOpenRouter copies OPENROUTER_API_KEY onto llm.openrouter when set.
func applyOpenRouter() {
	setFromEnv("OPENROUTER_API_KEY", "llm.openrouter.api_key")
}

// applyGroq copies GROQ_API_KEY onto asr.groq_whisper when set.
func applyGroq() {
	setFromEnv("GROQ_API_KEY", "asr.groq_whisper.api_key")
}

func applyMem0() {
	setFromEnv("MEM0_API_KEY", "memory.mem0.api_key")
}

func applyRedis() {
	if v := strings.TrimSpace(os.Getenv("REDIS_ENABLE")); v != "" {
		viper.Set("redis.enable", envBool(v))
	}
	setFromEnv("REDIS_HOST", "redis.host")
	setFromEnv("REDIS_PASSWORD", "redis.password")
	if v := strings.TrimSpace(os.Getenv("REDIS_PORT")); v != "" {
		port, err := strconv.Atoi(v)
		if err == nil && port > 0 {
			viper.Set("redis.port", port)
		}
	}
}

func envBool(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
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
	if err := requireValue("ENDPOINT_AUTH_TOKEN", util.GetManagerEndpointAuthToken(), defaultMCPToken); err != nil {
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
	return requireValue(key, viper.GetString(key), banned...)
}

func requireValue(name, value string, banned ...string) error {
	v := strings.TrimSpace(value)
	if v == "" {
		return fmt.Errorf("%s is unset", name)
	}
	for _, b := range banned {
		if v == b {
			return fmt.Errorf("%s uses a known default secret", name)
		}
	}
	return nil
}
