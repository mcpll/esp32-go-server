package client

import (
	"testing"

	utypes "xiaozhi-esp32-server-golang/internal/domain/config/types"

	"github.com/spf13/viper"
)

func TestShortMemoryIsNoneWhenRedisIsOff(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("redis.enable", false)
	viper.Set("config_provider.type", "redis")

	state := &ClientState{DeviceConfig: utypes.UConfig{MemoryMode: MemoryModeShort}}
	if got := state.GetMemoryMode(); got != MemoryModeNone {
		t.Fatalf("memory mode = %q, want none", got)
	}
}

func TestShortMemoryStaysWhenRedisIsOnWhateverTheProvider(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("redis.enable", true)
	viper.Set("config_provider.type", "pocketbase")

	state := &ClientState{DeviceConfig: utypes.UConfig{MemoryMode: MemoryModeShort}}
	if got := state.GetMemoryMode(); got != MemoryModeShort {
		t.Fatalf("memory mode = %q, want short", got)
	}
}

func TestLongMemoryDoesNotNeedRedis(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("redis.enable", false)
	viper.Set("config_provider.type", "manager")

	state := &ClientState{DeviceConfig: utypes.UConfig{MemoryMode: MemoryModeLong}}
	if got := state.GetMemoryMode(); got != MemoryModeLong {
		t.Fatalf("memory mode = %q, want long", got)
	}
}
