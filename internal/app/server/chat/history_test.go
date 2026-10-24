package chat

import (
	"context"
	"testing"

	. "xiaozhi-esp32-server-golang/internal/data/client"
	redisdb "xiaozhi-esp32-server-golang/internal/db/redis"
	"xiaozhi-esp32-server-golang/internal/domain/config/types"
	"xiaozhi-esp32-server-golang/internal/domain/memory/llm_memory"

	"github.com/alicebob/miniredis/v2"
	"github.com/cloudwego/eino/schema"
	goredis "github.com/redis/go-redis/v9"
	"github.com/spf13/viper"
)

func TestHistoryLoadFollowsShortMemoryNotTheProviderType(t *testing.T) {
	ctx := context.Background()
	useMiniRedis(t)
	viper.Set("redis.enable", true)
	viper.Set("redis.key_prefix", "xiaozhi")
	viper.Set("config_provider.type", "manager")

	if err := llm_memory.Get().AddMessage(ctx, "dev-1", "agent-1", *schema.UserMessage("ciao dal passato")); err != nil {
		t.Fatal(err)
	}

	session := historySession(ctx, "dev-1", MemoryModeShort)
	if err := session.initHistoryMessages(); err != nil {
		t.Fatal(err)
	}
	messages := session.clientState.GetMessages(20)
	if len(messages) != 1 || messages[0].Content != "ciao dal passato" {
		t.Fatalf("history = %+v", messages)
	}
}

func TestHistoryLoadSkipsLongAndNoneEvenWhenTheProviderIsRedis(t *testing.T) {
	ctx := context.Background()
	useMiniRedis(t)
	viper.Set("redis.enable", true)
	viper.Set("redis.key_prefix", "xiaozhi")
	viper.Set("config_provider.type", "redis")

	if err := llm_memory.Get().AddMessage(ctx, "dev-1", "agent-1", *schema.UserMessage("non caricare")); err != nil {
		t.Fatal(err)
	}

	for _, mode := range []string{MemoryModeNone, MemoryModeLong} {
		session := historySession(ctx, "dev-1", mode)
		if err := session.initHistoryMessages(); err != nil {
			t.Fatal(err)
		}
		if messages := session.clientState.GetMessages(20); len(messages) != 0 {
			t.Fatalf("mode %s loaded %+v", mode, messages)
		}
	}
}

func TestHistoryLoadSkipsShortWhenRedisIsOff(t *testing.T) {
	ctx := context.Background()
	useMiniRedis(t)
	viper.Set("redis.enable", false)
	viper.Set("redis.key_prefix", "xiaozhi")
	viper.Set("config_provider.type", "redis")

	if err := llm_memory.Get().AddMessage(ctx, "dev-1", "agent-1", *schema.UserMessage("non caricare")); err != nil {
		t.Fatal(err)
	}

	session := historySession(ctx, "dev-1", MemoryModeShort)
	if err := session.initHistoryMessages(); err != nil {
		t.Fatal(err)
	}
	if messages := session.clientState.GetMessages(20); len(messages) != 0 {
		t.Fatalf("short without Redis loaded %+v", messages)
	}
}

func historySession(ctx context.Context, deviceID, mode string) *ChatSession {
	return &ChatSession{
		ctx: ctx,
		clientState: &ClientState{
			Dialogue: &Dialogue{},
			DeviceID: deviceID,
			AgentID:  "agent-1",
			DeviceConfig: types.UConfig{
				MemoryMode: mode,
			},
		},
	}
}

func useMiniRedis(t *testing.T) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	redisdb.SetClientForTest(client)
	llm_memory.ResetForTest()
	viper.Reset()
	t.Cleanup(func() {
		redisdb.SetClientForTest(nil)
		llm_memory.ResetForTest()
		viper.Reset()
	})
}
