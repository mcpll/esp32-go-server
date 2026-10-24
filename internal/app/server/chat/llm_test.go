package chat

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	data_client "xiaozhi-esp32-server-golang/internal/data/client"
	config_types "xiaozhi-esp32-server-golang/internal/domain/config/types"

	"github.com/cloudwego/eino/schema"
	mcp_go "github.com/mark3labs/mcp-go/mcp"
	"github.com/spf13/viper"
)

func TestHandleToolResultAcceptsPlainText(t *testing.T) {
	manager := &LLMManager{}

	result, ok := manager.handleToolResult("plain text result")
	if !ok {
		t.Fatal("expected plain text tool result to be accepted")
	}

	if result.IsError {
		t.Fatal("expected plain text tool result not to be marked as error")
	}

	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(result.Content))
	}

	textContent, ok := result.Content[0].(mcp_go.TextContent)
	if !ok {
		t.Fatalf("expected text content, got %T", result.Content[0])
	}

	if textContent.Text != "plain text result" {
		t.Fatalf("expected original text to be preserved, got %q", textContent.Text)
	}
}

func TestHandleToolResultAcceptsMCPJSON(t *testing.T) {
	manager := &LLMManager{}

	result, ok := manager.handleToolResult(`{"content":[{"type":"text","text":"json result"}],"isError":false}`)
	if !ok {
		t.Fatal("expected MCP JSON tool result to be accepted")
	}

	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(result.Content))
	}

	textContent, ok := result.Content[0].(mcp_go.TextContent)
	if !ok {
		t.Fatalf("expected text content, got %T", result.Content[0])
	}

	if textContent.Text != "json result" {
		t.Fatalf("expected parsed text content, got %q", textContent.Text)
	}
}

func TestGetMessagesUsesToolRoundMessagesInNoneMode(t *testing.T) {
	manager := newTestLLMManager(data_client.MemoryModeNone)
	user := schema.UserMessage("what is the weather in Milan")
	assistant := schema.AssistantMessage("", []schema.ToolCall{
		{
			ID:   "call_weather_1",
			Type: "function",
			Function: schema.FunctionCall{
				Name:      "get_weather",
				Arguments: `{"city":"Shanghai"}`,
			},
		},
	})
	toolMsg := schema.ToolMessage("Milan is cloudy, 22 degrees", "call_weather_1")

	ctx := appendToolRoundMessagesToContext(context.Background(), []*schema.Message{user, assistant, toolMsg})
	messages := manager.GetMessages(ctx, nil, 10, nil)

	if len(messages) != 4 {
		t.Fatalf("expected system + 3 tool-round messages, got %d", len(messages))
	}
	if messages[1].Role != schema.User || messages[1].Content != user.Content {
		t.Fatalf("expected tool-round user message to be forwarded, got role=%s content=%q", messages[1].Role, messages[1].Content)
	}
	if messages[2].Role != schema.Assistant || len(messages[2].ToolCalls) != 1 || messages[2].ToolCalls[0].ID != "call_weather_1" {
		t.Fatalf("expected assistant tool call message to be forwarded, got %+v", messages[2])
	}
	if messages[3].Role != schema.Tool || messages[3].ToolCallID != "call_weather_1" || messages[3].Content != toolMsg.Content {
		t.Fatalf("expected tool result message to be forwarded, got %+v", messages[3])
	}
}

func TestAppendToolRoundMessagesAccumulatesInOrder(t *testing.T) {
	user := schema.UserMessage("set an alarm")
	assistant := schema.AssistantMessage("", []schema.ToolCall{
		{
			ID:   "call_alarm_1",
			Type: "function",
			Function: schema.FunctionCall{
				Name:      "set_alarm",
				Arguments: `{"time":"07:30"}`,
			},
		},
	})
	toolMsg := schema.ToolMessage("alarm set for 7:30", "call_alarm_1")

	ctx := context.Background()
	ctx = appendToolRoundMessagesToContext(ctx, []*schema.Message{user})
	ctx = appendToolRoundMessagesToContext(ctx, []*schema.Message{assistant, toolMsg})

	messages := toolRoundMessagesFromContext(ctx)
	if len(messages) != 3 {
		t.Fatalf("expected 3 accumulated tool-round messages, got %d", len(messages))
	}
	if messages[0].Role != schema.User || messages[1].Role != schema.Assistant || messages[2].Role != schema.Tool {
		t.Fatalf("expected user -> assistant -> tool order, got roles %s -> %s -> %s", messages[0].Role, messages[1].Role, messages[2].Role)
	}
}

