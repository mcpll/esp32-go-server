package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"hash/fnv"
	"runtime"
	"sync"
	"time"

	data_client "xiaozhi-esp32-server-golang/internal/data/client"
	"xiaozhi-esp32-server-golang/internal/data/history"
	"xiaozhi-esp32-server-golang/internal/domain/eventbus"
	"xiaozhi-esp32-server-golang/internal/domain/memory/llm_memory"
	"xiaozhi-esp32-server-golang/internal/util"
	log "xiaozhi-esp32-server-golang/logger"

	"github.com/cloudwego/eino/schema"
	"github.com/spf13/viper"
)

var (
	// MessageWorkerNum message worker count (based on CPU cores; shared config for Redis+History)
	// Must be a power of 2 for hash distribution
	MessageWorkerNum = getMessageWorkerNum()
)

// getMessageWorkerNum computes worker count from CPU cores, rounding up to the nearest power of 2
// Min 4, max 64
func getMessageWorkerNum() int {
	cpuNum := runtime.NumCPU()

	// Min 4, max 64
	if cpuNum < 4 {
		return 4
	}
	if cpuNum > 64 {
		return 64
	}

	// Round up to nearest power of 2
	power := 1
	for power < cpuNum {
		power <<= 1
	}
	return power
}

// MessageWorker message worker
// Fixed-size goroutine pool; route by SessionID hash so one session stays ordered
// Handles Redis, MemoryProvider, and History messages uniformly
type MessageWorker struct {
	client  *history.HistoryClient
	workers []chan *eventbus.AddMessageEvent // channel per worker
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

// NewMessageWorker creates the message worker
func NewMessageWorker(cfg history.HistoryClientConfig) *MessageWorker {
	client := history.NewHistoryClient(cfg)
	ctx, cancel := context.WithCancel(context.Background())

	worker := &MessageWorker{
		client:  client,
		workers: make([]chan *eventbus.AddMessageEvent, MessageWorkerNum),
		ctx:     ctx,
		cancel:  cancel,
	}

	// Init each worker channel and start its goroutine
	for i := 0; i < MessageWorkerNum; i++ {
		worker.workers[i] = make(chan *eventbus.AddMessageEvent, 100) // buffer 100 messages
		worker.wg.Add(1)
		go worker.workerLoop(i)
	}

	worker.subscribeEvents()
	log.Infof("MessageWorker initialized, started %d worker goroutines (Redis+MemoryProvider+History)", MessageWorkerNum)
	return worker
}

// workerLoop per-worker loop (preserves order)
func (w *MessageWorker) workerLoop(index int) {
	defer w.wg.Done()
	defer log.Infof("MessageWorker worker %d exited", index)

	ch := w.workers[index]
	for {
		select {
		case <-w.ctx.Done():
			// Drain remaining messages from the channel
			for {
				select {
				case event := <-ch:
					if event != nil {
						w.processMessage(event)
					}
				default:
					return
				}
			}
		case event, ok := <-ch:
			if !ok {
				// channel already closed
				return
			}
			if event != nil {
				w.processMessage(event)
			}
		}
	}
}

// processMessage process message (runs in order on the worker goroutine)
// Handle Redis, MemoryProvider, and History; keep order per device/session
func (w *MessageWorker) processMessage(event *eventbus.AddMessageEvent) {
	// 1. Handle History (all messages)
	// Use a separate context unaffected by event.ClientState.Ctx so history save survives chat cancel
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Decide create vs update
	if event.IsUpdate {
		// Phase 2: update audio
		w.updateMessageAudio(ctx, event)
	} else {
		// Phase 1: save text message (includes Redis)
		w.saveMessageText(ctx, event)
	}

	// 2. Handle MemoryProvider (only when !IsUpdate; independent of redis/manager)
	// Long-term memory (memobase/mem0); needed for both redis and manager
	if !event.IsUpdate {
		w.processMemoryProvider(event)
	}
}

// processMemoryProvider handle long-term memory (memobase/mem0)
// Independent of redis/manager; always process
func (w *MessageWorker) processMemoryProvider(event *eventbus.AddMessageEvent) {
	clientState := event.ClientState
	if clientState.MemoryProvider == nil {
		return
	}
	if clientState.GetMemoryMode() != data_client.MemoryModeLong {
		return
	}

	err := clientState.MemoryProvider.AddMessage(
		clientState.Ctx,
		clientState.GetDeviceIDOrAgentID(),
		event.Msg)
	if err != nil {
		log.Errorf("add message to memory provider failed: %v", err)
	}
}

// hashSessionID hash SessionID and return worker index
func (w *MessageWorker) hashSessionID(sessionID string) int {
	if sessionID == "" {
		return 0 // If SessionID is empty, use the first worker
	}

	// Use FNV-1a hash
	h := fnv.New32a()
	h.Write([]byte(sessionID))
	hash := h.Sum32()
	return int(hash) % MessageWorkerNum
}

// subscribeEvents subscribe to EventBus events
func (w *MessageWorker) subscribeEvents() {
	bus := eventbus.Get()
	// Subscribe to the shared add-message event (same Topic as EventHandle)
	bus.Subscribe(eventbus.TopicAddMessage, w.handleAddMessage)
}

// handleAddMessage handle add-message events (route to the matching worker)
func (w *MessageWorker) handleAddMessage(event *eventbus.AddMessageEvent) {
	if event == nil || event.ClientState == nil {
		return
	}

	// Routing key: prefer SessionID, else DeviceID
	key := event.ClientState.SessionID
	if key == "" {
		key = event.ClientState.DeviceID
	}
	if key == "" {
		log.Warnf("both SessionID and DeviceID are empty, cannot route message")
		return
	}

	// Hash and route to the matching worker
	workerIndex := w.hashSessionID(key)

	// Non-blocking send to the worker channel
	select {
	case w.workers[workerIndex] <- event:
		// Send succeeded
	default:
		// Channel full; log warning (rare because the channel is buffered)
		log.Warnf("worker %d channel is full, dropping message, session_id: %s, device_id: %s",
			workerIndex, event.ClientState.SessionID, event.ClientState.DeviceID)
	}
}

// saveMessageText save text message (phase 1, or text+audio in one shot)
// Includes Redis handling when config_provider.type is redis
func (w *MessageWorker) saveMessageText(ctx context.Context, event *eventbus.AddMessageEvent) {
	// Handle Redis (only when config_provider.type is redis)
	// Append to Redis message list (for LLM context)
	providerType := viper.GetString("config_provider.type")
	if providerType == "redis" {
		clientState := event.ClientState
		llm_memory.Get().AddMessage(
			clientState.Ctx,
			clientState.DeviceID,
			clientState.AgentID,
			event.Msg)
		return
	}

	// Determine message role
	var role history.MessageType
	switch event.Msg.Role {
	case schema.User:
		role = history.MessageTypeUser
	case schema.Assistant:
		role = history.MessageTypeAssistant
	case schema.Tool:
		role = history.MessageTypeTool
	case schema.System:
		role = history.MessageTypeSystem
	default:
		log.Warnf("unsupported message role: %s", event.Msg.Role)
		return
	}

	// Convert audio format if present
	var audioBase64 string
	var audioFormat string
	var audioSize int

	if len(event.AudioData) > 0 {
		// ASR message: text and audio together, save once
		var wavData []byte
		var err error

		// Pick audio conversion by message role
		if event.Msg.Role == schema.User {
			// User message (ASR): PCM float32
			if len(event.AudioData) > 0 {
				wavData, err = util.PCMFloat32BytesToWav(
					event.AudioData[0], // User message has a single element
					event.SampleRate,
					event.Channels)
			}
		} else {
			// Assistant message (TTS): Opus (should not land here; Assistant uses two-phase save)
			wavData, err = util.OpusFramesToWav(
				event.AudioData,
				event.SampleRate,
				event.Channels)
		}

		if err != nil {
			log.Errorf("audio conversion failed, device_id: %s, message_id: %s, role: %s, error: %v",
				event.ClientState.DeviceID, event.MessageID, event.Msg.Role, err)
			// Fallback: concatenate all frames
			var fallbackData []byte
			for _, frame := range event.AudioData {
				fallbackData = append(fallbackData, frame...)
			}
			audioBase64 = base64.StdEncoding.EncodeToString(fallbackData)
			audioSize = event.AudioSize
			audioFormat = "raw" // Fallback uses raw format
		} else {
			audioBase64 = base64.StdEncoding.EncodeToString(wavData)
			audioSize = len(wavData)
			audioFormat = "wav"
		}
	}

	// Build Metadata (timestamp only)
	metadata := map[string]interface{}{
		"timestamp": event.Timestamp.Format(time.RFC3339),
	}

	// Prepare tool-call fields
	var toolCallID string
	var toolCallsJSON *string

	// Tool role: save tool_call_id
	if event.Msg.Role == schema.Tool && event.Msg.ToolCallID != "" {
		toolCallID = event.Msg.ToolCallID
	}

	// Assistant role: save ToolCalls if any
	if event.Msg.Role == schema.Assistant && len(event.Msg.ToolCalls) > 0 {
		// Serialize ToolCalls to JSON string
		toolCallsBytes, err := json.Marshal(event.Msg.ToolCalls)
		if err != nil {
			log.Warnf("failed to serialize ToolCalls, device_id: %s, message_id: %s, error: %v",
				event.ClientState.DeviceID, event.MessageID, err)
		} else {
			jsonStr := string(toolCallsBytes)
			toolCallsJSON = &jsonStr
		}
	}

	req := &history.SaveMessageRequest{
		MessageID:     event.MessageID,
		DeviceID:      event.ClientState.DeviceID,
		AgentID:       event.ClientState.AgentID,
		SessionID:     event.ClientState.SessionID,
		Role:          role,
		Content:       event.Msg.Content,
		ToolCallID:    toolCallID,
		ToolCallsJSON: toolCallsJSON,
		AudioData:     audioBase64,
		AudioFormat:   audioFormat,
		AudioSize:     audioSize,
		Metadata:      metadata,
	}

	if err := w.client.SaveMessage(ctx, req); err != nil {
		log.Errorf("failed to save message, device_id: %s, message_id: %s, error: %v",
			event.ClientState.DeviceID, event.MessageID, err)
	}
}

// updateMessageAudio update message audio (phase 2)
func (w *MessageWorker) updateMessageAudio(ctx context.Context, event *eventbus.AddMessageEvent) {
	// Convert audio format
	var audioBase64 string
	var audioSize int

	if len(event.AudioData) > 0 {
		var wavData []byte
		var err error

		// Pick audio conversion by message role
		// User message (ASR): PCM float32 via PCMFloat32BytesToWav
		// Assistant message (TTS): Opus via OpusFramesToWav
		if event.Msg.Role == schema.User {
			// User message: PCM float32
			// event.AudioData is [][]byte, but User has one element (full PCM float32 bytes)
			if len(event.AudioData) > 0 {
				wavData, err = util.PCMFloat32BytesToWav(
					event.AudioData[0], // User message has a single element
					event.SampleRate,
					event.Channels)
			}
		} else {
			// Assistant message: Opus
			wavData, err = util.OpusFramesToWav(
				event.AudioData,
				event.SampleRate,
				event.Channels)
		}

		if err != nil {
			log.Errorf("audio conversion failed, device_id: %s, message_id: %s, role: %s, error: %v",
				event.ClientState.DeviceID, event.MessageID, event.Msg.Role, err)
			// Fallback: concatenate all frames
			var fallbackData []byte
			for _, frame := range event.AudioData {
				fallbackData = append(fallbackData, frame...)
			}
			audioBase64 = base64.StdEncoding.EncodeToString(fallbackData)
			audioSize = event.AudioSize
		} else {
			audioBase64 = base64.StdEncoding.EncodeToString(wavData)
			audioSize = len(wavData)
		}
	}

	// Build update request
	req := &history.UpdateMessageAudioRequest{
		MessageID:   event.MessageID,
		AudioData:   audioBase64,
		AudioFormat: "wav",
		AudioSize:   audioSize,
		Metadata: map[string]interface{}{
			"tts_duration": event.TTSDuration,
		},
	}

	// Call update API
	if err := w.client.UpdateMessageAudio(ctx, req); err != nil {
		log.Errorf("failed to update message audio, device_id: %s, message_id: %s, error: %v",
			event.ClientState.DeviceID, event.MessageID, err)
	}
}
