package server

import (
	"context"
	"testing"

	data_client "xiaozhi-esp32-server-golang/internal/data/client"
	redisdb "xiaozhi-esp32-server-golang/internal/db/redis"
	utypes "xiaozhi-esp32-server-golang/internal/domain/config/types"
	"xiaozhi-esp32-server-golang/internal/domain/eventbus"
	"xiaozhi-esp32-server-golang/internal/domain/memory"
	"xiaozhi-esp32-server-golang/internal/domain/memory/llm_memory"

	"github.com/alicebob/miniredis/v2"
	"github.com/cloudwego/eino/schema"
	goredis "github.com/redis/go-redis/v9"
	"github.com/spf13/viper"
)

func TestSaveShortMemoryFollowsShortModeNotTheProviderType(t *testing.T) {
	ctx := context.Background()
	mr := useMiniRedis(t)
	viper.Set("redis.enable", true)
	viper.Set("redis.key_prefix", "xiaozhi")
	viper.Set("config_provider.type", "pocketbase")

	saveShort(ctx, data_client.MemoryModeShort, "dev-1", "agent-1", "ricordatemi")

	messages, err := llm_memory.Get().GetMessages(ctx, "dev-1", "agent-1", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].Content != "ricordatemi" {
		t.Fatalf("saved = %+v", messages)
	}
	if keys := mr.Keys(); len(keys) == 0 {
		t.Fatal("short memory wrote nothing")
	}
}

func TestSaveShortMemorySkipsLongNoneAndShortWithoutRedis(t *testing.T) {
	ctx := context.Background()
	mr := useMiniRedis(t)
	viper.Set("redis.key_prefix", "xiaozhi")
	viper.Set("config_provider.type", "redis")

	viper.Set("redis.enable", true)
	saveShort(ctx, data_client.MemoryModeLong, "dev-long", "agent-1", "lunga")
	saveShort(ctx, data_client.MemoryModeNone, "dev-none", "agent-1", "niente")

	viper.Set("redis.enable", false)
	saveShort(ctx, data_client.MemoryModeShort, "dev-off", "agent-1", "spenta")

	if keys := mr.Keys(); len(keys) != 0 {
		t.Fatalf("redis keys = %v", keys)
	}
}

func TestLongMemoryStoresUnderTheAgentOrDeviceKey(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("redis.enable", false)
	viper.Set("config_provider.type", "pocketbase")

	withAgent := &recordingMemory{}
	saveLong(withAgent, "dev-1", "agent-9", "mi chiamo Matteo")
	if len(withAgent.added) != 1 || withAgent.added[0].id != "agent-9" || withAgent.added[0].content != "mi chiamo Matteo" {
		t.Fatalf("stored = %+v", withAgent.added)
	}

	deviceOnly := &recordingMemory{}
	saveLong(deviceOnly, "dev-1", "", "solo dispositivo")
	if len(deviceOnly.added) != 1 || deviceOnly.added[0].id != "dev-1" {
		t.Fatalf("stored = %+v", deviceOnly.added)
	}

	short := &recordingMemory{}
	viper.Set("redis.enable", true)
	worker := &MessageWorker{}
	worker.processMemoryProvider(&eventbus.AddMessageEvent{
		ClientState: &data_client.ClientState{
			Ctx:            context.Background(),
			DeviceID:       "dev-1",
			AgentID:        "agent-9",
			MemoryProvider: short,
			DeviceConfig:   utypes.UConfig{MemoryMode: data_client.MemoryModeShort},
		},
		Msg: *schema.UserMessage("non in mem0"),
	})
	if len(short.added) != 0 {
		t.Fatalf("short stored in mem0: %+v", short.added)
	}
}

func saveShort(ctx context.Context, mode, deviceID, agentID, text string) {
	worker := &MessageWorker{}
	worker.saveShortMemory(&eventbus.AddMessageEvent{
		ClientState: &data_client.ClientState{
			Ctx:      ctx,
			DeviceID: deviceID,
			AgentID:  agentID,
			DeviceConfig: utypes.UConfig{
				MemoryMode: mode,
			},
		},
		Msg: *schema.UserMessage(text),
	})
}

func saveLong(store *recordingMemory, deviceID, agentID, text string) {
	worker := &MessageWorker{}
	worker.processMemoryProvider(&eventbus.AddMessageEvent{
		ClientState: &data_client.ClientState{
			Ctx:            context.Background(),
			DeviceID:       deviceID,
			AgentID:        agentID,
			MemoryProvider: store,
			DeviceConfig:   utypes.UConfig{MemoryMode: data_client.MemoryModeLong},
		},
		Msg: *schema.UserMessage(text),
	})
}

type storedMemory struct {
	id      string
	content string
}

type recordingMemory struct {
	added []storedMemory
}

func (r *recordingMemory) AddMessage(_ context.Context, agentID string, msg schema.Message) error {
	r.added = append(r.added, storedMemory{id: agentID, content: msg.Content})
	return nil
}

func (r *recordingMemory) GetMessages(context.Context, string, int) ([]*schema.Message, error) {
	return nil, nil
}

func (r *recordingMemory) GetContext(context.Context, string, int) (string, error) {
	return "", nil
}

func (r *recordingMemory) Search(context.Context, string, string, int, int64) (string, error) {
	return "", nil
}

func (r *recordingMemory) Flush(context.Context, string) error { return nil }

func (r *recordingMemory) ResetMemory(context.Context, string) error { return nil }

var _ memory.MemoryProvider = (*recordingMemory)(nil)

func useMiniRedis(t *testing.T) *miniredis.Miniredis {
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
	return mr
}
