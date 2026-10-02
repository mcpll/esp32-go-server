package server

import (
	"context"
	"hash/fnv"
	"runtime"
	"sync"

	data_client "xiaozhi-esp32-server-golang/internal/data/client"
	"xiaozhi-esp32-server-golang/internal/domain/eventbus"
	"xiaozhi-esp32-server-golang/internal/domain/memory/llm_memory"
	log "xiaozhi-esp32-server-golang/logger"

	"github.com/spf13/viper"
)

var (
	// MessageWorkerNum message worker count (based on CPU cores)
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
// Handles Redis short memory and the long-term memory provider
type MessageWorker struct {
	workers []chan *eventbus.AddMessageEvent // channel per worker
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

// NewMessageWorker creates the message worker
func NewMessageWorker() *MessageWorker {
	ctx, cancel := context.WithCancel(context.Background())

	worker := &MessageWorker{
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
	log.Infof("MessageWorker initialized, started %d worker goroutines (Redis short memory + memory provider)", MessageWorkerNum)
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
// Handles Redis short memory and the memory provider; keeps order per device/session
func (w *MessageWorker) processMessage(event *eventbus.AddMessageEvent) {
	// Audio updates of an already-saved message only mattered for the removed history store
	if event.IsUpdate {
		return
	}

	// 1. Redis short memory (for LLM context)
	w.saveShortMemory(event)

	// 2. Long-term memory provider (memobase/mem0)
	w.processMemoryProvider(event)
}

// processMemoryProvider handle long-term memory (memobase/mem0)
// Only acts in long memory mode
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

// saveShortMemory appends the message to the Redis message list used as LLM context.
// It only runs when Redis is enabled and the agent's memory mode is not none.
func (w *MessageWorker) saveShortMemory(event *eventbus.AddMessageEvent) {
	clientState := event.ClientState
	if !viper.GetBool("redis.enable") || clientState.GetMemoryMode() == data_client.MemoryModeNone {
		return
	}
	llm_memory.Get().AddMessage(
		clientState.Ctx,
		clientState.DeviceID,
		clientState.AgentID,
		event.Msg)
}