func TestGetMessagesIgnoresToolRoundMessagesOutsideNoneMode(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("redis.enable", true)
	manager := newTestLLMManager(data_client.MemoryModeShort)
	historyUser := schema.UserMessage("earlier message")
	manager.clientState.AddMessage(historyUser)

	ctx := appendToolRoundMessagesToContext(context.Background(), []*schema.Message{
		schema.UserMessage("temporary tool message"),
	})
	messages := manager.GetMessages(ctx, nil, 10, nil)

	if len(messages) != 2 {
		t.Fatalf("expected system + history message, got %d", len(messages))
	}
	if messages[1].Content != historyUser.Content {
		t.Fatalf("expected short memory mode to keep dialogue history, got %q", messages[1].Content)
	}
}

func TestTTSTurnTrackerWaitBlocksUntilAllDone(t *testing.T) {
	tracker := newTTSTurnTracker()
	done1 := tracker.Add()
	done2 := tracker.Add()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	waitDone := make(chan error, 1)
	go func() {
		waitDone <- tracker.Wait(ctx)
	}()

	select {
	case err := <-waitDone:
		t.Fatalf("wait returned too early: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	done1(nil)

	select {
	case err := <-waitDone:
		t.Fatalf("wait returned before final TTS item completed: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	done2(nil)

	select {
	case err := <-waitDone:
		if err != nil {
			t.Fatalf("wait returned unexpected error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("wait did not return after all TTS items completed")
	}
}

func TestTTSTurnTrackerWaitHonorsContextCancellation(t *testing.T) {
	tracker := newTTSTurnTracker()
	_ = tracker.Add()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := tracker.Wait(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
}

func TestWaitForTTSTurnDrainIfRootSkipsNestedContexts(t *testing.T) {
	ctx := ensureTTSTurnTrackerInContext(context.Background())
	tracker := ttsTurnTrackerFromContext(ctx)
	if tracker == nil {
		t.Fatal("expected tracker to be stored in context")
	}

	done := tracker.Add()
	defer done(nil)

	nestedCtx := context.WithValue(ctx, "nest", 2)
	waitCtx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if err := waitForTTSTurnDrainIfRoot(nestedCtx); err != nil {
		t.Fatalf("expected nested context wait to be skipped, got %v", err)
	}

	rootWaitDone := make(chan error, 1)
	go func() {
		rootWaitDone <- waitForTTSTurnDrainIfRoot(context.WithValue(ctx, "nest", 1))
	}()

	select {
	case err := <-rootWaitDone:
		t.Fatalf("root wait returned before TTS item completion: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	done(nil)

	select {
	case err := <-rootWaitDone:
		if err != nil {
			t.Fatalf("root wait returned unexpected error: %v", err)
		}
	case <-waitCtx.Done():
		t.Fatal("root wait did not finish after TTS item completion")
	}
}

func TestShortMemoryWithoutRedisDropsDialogueHistory(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("redis.enable", false)
	viper.Set("config_provider.type", "redis")

	manager := newTestLLMManager(data_client.MemoryModeShort)
	manager.clientState.AddMessage(schema.UserMessage("ieri"))
	messages := manager.GetMessages(context.Background(), schema.UserMessage("oggi"), 10, nil)
	for _, message := range messages {
		if message.Content == "ieri" {
			t.Fatalf("short without Redis kept dialogue history: %+v", messages)
		}
	}
	if messages[len(messages)-1].Content != "oggi" {
		t.Fatalf("current turn missing: %+v", messages)
	}
}

func TestLongMemoryPromptIsItalian(t *testing.T) {
	manager := newTestLLMManager(data_client.MemoryModeLong)
	manager.clientState.AgentID = "agent-9"
	manager.clientState.MemoryContext = "preferisce il tè"
	manager.clientState.MemoryProvider = stubMemory{search: "ha un gatto di nome Nino"}

	messages := manager.GetMessages(context.Background(), schema.UserMessage("come si chiama?"), 10, nil)
	prompt := messages[0].Content
	if !strings.Contains(prompt, "Informazioni personali:\npreferisce il tè") {
		t.Fatalf("personal info prompt = %q", prompt)
	}
	if !strings.Contains(prompt, "Memorie correlate:\nha un gatto di nome Nino") {
		t.Fatalf("recall prompt = %q", prompt)
	}
}

type stubMemory struct {
	search string
}

func (s stubMemory) AddMessage(context.Context, string, schema.Message) error { return nil }

func (s stubMemory) GetMessages(context.Context, string, int) ([]*schema.Message, error) {
	return nil, nil
}

func (s stubMemory) GetContext(context.Context, string, int) (string, error) { return "", nil }

func (s stubMemory) Search(context.Context, string, string, int, int64) (string, error) {
	return s.search, nil
}

func (s stubMemory) Flush(context.Context, string) error { return nil }

func (s stubMemory) ResetMemory(context.Context, string) error { return nil }

func newTestLLMManager(memoryMode string) *LLMManager {
	return &LLMManager{
		clientState: &data_client.ClientState{
			Dialogue: &data_client.Dialogue{},
			DeviceConfig: config_types.UConfig{
				MemoryMode: memoryMode,
			},
			SystemPrompt: "You are a test assistant",
		},
	}
}
