package store

import (
	"sync"
	"testing"

	"github.com/spf13/viper"
)

func resetViper(t *testing.T) {
	t.Helper()
	mu.Lock()
	applied = nil
	mu.Unlock()
	viper.Reset()
	t.Cleanup(func() {
		mu.Lock()
		applied = nil
		mu.Unlock()
		viper.Reset()
	})
}

func TestMergeDropsSecretsFromSettings(t *testing.T) {
	resetViper(t)
	viper.Set("mqtt_server.password", "from-yaml")
	viper.Set("asr.aliyun_qwen3.api_key", "real-key")
	viper.Set("ota.signature_key", "sig")

	err := Merge(map[string]any{
		"mqtt_server": map[string]any{"password": "from-settings", "listen_port": 9},
		"asr":         map[string]any{"aliyun_qwen3": map[string]any{"api_key": "stolen", "model": "m"}},
		"ota": map[string]any{
			"signature_key": "stolen",
			"test":          map[string]any{"websocket": map[string]any{"url": "ws://new/"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := viper.GetString("mqtt_server.password"); got != "from-yaml" {
		t.Fatalf("password = %q", got)
	}
	if got := viper.GetString("asr.aliyun_qwen3.api_key"); got != "real-key" {
		t.Fatalf("api_key = %q", got)
	}
	if got := viper.GetString("asr.aliyun_qwen3.model"); got != "m" {
		t.Fatalf("model = %q", got)
	}
	if got := viper.GetString("ota.signature_key"); got != "sig" {
		t.Fatalf("signature_key = %q", got)
	}
	if got := viper.GetString("ota.test.websocket.url"); got != "ws://new/" {
		t.Fatalf("ota url = %q", got)
	}
	if got := viper.GetInt("mqtt_server.listen_port"); got != 9 {
		t.Fatalf("listen_port = %d", got)
	}
}

func TestEmptySeedNeverOverridesFile(t *testing.T) {
	resetViper(t)
	viper.Set("ota.test.websocket.url", "ws://from-file/xiaozhi/v1/")
	seed := map[string]any{
		"ota": map[string]any{
			"test": map[string]any{
				"websocket": map[string]any{"url": ""},
				"mqtt":      map[string]any{"enable": false, "endpoint": ""},
			},
		},
	}
	if err := Merge(seed); err != nil {
		t.Fatal(err)
	}
	if err := Merge(seed); err != nil {
		t.Fatal(err)
	}
	if got := viper.GetString("ota.test.websocket.url"); got != "ws://from-file/xiaozhi/v1/" {
		t.Fatalf("url = %q", got)
	}
	if !viper.IsSet("ota.test.mqtt.enable") || viper.GetBool("ota.test.mqtt.enable") {
		t.Fatalf("enable = %v, set=%v", viper.GetBool("ota.test.mqtt.enable"), viper.IsSet("ota.test.mqtt.enable"))
	}

	if err := Merge(map[string]any{
		"ota": map[string]any{"test": map[string]any{"websocket": map[string]any{"url": "ws://from-settings/xiaozhi/v1/"}}},
	}); err != nil {
		t.Fatal(err)
	}
	if got := viper.GetString("ota.test.websocket.url"); got != "ws://from-settings/xiaozhi/v1/" {
		t.Fatalf("explicit url = %q", got)
	}
}

func TestClearedSettingDropsTheOldValue(t *testing.T) {
	resetViper(t)
	viper.Set("vad.silero_vad.model_path", "config/models/vad/silero_vad.onnx")
	viper.Set("mqtt_server.password", "from-yaml")
	viper.Set("ota.signature_key", "sig")

	first := map[string]any{
		"ota":  map[string]any{"test": map[string]any{"websocket": map[string]any{"url": "ws://old/"}}},
		"vad":  map[string]any{"provider": "silero_vad"},
		"mqtt": map[string]any{"password": "client-secret", "broker": "127.0.0.1"},
	}
	if err := Merge(first); err != nil {
		t.Fatal(err)
	}
	if got := viper.GetString("mqtt.password"); got != "client-secret" {
		t.Fatalf("mqtt.password = %q", got)
	}

	if err := Merge(map[string]any{
		"ota":         map[string]any{"test": map[string]any{"websocket": map[string]any{}}},
		"vad":         map[string]any{"provider": "silero_vad"},
		"mqtt":        map[string]any{"broker": "127.0.0.1"},
		"mqtt_server": map[string]any{"password": "stolen", "listen_port": 9},
	}); err != nil {
		t.Fatal(err)
	}
	if got := viper.GetString("ota.test.websocket.url"); got != "" {
		t.Fatalf("url = %q", got)
	}
	if got := viper.GetString("ota.signature_key"); got != "sig" {
		t.Fatalf("signature = %q", got)
	}
	if got := viper.GetString("vad.silero_vad.model_path"); got != "config/models/vad/silero_vad.onnx" {
		t.Fatalf("model = %q", got)
	}
	if got := viper.GetString("vad.provider"); got != "silero_vad" {
		t.Fatalf("provider = %q", got)
	}
	if got := viper.GetString("mqtt.password"); got != "" {
		t.Fatalf("mqtt.password = %q", got)
	}
	if got := viper.GetString("mqtt.broker"); got != "127.0.0.1" {
		t.Fatalf("broker = %q", got)
	}
	if got := viper.GetString("mqtt_server.password"); got != "from-yaml" {
		t.Fatalf("broker password = %q", got)
	}
	if got := viper.GetInt("mqtt_server.listen_port"); got != 9 {
		t.Fatalf("listen_port = %d", got)
	}
}

func TestMergeWhileReadersRun(t *testing.T) {
	resetViper(t)

	if err := Merge(map[string]any{"ota": map[string]any{"url": "http://a"}}); err != nil {
		t.Fatal(err)
	}

	const readers = 8
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(readers)
	for i := 0; i < readers; i++ {
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_ = GetString("ota.url")
					_ = GetBool("auth.enable")
					_ = GetStringMap("ota")
					_ = AllSettings()
				}
			}
		}()
	}

	for i := 0; i < 50; i++ {
		if err := Merge(map[string]any{"ota": map[string]any{"url": "http://b"}}); err != nil {
			t.Fatal(err)
		}
	}
	close(stop)
	wg.Wait()

	if got := GetString("ota.url"); got != "http://b" {
		t.Fatalf("ota.url = %q, want http://b", got)
	}
}
