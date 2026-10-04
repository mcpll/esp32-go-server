package store

import (
	"sync"
	"testing"

	"github.com/spf13/viper"
)

func TestMergeDropsSecretsFromSettings(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
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

func TestMergeWhileReadersRun(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

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
