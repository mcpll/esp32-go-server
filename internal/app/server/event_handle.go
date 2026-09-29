package server

import (
	"context"
	"hash/fnv"
	"sync"
	. "xiaozhi-esp32-server-golang/internal/data/client"
	"xiaozhi-esp32-server-golang/internal/domain/eventbus"
	log "xiaozhi-esp32-server-golang/logger"
)

// EventWrapper wraps events for unified handling of different types
type EventWrapper struct {
	Topic string      // topic name
	Data  interface{} // event data
}

// TopicHandler common topic handler interface
type TopicHandler interface {
	// Process handles the event
	Process(ctx context.Context, data interface{}) error
	// GetRoutingKey returns the hash routing key (usually DeviceID or SessionID)
	GetRoutingKey(data interface{}) string
}

// UnifiedWorkerPool unified worker pool that can handle multiple topics
type UnifiedWorkerPool struct {
	workers   []chan *EventWrapper
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	handlers  map[string]TopicHandler // topic -> handler map
	workerNum int
	mu        sync.RWMutex // protects handlers map
}

// NewUnifiedWorkerPool creates a unified worker pool
func NewUnifiedWorkerPool(workerNum int) *UnifiedWorkerPool {
	ctx, cancel := context.WithCancel(context.Background())

	pool := &UnifiedWorkerPool{
		workers:   make([]chan *EventWrapper, workerNum),
		ctx:       ctx,
		cancel:    cancel,
		handlers:  make(map[string]TopicHandler),
		workerNum: workerNum,
	}

	// Init each worker channel and start its goroutine
	for i := 0; i < workerNum; i++ {
		pool.workers[i] = make(chan *EventWrapper, 100) // buffer 100 messages
		pool.wg.Add(1)
		go pool.workerLoop(i)
	}

	log.Infof("UnifiedWorkerPool initialized, started %d worker goroutines (multi-topic)", workerNum)
	return pool
}

// RegisterHandler registers a topic handler
func (p *UnifiedWorkerPool) RegisterHandler(topic string, handler TopicHandler) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.handlers[topic] = handler
	log.Infof("UnifiedWorkerPool: register topic handler [%s]", topic)
}

