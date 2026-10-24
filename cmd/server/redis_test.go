package main

import (
	"testing"
	"time"

	redisdb "xiaozhi-esp32-server-golang/internal/db/redis"

	"github.com/spf13/viper"
)

func TestInitRedisDoesNotConnectWhenDisabled(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("redis.enable", false)
	viper.Set("redis.host", "127.0.0.1")
	viper.Set("redis.port", 1)

	start := time.Now()
	if err := initRedis(); err != nil {
		t.Fatalf("initRedis: %v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("disabled Redis tried to connect")
	}
	if redisdb.GetClient() != nil {
		t.Fatal("Redis client started while redis.enable is false")
	}
}

func TestRedisAvailableSettingDoesNotChangeTheRedisClient(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("redis.enable", false)
	viper.Set("redis.host", "127.0.0.1")
	viper.Set("config_provider.type", "pocketbase")

	ApplySystemConfigToViper(map[string]interface{}{
		"redis_available": map[string]interface{}{"enabled": true},
	})

	if viper.GetBool("redis.enable") {
		t.Fatal("the console flag turned Redis on")
	}
	if got := viper.GetString("redis.host"); got != "127.0.0.1" {
		t.Fatalf("redis.host = %q", got)
	}
	if got := viper.GetString("config_provider.type"); got != "pocketbase" {
		t.Fatalf("config_provider.type = %q", got)
	}
}