// workerLoop is each worker's processing loop (ordered)
func (p *UnifiedWorkerPool) workerLoop(index int) {
	defer p.wg.Done()
	defer log.Infof("UnifiedWorkerPool worker %d exited", index)

	ch := p.workers[index]
	for {
		select {
		case <-p.ctx.Done():
			// Drain remaining messages from the channel
			for {
				select {
				case event := <-ch:
					if event != nil {
						p.processEvent(event)
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
				p.processEvent(event)
			}
		}
	}
}

// processEvent dispatches the event to the topic handler
func (p *UnifiedWorkerPool) processEvent(event *EventWrapper) {
	p.mu.RLock()
	handler, exists := p.handlers[event.Topic]
	p.mu.RUnlock()

	if !exists {
		log.Warnf("UnifiedWorkerPool: topic [%s] has no registered handler, skip", event.Topic)
		return
	}

	if err := handler.Process(context.Background(), event.Data); err != nil {
		log.Errorf("UnifiedWorkerPool: topic [%s] handle failed: %v", event.Topic, err)
	}
}

// Route sends the event to a worker by hash
func (p *UnifiedWorkerPool) Route(topic string, data interface{}) bool {
	p.mu.RLock()
	handler, exists := p.handlers[topic]
	p.mu.RUnlock()

	if !exists {
		log.Warnf("UnifiedWorkerPool: topic [%s] has no registered handler, cannot route", topic)
		return false
	}

	// Get routing key
	key := handler.GetRoutingKey(data)
	if key == "" {
		log.Warnf("UnifiedWorkerPool: topic [%s] route key empty, cannot route message", topic)
		return false
	}

	// Compute hash and route to the worker
	workerIndex := p.hashKey(key)

	// Create event wrapper
	event := &EventWrapper{
		Topic: topic,
		Data:  data,
	}

	// Non-blocking send to the worker channel
	select {
	case p.workers[workerIndex] <- event:
		return true
	default:
		log.Warnf("UnifiedWorkerPool: topic [%s] worker %d channel full, drop message, key: %s",
			topic, workerIndex, key)
		return false
	}
}

// hashKey hashes the key to a worker index
func (p *UnifiedWorkerPool) hashKey(key string) int {
	if key == "" {
		return 0
	}
	h := fnv.New32a()
	h.Write([]byte(key))
	hash := h.Sum32()
	return int(hash) % p.workerNum
}

// Close shuts down the worker pool
func (p *UnifiedWorkerPool) Close() {
	p.cancel()
	p.wg.Wait()

	// Close all worker channels
	for i := 0; i < p.workerNum; i++ {
		close(p.workers[i])
	}

	log.Info("UnifiedWorkerPool closed")
}

type EventHandle struct {
	// Unified worker pool that can handle multiple topics
	workerPool *UnifiedWorkerPool
	// App reference for ChatManager access
	app *App
}

// SessionEndHandler handles SessionEnd events
type SessionEndHandler struct{}

func (h *SessionEndHandler) Process(ctx context.Context, data interface{}) error {
	clientState, ok := data.(*ClientState)
	if !ok || clientState == nil {
		return nil
	}

	if clientState.MemoryProvider == nil {
		return nil
	}
	if clientState.GetMemoryMode() != MemoryModeLong {
		return nil
	}

	log.Debugf("HandleSessionEnd: deviceId: %s", clientState.DeviceID)

	// Add message to long-term memory
	err := clientState.MemoryProvider.Flush(
		clientState.Ctx,
		clientState.GetDeviceIDOrAgentID())
	if err != nil {
		log.Errorf("flush message to memory provider failed: %v", err)
		return err
	}
	return nil
}

func (h *SessionEndHandler) GetRoutingKey(data interface{}) string {
	clientState, ok := data.(*ClientState)
	if !ok || clientState == nil {
		return ""
	}
	return clientState.DeviceID
}

// ExitChatHandler handles ExitChat events
type ExitChatHandler struct {
	eventHandle *EventHandle // holds EventHandle for App access
}

func (h *ExitChatHandler) Process(ctx context.Context, data interface{}) error {
	event, ok := data.(*eventbus.ExitChatEvent)
	if !ok || event == nil {
		return nil
	}

	clientState := event.ClientState
	if clientState == nil {
		return nil
	}

	log.Debugf("Handle exit chat event: device_id: %s, reason: %s, trigger: %s, user_text: %s",
		clientState.DeviceID, event.Reason, event.TriggerType, event.UserText)

	// Get ChatManager by deviceId
	if h.eventHandle == nil || h.eventHandle.app == nil {
		log.Warnf("EventHandle or App not initialized, cannot get ChatManager")
		return nil
	}

	chatManager, exists := h.eventHandle.app.GetChatManager(clientState.DeviceID)
	if !exists {
		log.Warnf("ChatManager for device %s not found, may already be closed", clientState.DeviceID)
		return nil
	}

	return chatManager.ExitChat()
}

func (h *ExitChatHandler) GetRoutingKey(data interface{}) string {
	event, ok := data.(*eventbus.ExitChatEvent)
	if !ok || event == nil || event.ClientState == nil {
		return ""
	}
	return event.ClientState.DeviceID
}

func NewEventHandle(app *App) (*EventHandle, error) {
	// Create unified worker pool
	workerPool := NewUnifiedWorkerPool(MessageWorkerNum)

	// Register SessionEnd handler
	sessionEndHandler := &SessionEndHandler{}
	workerPool.RegisterHandler(eventbus.TopicSessionEnd, sessionEndHandler)

	handle := &EventHandle{
		workerPool: workerPool,
		app:        app,
	}

	// Register ExitChat handler
	exitChatHandler := &ExitChatHandler{
		eventHandle: handle,
	}
	workerPool.RegisterHandler(eventbus.TopicExitChat, exitChatHandler)

	log.Infof("EventHandle initialized (unified worker pool for multiple topics; Redis handling moved to MessageWorker)")
	return handle, nil
}

func (s *EventHandle) Start() error {
	// Subscribe to SessionEnd events
	go s.HandleSessionEnd()

	// Subscribe to ExitChat events
	go s.HandleExitChat()

	// Other topic subscriptions can be added here
	// go s.HandleDeviceOnline()

	return nil
}

// HandleSessionEnd subscribes and handles SessionEnd events
func (s *EventHandle) HandleSessionEnd() error {
	eventbus.Get().Subscribe(eventbus.TopicSessionEnd, func(clientState *ClientState) {
		if clientState == nil {
			log.Warnf("HandleSessionEnd: clientState is nil, skipping")
			return
		}

		// Route to the unified worker pool
		s.workerPool.Route(eventbus.TopicSessionEnd, clientState)
	})
	return nil
}

// HandleExitChat subscribes and handles ExitChat events
func (s *EventHandle) HandleExitChat() error {
	eventbus.Get().Subscribe(eventbus.TopicExitChat, func(event *eventbus.ExitChatEvent) {
		if event == nil {
			log.Warnf("HandleExitChat: event is nil, skipping")
			return
		}

		// Route to the unified worker pool
		s.workerPool.Route(eventbus.TopicExitChat, event)
	})
	return nil
}

// RegisterTopic registers a new topic handler (convenience)
func (s *EventHandle) RegisterTopic(topic string, handler TopicHandler) {
	s.workerPool.RegisterHandler(topic, handler)
}

// Close shuts down EventHandle and the worker pool gracefully
func (s *EventHandle) Close() {
	if s.workerPool != nil {
		s.workerPool.Close()
	}
	log.Info("EventHandle closed")
}
