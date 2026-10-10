package chat

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	. "xiaozhi-esp32-server-golang/internal/data/client"
	chathooks "xiaozhi-esp32-server-golang/internal/domain/chat/hooks"
	"xiaozhi-esp32-server-golang/internal/domain/chat/streamtransform"
	config_types "xiaozhi-esp32-server-golang/internal/domain/config/types"
	"xiaozhi-esp32-server-golang/internal/domain/eventbus"
	"xiaozhi-esp32-server-golang/internal/domain/llm"
	llm_common "xiaozhi-esp32-server-golang/internal/domain/llm/common"
	"xiaozhi-esp32-server-golang/internal/domain/speaker"
	"xiaozhi-esp32-server-golang/internal/pool"
	"xiaozhi-esp32-server-golang/internal/util"
	log "xiaozhi-esp32-server-golang/logger"

	"github.com/cloudwego/eino/schema"
	"github.com/spf13/viper"
)

const (
	MaxMessageCount = 10

	McpReadResourcePageSize       = 100 * 1024
	McpReadResourceStreamDoneFlag = "[DONE]"
)

// Context key type to avoid collisions
type contextKey int

const (
	ttsPlaybackCompletionGrace time.Duration = 150 * time.Millisecond
	fullTextKey                contextKey    = iota
	toolRoundMessagesKey
	ttsTurnTrackerKey
	ttsPlaybackStartHookKey
	ttsTurnEndPolicyKey
	ttsTurnEndPolicyHandlerKey
	ttsTurnPlaybackSettledKey
)

const (
	interruptExtraKey      = "interrupt"
	interruptByExtraKey    = "interrupt_by"
	interruptStageExtraKey = "interrupt_stage"
	interruptContentSuffix = " [user interrupted]"
)

// GetLastMessageID returns the MessageID of the last saved message (two-phase save)
func (l *LLMManager) GetLastMessageID(role string) (string, bool) {
	l.lastMessageIDMu.RLock()
	defer l.lastMessageIDMu.RUnlock()
	id, ok := l.lastMessageID[role]
	return id, ok
}

type LLMResponseChannelItem struct {
	ctx          context.Context
	userMessage  *schema.Message
	responseChan chan llm_common.LLMResponseStruct
	onStartFunc  func(args ...any)
	onEndFunc    func(err error, args ...any)
}

type llmHandleResult struct {
	ok                      bool
	suppressProtocolTtsStop bool
}

func llmHandleResultFromArgs(args []any) llmHandleResult {
	if len(args) == 0 {
		return llmHandleResult{}
	}
	result, ok := args[0].(llmHandleResult)
	if !ok {
		return llmHandleResult{}
	}
	return result
}

func (l *LLMManager) finishTTSTurn(ctx context.Context, stopErr error, result llmHandleResult) {
	l.finishTTSTurnWithReason(ctx, stopErr, result, "LLMManager.finishTTSTurn")
}

func (l *LLMManager) finishTTSTurnWithReason(ctx context.Context, stopErr error, result llmHandleResult, reason string) {
	if l == nil || l.ttsManager == nil {
		return
	}

	if result.suppressProtocolTtsStop {
		// Media tools wait for playback to finish before returning here; still need a protocol-level tts_stop,
		// otherwise the client stays in the "speaking" state.
		log.Debugf("Media output finished, send tts stop via normal TTS teardown")
	}

	l.ttsManager.EnqueueTtsStopWithReason(ctx, reason)
	l.ttsManager.RequestTurnEnd(ctx, stopErr)
}

type llmResponseChannelOptions struct {
	disableTTSCommands bool
	onStartFunc        func(args ...any)
	onEndFunc          func(err error, args ...any)
	onTTSPlaybackStart func()
	ttsTurnEndPolicy   ttsTurnEndPolicy
}

type ttsPlaybackStartHook func()

type ttsTurnEndPolicy uint8

const (
	ttsTurnEndPolicyNone ttsTurnEndPolicy = iota
	ttsTurnEndPolicyGoodbyeAndIdle
)

type ttsTurnEndPolicyHandler interface {
	handleTTSTurnEndPolicy(ctx context.Context, policy ttsTurnEndPolicy, stopErr error)
}

func withTTSPlaybackStartHook(ctx context.Context, hook func()) context.Context {
	if ctx == nil || hook == nil {
		return ctx
	}

	var once sync.Once
	return context.WithValue(ctx, ttsPlaybackStartHookKey, ttsPlaybackStartHook(func() {
		once.Do(hook)
	}))
}

func ttsPlaybackStartHookFromContext(ctx context.Context) func() {
	if ctx == nil {
		return nil
	}
	hook, ok := ctx.Value(ttsPlaybackStartHookKey).(ttsPlaybackStartHook)
	if !ok || hook == nil {
		return nil
	}
	return func() {
		hook()
	}
}

func withTTSTurnEndPolicy(ctx context.Context, policy ttsTurnEndPolicy) context.Context {
	if ctx == nil || policy == ttsTurnEndPolicyNone {
		return ctx
	}
	return context.WithValue(ctx, ttsTurnEndPolicyKey, policy)
}

func ttsTurnEndPolicyFromContext(ctx context.Context) ttsTurnEndPolicy {
	if ctx == nil {
		return ttsTurnEndPolicyNone
	}
	policy, ok := ctx.Value(ttsTurnEndPolicyKey).(ttsTurnEndPolicy)
	if !ok {
		return ttsTurnEndPolicyNone
	}
	return policy
}

func withTTSTurnEndPolicyHandler(ctx context.Context, handler ttsTurnEndPolicyHandler) context.Context {
	if ctx == nil || handler == nil {
		return ctx
	}
	return context.WithValue(ctx, ttsTurnEndPolicyHandlerKey, handler)
}

func withTTSTurnPlaybackSettled(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, ttsTurnPlaybackSettledKey, true)
}

func ttsTurnPlaybackSettledFromContext(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	settled, ok := ctx.Value(ttsTurnPlaybackSettledKey).(bool)
	return ok && settled
}

func ttsTurnEndPolicyHandlerFromContext(ctx context.Context) ttsTurnEndPolicyHandler {
	if ctx == nil {
		return nil
	}
	handler, ok := ctx.Value(ttsTurnEndPolicyHandlerKey).(ttsTurnEndPolicyHandler)
	if !ok {
		return nil
	}
	return handler
}

type ttsTurnTracker struct {
	mu      sync.Mutex
	pending int
	doneCh  chan struct{}
}

func newTTSTurnTracker() *ttsTurnTracker {
	doneCh := make(chan struct{})
	close(doneCh)
	return &ttsTurnTracker{doneCh: doneCh}
}

func (t *ttsTurnTracker) Add() func(error) {
	if t == nil {
		return func(error) {}
	}

	t.mu.Lock()
	if t.pending == 0 {
		t.doneCh = make(chan struct{})
	}
	t.pending++
	t.mu.Unlock()

	var once sync.Once
	return func(error) {
		once.Do(func() {
			t.mu.Lock()
			defer t.mu.Unlock()
			if t.pending == 0 {
				return
			}
			t.pending--
			if t.pending == 0 {
				close(t.doneCh)
			}
		})
	}
}

func (t *ttsTurnTracker) Wait(ctx context.Context) error {
	if t == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	t.mu.Lock()
	pending := t.pending
	doneCh := t.doneCh
	t.mu.Unlock()

	if pending == 0 {
		return nil
	}

	select {
	case <-doneCh:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type LLMManager struct {
	clientState       *ClientState
	session           *ChatSession
	serverTransport   *ServerTransport
	ttsManager        *TTSManager
	transformRegistry *streamtransform.Registry

	einoTools []*schema.ToolInfo

	llmResponseQueue *util.Queue[LLMResponseChannelItem]

	// Stores the MessageID of the last saved message (two-phase save)
	// key: role (user/assistant), value: MessageID
	lastMessageID   map[string]string
	lastMessageIDMu sync.RWMutex // Guards concurrent access to lastMessageID
}

func NewLLMManager(clientState *ClientState, serverTransport *ServerTransport, ttsManager *TTSManager, session *ChatSession, transformRegistry *streamtransform.Registry) *LLMManager {
	return &LLMManager{
		clientState:       clientState,
		session:           session,
		serverTransport:   serverTransport,
		ttsManager:        ttsManager,
		transformRegistry: transformRegistry,
		llmResponseQueue:  util.NewQueue[LLMResponseChannelItem](10),
		lastMessageID:     make(map[string]string),
	}
}

func (l *LLMManager) openOutputPipeline(ctx context.Context) (*streamtransform.Pipeline, error) {
	if l == nil || l.transformRegistry == nil {
		return &streamtransform.Pipeline{}, nil
	}

	sessionID := ""
	deviceID := ""
	if l.clientState != nil {
		sessionID = l.clientState.SessionID
		deviceID = l.clientState.DeviceID
	}

	return l.transformRegistry.Open(streamtransform.Context{
		Ctx:       ctx,
		SessionID: sessionID,
		DeviceID:  deviceID,
		RequestID: fmt.Sprintf("%s-%d", sessionID, time.Now().UnixNano()),
	})
}

func (l *LLMManager) emitLLMOutputRaw(ctx context.Context, data chathooks.LLMOutputRawData) (chathooks.LLMOutputRawData, bool, error) {
	if l == nil || l.session == nil || l.session.hookHub == nil {
		return data, false, nil
	}
	return l.session.hookHub.EmitLLMOutputRaw(l.session.hookContext(ctx), data)
}

// handleLLMWithContextAndTools handles LLM responses with context control (with or without tools)
// Internally manages LLM resource acquire/release
func (l *LLMManager) handleLLMWithContextAndTools(
	ctx context.Context,
	dialogue []*schema.Message,
	tools []*schema.ToolInfo,
) (chan llm_common.LLMResponseStruct, error) {
	// Acquire LLM resource
	llmWrapper, err := pool.Acquire[llm.LLMProvider](
		"llm",
		l.clientState.DeviceConfig.Llm.Provider,
		l.clientState.DeviceConfig.Llm.Config,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire LLM resource: %w", err)
	}

	// Get provider
	llmProvider := llmWrapper.GetProvider()

	// Call LLM provider
	msgChan := llmProvider.ResponseWithContext(ctx, l.clientState.SessionID, dialogue, tools)

	pipeline, err := l.openOutputPipeline(ctx)
	if err != nil {
		pool.Release(llmWrapper)
		return nil, fmt.Errorf("failed to create the LLM output transform pipeline: %w", err)
	}

	// Create response channel
	responseChannel := make(chan llm_common.LLMResponseStruct, 2)
	startTs := time.Now().UnixMilli()
	var firstSegment bool
	var rawFullText strings.Builder

	// Start goroutine to handle the response
	go func() {
		defer func() {
			log.Debugf("full Response with %d tools, fullText: %s", len(tools), rawFullText.String())
			close(responseChannel)
			if closeErr := pipeline.Close(); closeErr != nil {
				log.Warnf("Failed to close LLM output stream transform pipeline: %v", closeErr)
			}
			// Release resource
			pool.Release(llmWrapper)
			log.Debugf("LLM resource released")
		}()

		isFirstOutput := true
		llmFirstTokenMarked := false

		emitResponse := func(item streamtransform.Item) bool {
			response := llm_common.LLMResponseStruct{
				IsEnd: item.IsEnd,
			}

			switch item.Kind {
			case streamtransform.ItemKindToolCalls:
				response.ToolCalls = item.ToolCalls
				if len(item.ToolCalls) > 0 {
					response.IsStart = isFirstOutput
				}
			case streamtransform.ItemKindTextDelta, streamtransform.ItemKindTextSegment:
				response.Text = item.Text
				if strings.TrimSpace(item.Text) != "" {
					response.IsStart = isFirstOutput
					if !firstSegment {
						firstSegment = true
						firstSentenceTs := time.Now().UnixMilli()
						if l.clientState.MarkLlmFirstSentenceAt(firstSentenceTs) && l.session != nil {
							l.session.TraceLlmFirstSentence(ctx, firstSentenceTs)
						}
						log.Infof("Latency stats: LLM first sentence: %d ms", firstSentenceTs-startTs)
					}
					if isFirstOutput {
						isFirstOutput = false
					}
				}
			default:
				return true
			}

			if strings.TrimSpace(response.Text) == "" && len(response.ToolCalls) == 0 && !response.IsEnd {
				return true
			}

			select {
			case <-ctx.Done():
				log.Infof("Context canceled, stop LLM response handling: %v, context done, exit", ctx.Err())
				return false
			case responseChannel <- response:
				return true
			}
		}

		pushToPipeline := func(item streamtransform.Item) (bool, error) {
			items, stop, err := pipeline.Push(item)
			if err != nil {
				return false, err
			}
			for _, out := range items {
				if !emitResponse(out) {
					return true, nil
				}
			}
			return stop, nil
		}

		pushRawText := func(delta string, isEnd bool, errVal error) (bool, error) {
			payload, stop, hookErr := l.emitLLMOutputRaw(ctx, chathooks.LLMOutputRawData{
				Delta:    delta,
				FullText: rawFullText.String(),
				IsEnd:    isEnd,
				Err:      errVal,
			})
			if hookErr != nil {
				log.Warnf("LLM_OUTPUT_RAW hook failed: %v", hookErr)
			}
			if stop {
				log.Infof("LLM_OUTPUT_RAW hook requested stopping current flow")
				return true, nil
			}
			if payload.Delta != "" {
				rawFullText.WriteString(payload.Delta)
			}
			return pushToPipeline(streamtransform.Item{
				Kind:  streamtransform.ItemKindTextDelta,
				Text:  payload.Delta,
				IsEnd: payload.IsEnd,
			})
		}

		pushRawToolCalls := func(toolCalls []schema.ToolCall) (bool, error) {
			payload, stop, hookErr := l.emitLLMOutputRaw(ctx, chathooks.LLMOutputRawData{
				FullText:  rawFullText.String(),
				ToolCalls: toolCalls,
			})
			if hookErr != nil {
				log.Warnf("LLM_OUTPUT_RAW hook failed: %v", hookErr)
			}
			if stop {
				log.Infof("LLM_OUTPUT_RAW hook requested stopping current flow")
				return true, nil
			}
			if len(payload.ToolCalls) == 0 {
				return false, nil
			}
			return pushToPipeline(streamtransform.Item{
				Kind:      streamtransform.ItemKindToolCalls,
				ToolCalls: payload.ToolCalls,
			})
		}

		for {
			select {
			case <-ctx.Done():
				log.Infof("Context canceled, stop LLM response handling: %v, context done, exit", ctx.Err())
				return
			case message, ok := <-msgChan:
				if !ok {
					stop, pushErr := pushRawText("", true, nil)
					if pushErr != nil {
						log.Errorf("Failed to handle LLM end stream: %v", pushErr)
					}
					if stop || pushErr != nil {
						return
					}
					return
				}
				if message == nil {
					continue
				}
				if llm.IsLLMErrorMessage(message) {
					errMsg := llm.LLMErrorMessage(message)
					log.Warnf("LLM returned error: %s", errMsg)
					stop, pushErr := pushRawText(errMsg, true, nil)
					if pushErr != nil {
						log.Errorf("Failed to handle LLM error output: %v", pushErr)
					}
					if stop || pushErr != nil {
						return
					}
					return
				}
				if message.Content != "" {
					if !llmFirstTokenMarked {
						firstTokenTs := time.Now().UnixMilli()
						l.clientState.MarkLlmFirstToken()
						if l.session != nil {
							l.session.TraceLlmFirstToken(ctx, firstTokenTs)
						}
						llmFirstTokenMarked = true
					}
					stop, pushErr := pushRawText(message.Content, false, nil)
					if pushErr != nil {
						log.Errorf("Failed to handle LLM text stream: %v", pushErr)
						return
					}
					if stop {
						return
					}
				}
				if len(message.ToolCalls) > 0 {
					log.Infof("Handling tool call: %+v", message.ToolCalls)
					stop, pushErr := pushRawToolCalls(message.ToolCalls)
					if pushErr != nil {
						log.Errorf("Failed to handle LLM tool stream: %v", pushErr)
						return
					}
					if stop {
						return
					}
				}
			}
		}
	}()

	return responseChannel, nil
}

func (l *LLMManager) Start(ctx context.Context) {
	l.processLLMResponseQueue(ctx)
}

func (l *LLMManager) processLLMResponseQueue(ctx context.Context) {
	for {
		item, err := l.llmResponseQueue.Pop(ctx, 0) // Blocking
		if err != nil {
			if err == util.ErrQueueCtxDone {
				return
			}
			// Other errors
			continue
		}

		log.Debugf("processLLMResponseQueue item: %+v", item)
		if item.onStartFunc != nil {
			item.onStartFunc()
		}

		// Call handleLLMResponse; it fills fullText and toolCalls from context
		result, err := l.handleLLMResponse(item.ctx, item.userMessage, item.responseChan)
		if waitErr := waitForTTSTurnDrainIfRoot(item.ctx); err == nil && waitErr != nil {
			err = waitErr
		}

		if item.onEndFunc != nil {
			item.onEndFunc(err, result)
		}
	}
}

func (l *LLMManager) ClearLLMResponseQueue() {
	l.llmResponseQueue.Clear()
}

func (l *LLMManager) AddTextToTTSQueue(text string) error {
	return l.AddTextToTTSQueueWithOptions(text, llmResponseChannelOptions{})
}

func (l *LLMManager) AddTextToTTSQueueWithOptions(text string, options llmResponseChannelOptions) error {
	log.Debugf("AddTextToTTSQueue text: %s", text)
	msg := &schema.Message{
		Role:    schema.User,
		Content: text,
	}
	llmResponseChan := make(chan llm_common.LLMResponseStruct, 10)
	llmResponseChan <- llm_common.LLMResponseStruct{
		IsStart: true,
		IsEnd:   true,
		Text:    text,
	}
	close(llmResponseChan)

	sessionCtx := l.clientState.SessionCtx.Get(l.clientState.Ctx)
	ctx := l.clientState.AfterAsrSessionCtx.Get(sessionCtx)
	ctx = withTTSPlaybackStartHook(ctx, options.onTTSPlaybackStart)
	ctx = withTTSTurnEndPolicy(ctx, options.ttsTurnEndPolicy)
	if err := l.HandleLLMResponseChannelAsyncWithOptions(ctx, msg, llmResponseChan, options); err != nil {
		log.Warnf("AddTextToTTSQueue enqueue failed: %v", err)
		return err
	}

	return nil
}

func chainLLMResponseStartHooks(hooks ...func(args ...any)) func(args ...any) {
	filtered := make([]func(args ...any), 0, len(hooks))
	for _, hook := range hooks {
		if hook != nil {
			filtered = append(filtered, hook)
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	return func(args ...any) {
		for _, hook := range filtered {
			hook(args...)
		}
	}
}

func chainLLMResponseEndHooks(hooks ...func(err error, args ...any)) func(err error, args ...any) {
	filtered := make([]func(err error, args ...any), 0, len(hooks))
	for _, hook := range hooks {
		if hook != nil {
			filtered = append(filtered, hook)
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	return func(err error, args ...any) {
		for _, hook := range filtered {
			hook(err, args...)
		}
	}
}

func (l *LLMManager) HandleLLMResponseChannelAsync(ctx context.Context, userMessage *schema.Message, responseChan chan llm_common.LLMResponseStruct) error {
	return l.handleLLMResponseChannelAsync(ctx, userMessage, responseChan, llmResponseChannelOptions{})
}

func (l *LLMManager) HandleLLMResponseChannelAsyncWithOptions(ctx context.Context, userMessage *schema.Message, responseChan chan llm_common.LLMResponseStruct, options llmResponseChannelOptions) error {
	return l.handleLLMResponseChannelAsync(ctx, userMessage, responseChan, options)
}

func (l *LLMManager) handleLLMResponseChannelAsync(ctx context.Context, userMessage *schema.Message, responseChan chan llm_common.LLMResponseStruct, options llmResponseChannelOptions) error {
	ctx = ensureTTSTurnTrackerInContext(ctx)
	ctx = withTTSPlaybackStartHook(ctx, options.onTTSPlaybackStart)
	ctx = withTTSTurnEndPolicy(ctx, options.ttsTurnEndPolicy)

	needSendTtsCmd := true
	val := ctx.Value("nest")
	nest := 0
	log.Debugf("AddLLMResponseChannel nest: %+v", val)
	if n, ok := val.(int); ok {
		nest = n
		if nest > 1 {
			needSendTtsCmd = false
		}
	}
	if options.disableTTSCommands {
		needSendTtsCmd = false
	}

	// Init or reuse fullText in context (for chat history)
	// If context already has fullText (LLM continue after tools), reuse; else create new
	var fullText *strings.Builder
	if existingFullText, ok := ctx.Value(fullTextKey).(*strings.Builder); ok && existingFullText != nil {
		fullText = existingFullText
		log.Debugf("Reusing existing fullText, current length: %d", fullText.Len())
	} else {
		fullText = &strings.Builder{}
		ctx = context.WithValue(ctx, fullTextKey, fullText)
		log.Debugf("Creating new fullText")
	}

	var onStartFunc func(...any)
	var onEndFunc func(err error, args ...any)

	if needSendTtsCmd {
		onStartFunc = func(...any) {
			// Detect first LLM call via context nest; clear TTS audio cache only on first call
			val := ctx.Value("nest")
			if nest, ok := val.(int); !ok || nest <= 1 {
				// First call or no nest value: clear TTS audio cache
				l.ttsManager.ClearAudioHistory()
				log.Debugf("onStartFunc first call, TTS audio cache cleared")
			}
			l.ttsManager.EnqueueTtsStartWithReason(ctx, "LLMManager.handleLLMResponseChannelAsync onStart")
		}
		onEndFunc = func(err error, args ...any) {
			handleResult := llmHandleResultFromArgs(args)
			l.clientState.MarkLlmEnd()
			if l.session != nil {
				l.session.TraceLlmEnd(ctx, time.Now().UnixMilli(), err)
			}
			strFullText := fullText.String()

			l.finishTTSTurnWithReason(ctx, err, handleResult, "LLMManager.handleLLMResponseChannelAsync onEnd")

			// Get fullText from closure
			audioData := l.ttsManager.GetAndClearAudioHistory()

			// Compute total audio size (sum of all frame byte lengths)
			audioSize := 0
			for _, frame := range audioData {
				audioSize += len(frame)
			}

			// Send event only on the first call (nest<=1)
			if nest <= 1 {
				// Get MessageID from LLMManager (Assistant role)
				// If MessageID is missing, phase-1 save is incomplete; skip phase-2 update
				messageID, ok := l.GetLastMessageID(string(schema.Assistant))
				if !ok {
					log.Warnf("MessageID not found when TTS finished, skip second-stage audio update")
					return
				}

				// Publish event: phase 2 (update audio)
				assistantMsg := schema.AssistantMessage(strFullText, nil)
				eventbus.Get().Publish(eventbus.TopicAddMessage, &eventbus.AddMessageEvent{
					ClientState: l.clientState,
					Msg:         *assistantMsg,
					MessageID:   messageID,
					AudioData:   audioData, // Phase 2: with audio
					AudioSize:   audioSize,
					SampleRate:  l.clientState.OutputAudioFormat.SampleRate,
					Channels:    l.clientState.OutputAudioFormat.Channels,
					Timestamp:   time.Now(),
					IsUpdate:    true, // Update message
				})
			}
		}
	}

	onStartFunc = chainLLMResponseStartHooks(onStartFunc, options.onStartFunc)
	onEndFunc = chainLLMResponseEndHooks(onEndFunc, options.onEndFunc)

	item := LLMResponseChannelItem{
		ctx:          ctx,
		userMessage:  userMessage,
		responseChan: responseChan,
		onStartFunc:  onStartFunc,
		onEndFunc:    onEndFunc,
	}

	err := l.llmResponseQueue.Push(item)
	if err != nil {
		log.Warnf("llmResponseQueue full or closed, drop message")
		return fmt.Errorf("llmResponseQueue is full or closed, dropping the message")
	}
	return nil
}

func (l *LLMManager) HandleLLMResponseChannelSync(ctx context.Context, userMessage *schema.Message, llmResponseChannel chan llm_common.LLMResponseStruct, einoTools []*schema.ToolInfo) (bool, error) {
	ctx = ensureTTSTurnTrackerInContext(ctx)

	needSendTtsCmd := true
	val := ctx.Value("nest")
	nest := 0
	log.Debugf("AddLLMResponseChannel nest: %+v", val)
	if n, ok := val.(int); ok {
		nest = n
		if nest > 1 {
			needSendTtsCmd = false
		}
	}

	// Init or reuse fullText in context (for chat history)
	// If context already has fullText (LLM continue after tools), reuse; else create new
	var fullText *strings.Builder
	if existingFullText, ok := ctx.Value(fullTextKey).(*strings.Builder); ok && existingFullText != nil {
		fullText = existingFullText
		log.Debugf("Reusing existing fullText, current length: %d", fullText.Len())
	} else {
		fullText = &strings.Builder{}
		ctx = context.WithValue(ctx, fullTextKey, fullText)
		log.Debugf("Creating new fullText")
	}

	if needSendTtsCmd {
		// Detect first LLM call via context nest; clear TTS audio cache only on first call
		if nest <= 1 {
			// First call or no nest value: clear TTS audio cache
			l.ttsManager.ClearAudioHistory()
			log.Debugf("HandleLLMResponseChannelSync first call, TTS audio cache cleared")
		}
		l.ttsManager.EnqueueTtsStartWithReason(ctx, "LLMManager.HandleLLMResponseChannelSync start")
	}

	result, err := l.handleLLMResponse(ctx, userMessage, llmResponseChannel)
	if waitErr := waitForTTSTurnDrainIfRoot(ctx); err == nil && waitErr != nil {
		err = waitErr
	}
	l.clientState.MarkLlmEnd()
	if l.session != nil {
		l.session.TraceLlmEnd(ctx, time.Now().UnixMilli(), err)
	}
	strFullText := fullText.String()

	if needSendTtsCmd {
		l.finishTTSTurnWithReason(ctx, err, result, "LLMManager.HandleLLMResponseChannelSync end")

		// Collect TTS audio and send chat-history event
		// Note: LLM responses after tools (nest > 1) also accumulate audio but do not clear the cache
		// Clear cache and send event only on the first call (nest<=1)
		audioData := l.ttsManager.GetAndClearAudioHistory()

		// Compute total audio size (sum of all frame byte lengths)
		audioSize := 0
		for _, frame := range audioData {
			audioSize += len(frame)
		}

		// Send event only on the first call (nest<=1)
		if nest <= 1 {
			// Get MessageID from LLMManager (Assistant role)
			// If MessageID is missing, phase-1 save is incomplete; skip phase-2 update
			messageID, ok := l.GetLastMessageID(string(schema.Assistant))
			if !ok {
				log.Warnf("MessageID not found when TTS finished, skip second-stage audio update")
				return result.ok, err
			}

			// Publish event: phase 2 (update audio)
			assistantMsg := schema.AssistantMessage(strFullText, nil)
			eventbus.Get().Publish(eventbus.TopicAddMessage, &eventbus.AddMessageEvent{
				ClientState: l.clientState,
				Msg:         *assistantMsg,
				MessageID:   messageID,
				AudioData:   audioData, // Phase 2: with audio
				AudioSize:   audioSize,
				SampleRate:  l.clientState.OutputAudioFormat.SampleRate,
				Channels:    l.clientState.OutputAudioFormat.Channels,
				Timestamp:   time.Now(),
			})
		}
	} else {
		// When nest > 1: TTS commands are not sent, but audio still accumulates in the cache
		// That audio is collected together when the first response ends (nest <= 1)
		log.Debugf("LLM response after tool call (nest=%d), audio will accumulate in cache", nest)
	}

	return result.ok, err
}

// handleLLMResponse handles the LLM response
func (l *LLMManager) handleLLMResponse(ctx context.Context, userMessage *schema.Message, llmResponseChannel chan llm_common.LLMResponseStruct) (llmHandleResult, error) {
	log.Debugf("handleLLMResponse start")
	defer log.Debugf("handleLLMResponse end")

	// Get fullText from context (for chat history)
	fullText := ctx.Value(fullTextKey).(*strings.Builder)
	state := l.clientState
	// toolCalls is local (internal tool-call logic; not chat history)
	var toolCalls []schema.ToolCall
	toolExecCtx := context.WithValue(ctx, "nest", 2)
	toolExecCtx = context.WithValue(toolExecCtx, fullTextKey, fullText)
	if speechStartHook := ttsPlaybackStartHookFromContext(ctx); speechStartHook != nil {
		toolExecCtx = withTTSPlaybackStartHook(toolExecCtx, speechStartHook)
	}
	if l.clientState.GetMemoryMode() == MemoryModeNone && userMessage != nil {
		toolExecCtx = appendToolRoundMessagesToContext(toolExecCtx, []*schema.Message{userMessage})
	}
	ttsTracker := ttsTurnTrackerFromContext(ctx)
	var onTTSItemEnqueued func() func(error)
	onTTSPlaybackStart := ttsPlaybackStartHookFromContext(ctx)
	if ttsTracker != nil {
		onTTSItemEnqueued = ttsTracker.Add
	}
	toolExecutor := newToolCallExecutor(l, toolExecCtx)
	assistantSaved := false
	result := llmHandleResult{}

	saveInterruptedAssistant := func() {
		if assistantSaved {
			return
		}
		if ctx.Err() == nil {
			return
		}
		text := strings.TrimSpace(fullText.String())
		if text == "" {
			return
		}
		msg := schema.AssistantMessage(text, nil)
		msg.Extra = map[string]any{
			interruptExtraKey:      true,
			interruptByExtraKey:    "user",
			interruptStageExtraKey: "llm",
		}
		if err := l.AddLlmMessage(ctx, msg); err != nil {
			log.Errorf("Failed to save interrupted assistant message: %v", err)
			return
		}
		assistantSaved = true
	}

	select {
	case <-ctx.Done():
		saveInterruptedAssistant()
		log.Debugf("handleLLMResponse ctx done, return")
		return result, nil
	default:
	}

	for {
		select {
		case <-ctx.Done():
			// Context canceled; handle cancel first
			saveInterruptedAssistant()
			log.Infof("%s context canceled, stop processing LLM response, context done, exit", state.DeviceID)
			return result, nil
		default:
			// Non-blocking check; if ctx is not Done, continue handling the LLM response
			select {
			case llmResponse, ok := <-llmResponseChannel:
				if !ok {
					// Channel closed; exit goroutine
					log.Infof("LLM response channel closed, exit goroutine")
					result.ok = true
					return result, nil
				}
				if ctx.Err() != nil {
					saveInterruptedAssistant()
					log.Infof("%s LLM chunk arrived after context canceled, drop late response and exit", state.DeviceID)
					return result, nil
				}

				log.Debugf("LLM response: %+v", llmResponse)

				if len(llmResponse.ToolCalls) > 0 {
					log.Debugf("Got tools: %+v", llmResponse.ToolCalls)
					toolCalls = append(toolCalls, llmResponse.ToolCalls...)
					toolExecutor.Submit(llmResponse.ToolCalls)
				}

				hasText := strings.TrimSpace(llmResponse.Text) != ""
				if hasText || llmResponse.IsStart || llmResponse.IsEnd {
					// Dual-stream wrap-up needs an empty-text IsEnd; do not pass to TTS only when text is present.
					if err := l.ttsManager.handleTextResponseWithHooks(ctx, llmResponse, false, onTTSItemEnqueued, onTTSPlaybackStart); err != nil {
						result.ok = true
						return result, err
					}
				}
				if hasText {
					fullText.WriteString(llmResponse.Text)
				}

				if llmResponse.IsEnd {
					if len(toolCalls) == 0 {
						// Write to Redis
						if userMessage != nil {
							if userMessage.Role == schema.User {
								// Check whether the user message was already saved (saved during ASR)
								// Decide by whether the last message is a matching user message
								/*messages := l.clientState.GetMessages(1)
								shouldSave := true
								if len(messages) > 0 {
									lastMsg := messages[len(messages)-1]
									if lastMsg.Role == schema.User && lastMsg.Content == userMessage.Content {
										// User message already saved (during ASR); skip
										shouldSave = false
										log.Debugf("User message already saved during ASR handling, skip duplicate save: %s", userMessage.Content)
									}
								}
								if shouldSave {
									if err := l.AddLlmMessage(ctx, userMessage); err != nil {
										log.Errorf("Failed to save user message: %v", err)
									}
								}*/
							}
						}
						strFullText := fullText.String()
						if strings.TrimSpace(strFullText) != "" || len(toolCalls) > 0 {
							if err := l.AddLlmMessage(ctx, schema.AssistantMessage(strFullText, toolCalls)); err != nil {
								log.Errorf("Failed to save assistant message: %v", err)
							} else {
								assistantSaved = true
							}
						}
					}
					if len(toolCalls) > 0 {
						toolSummary, err := l.handleToolCallResponse(toolExecCtx, schema.AssistantMessage(fullText.String(), toolCalls), toolCalls, toolExecutor)
						if err != nil {
							log.Errorf("Failed to handle tool call response: %v", err)
							result.ok = true
							return result, fmt.Errorf("failed to handle the tool-call response: %v", err)
						}
						result.suppressProtocolTtsStop = toolSummary.hasMediaOutput
						if !toolSummary.invokeToolSuccess && strings.TrimSpace(llmResponse.Text) != "" {
							if err := l.ttsManager.handleTextResponseWithHooks(ctx, llmResponse, false, nil, onTTSPlaybackStart); err != nil {
								result.ok = true
								return result, err
							}
							fullText.WriteString(llmResponse.Text)
						}
					}

					result.ok = true
					return result, nil
				}
			case <-ctx.Done():
				// Context canceled; exit goroutine
				saveInterruptedAssistant()
				log.Infof("%s context canceled, stop processing LLM response, context done, exit", state.DeviceID)
				return result, nil
			}
		}
	}
}

func (l *LLMManager) DoLLmRequest(ctx context.Context, userMessage *schema.Message, einoTools []*schema.ToolInfo, isSync bool, speakerResult *speaker.IdentifyResult) error {
	log.Debugf("Sending LLM request with tools, seesionID: %s, requestEinoMessages: %+v", l.clientState.SessionID, userMessage)
	clientState := l.clientState

	l.einoTools = einoTools

	// Assemble history and the current user message
	requestMessages := l.GetMessages(ctx, userMessage, MaxMessageCount, speakerResult)

	if l.session != nil {
		payload, stop, hookErr := l.session.hookHub.EmitLLMInput(l.session.hookContext(ctx), chathooks.LLMInputData{
			UserMessage:     userMessage,
			RequestMessages: requestMessages,
			Tools:           einoTools,
		})
		if hookErr != nil {
			log.Warnf("LLM_INPUT hook failed: %v", hookErr)
		}
		userMessage = payload.UserMessage
		requestMessages = payload.RequestMessages
		einoTools = payload.Tools
		if stop {
			log.Infof("LLM_INPUT hook requested stopping current flow")
			return nil
		}
	}

	clientState.SetStartLlmTs()
	if l.session != nil {
		l.session.TraceLlmStart(ctx, time.Now().UnixMilli())
	}
	clientState.SetStatus(ClientStatusLLMStart)

	// Call internal method to handle LLM response; resources are managed inside
	responseSentences, err := l.handleLLMWithContextAndTools(
		ctx,
		requestMessages,
		einoTools,
	)
	if err != nil {
		log.Errorf("Failed to send LLM request with tools, seesionID: %s, error: %v", l.clientState.SessionID, err)
		return fmt.Errorf("failed to send the LLM request with tools: %v", err)
	}

	log.Debugf("DoLLmRequest goroutine started - SessionID: %s, context status: %v", l.clientState.SessionID, ctx.Err())

	if isSync {
		// Sync path: resources are released in handleLLMWithContextAndTools defer
		_, err := l.HandleLLMResponseChannelSync(ctx, userMessage, responseSentences, einoTools)
		if err != nil {
			log.Errorf("Failed to handle LLM response, seesionID: %s, error: %v", l.clientState.SessionID, err)
			return err
		}
	} else {
		// Async path: resources are released in handleLLMWithContextAndTools defer
		err = l.HandleLLMResponseChannelAsync(ctx, userMessage, responseSentences)
		if err != nil {
			log.Errorf("Failed to handle LLM response, seesionID: %s, error: %v", l.clientState.SessionID, err)
		}
	}

	log.Debugf("DoLLmRequest finished - SessionID: %s", l.clientState.SessionID)

	return nil
}

// AddMessage adds a message to chat history (unified entry for all message types)
func (l *LLMManager) AddMessage(ctx context.Context, msg *schema.Message) error {
	if msg == nil {
		log.Warnf("Tried to add nil message to chat history")
		return fmt.Errorf("message must not be nil")
	}

	// Generate MessageID (MD5-shortened to fit DB varchar(64))
	// Original format: {SessionID}-{Role}-{Timestamp}
	rawMessageID := fmt.Sprintf("%s-%s-%d",
		l.clientState.SessionID,
		msg.Role,
		time.Now().UnixMilli())
	// Use MD5 to produce a fixed 32-char hex string
	hash := md5.Sum([]byte(rawMessageID))
	messageID := hex.EncodeToString(hash[:])

	// Add to in-memory store synchronously
	l.clientState.AddMessage(msg)

	// Tool-role messages: save directly; no two-phase save (no audio)
	if msg.Role == schema.Tool {
		eventbus.Get().Publish(eventbus.TopicAddMessage, &eventbus.AddMessageEvent{
			ClientState: l.clientState,
			Msg:         *msg,
			MessageID:   messageID,
			AudioData:   nil, // Tool role has no audio
			AudioSize:   0,
			SampleRate:  0,
			Channels:    0,
			Timestamp:   time.Now(),
			IsUpdate:    false, // Single-shot save
		})
		return nil
	}

	// User/Assistant roles: two-phase save
	// Store MessageID in LLMManager for later audio updates
	if msg.Role == schema.User || msg.Role == schema.Assistant {
		l.lastMessageIDMu.Lock()
		l.lastMessageID[string(msg.Role)] = messageID
		l.lastMessageIDMu.Unlock()
	}

	// Publish event: phase 1 (text only, no audio)
	eventbus.Get().Publish(eventbus.TopicAddMessage, &eventbus.AddMessageEvent{
		ClientState: l.clientState,
		Msg:         *msg,
		MessageID:   messageID,
		AudioData:   nil, // Phase 1: no audio
		AudioSize:   0,
		SampleRate:  0,
		Channels:    0,
		Timestamp:   time.Now(),
		IsUpdate:    false, // New message
	})

	return nil
}

// AddLlmMessage kept for backward compatibility; delegates to AddMessage
func (l *LLMManager) AddLlmMessage(ctx context.Context, msg *schema.Message) error {
	return l.AddMessage(ctx, msg)
}

func (l *LLMManager) GetMessages(ctx context.Context, userMessage *schema.Message, count int, speakerResult *speaker.IdentifyResult) []*schema.Message {
	memoryMode := l.clientState.GetMemoryMode()
	includeHistory := memoryMode != MemoryModeNone

	// Build context from dialogue; in none mode only allow temp messages for the current tool-call chain
	messageList := make([]*schema.Message, 0)
	if includeHistory {
		messageList = l.clientState.GetMessages(count)
		if userMessage != nil {
			messageList = trimTrailingUserMessages(messageList)
		}
	} else if toolRoundMessages := toolRoundMessagesFromContext(ctx); len(toolRoundMessages) > 0 {
		messageList = toolRoundMessages
	}

	// Build system prompt
	systemPrompt := l.clientState.SystemPrompt
	globalSystemPrompt := strings.TrimSpace(viper.GetString("chat.global_system_prompt"))
	if globalSystemPrompt != "" {
		if systemPrompt != "" {
			systemPrompt = globalSystemPrompt + "\n\n" + systemPrompt
		} else {
			systemPrompt = globalSystemPrompt
		}
	}

	// Append current date/time
	now := time.Now()
	systemPrompt += fmt.Sprintf("\nCurrent time and date: %s %s", now.Format("2006-01-02 15:04:05"), now.Format("Monday"))

	if memoryMode == MemoryModeLong && l.clientState.MemoryContext != "" {
		systemPrompt += fmt.Sprintf("\nInformazioni personali:\n%s", l.clientState.MemoryContext)
	}

	log.Debugf("speakerResult: %+v, voiceIdentify: %+v", speakerResult, l.clientState.DeviceConfig.VoiceIdentify)

	// Fold speaker recognition into systemPrompt
	if speakerResult != nil && speakerResult.Identified {
		// Match speakerGroup in userConfig from speakerResult
		if l.clientState.DeviceConfig.VoiceIdentify != nil {
			// Prefer SpeakerName match (VoiceIdentify key is speakerGroup.Name)
			if speakerGroupInfo, found := l.clientState.DeviceConfig.VoiceIdentify[speakerResult.SpeakerName]; found {
				// If a matching speakerGroup is found, fold its description into systemPrompt
				if speakerGroupInfo.Prompt != "" {
					systemPrompt += fmt.Sprintf("\nIdentified speaker:\n%s", speakerGroupInfo.Prompt)
				}
			}
		}
	}

	//search memory
	if memoryMode == MemoryModeLong && l.clientState.MemoryProvider != nil && userMessage != nil {
		memoryContext, err := l.clientState.MemoryProvider.Search(ctx, l.clientState.GetDeviceIDOrAgentID(), userMessage.Content, 10, 180)
		if err != nil {
			log.Errorf("Failed to search memory: %v", err)
		}
		log.Debugf("Memory search succeeded, input: %s, memory: %s", userMessage.Content, memoryContext)
		if memoryContext != "" {
			systemPrompt += fmt.Sprintf("\nMemorie correlate:\n%s", memoryContext)
		}
	}

	systemPrompt += buildKnowledgeSearchRoutingPolicy(l.clientState.DeviceConfig.KnowledgeBases)

	retMessage := make([]*schema.Message, 0)
	retMessage = append(retMessage, &schema.Message{
		Role:    schema.System,
		Content: systemPrompt,
	})
	// Filter empty assistant messages to avoid 400 from the LLM API
	// Empty assistant messages (no Content and no ToolCalls) cause API errors
	for _, msg := range messageList {
		if msg != nil && msg.Role == schema.Assistant && msg.Content == "" && len(msg.ToolCalls) == 0 {
			log.Debugf("Filter out empty assistant messages to avoid sending to LLM API")
			continue
		}
		msgCopy := cloneMessageForRequest(msg)
		if isInterruptedMessage(msgCopy) {
			msgCopy.Content = decorateInterruptedContent(msgCopy.Content)
		}
		retMessage = append(retMessage, msgCopy)
	}
	if userMessage != nil {
		// Check whether the last retMessage is already the same user message to avoid duplicates
		shouldAdd := true
		if len(retMessage) > 0 {
			lastMsg := retMessage[len(retMessage)-1]
			if lastMsg.Role == schema.User && lastMsg.Content == userMessage.Content {
				// Last message is already the same user message; skip add
				shouldAdd = false
				//log.Debugf("Last message is already the same user message, skip duplicate add: %s", userMessage.Content)
			}
		}
		if shouldAdd {
			retMessage = append(retMessage, userMessage)
		}
	}
	return retMessage
}

func buildKnowledgeSearchRoutingPolicy(knowledgeBases []config_types.KnowledgeBaseRef) string {
	if len(knowledgeBases) == 0 {
		return ""
	}

	availableKBs := make([]string, 0, len(knowledgeBases))
	for _, kb := range knowledgeBases {
		if strings.EqualFold(strings.TrimSpace(kb.Status), "inactive") {
			continue
		}
		if strings.TrimSpace(kb.ExternalKBID) == "" {
			continue
		}
		name := strings.TrimSpace(kb.Name)
		if name == "" {
			name = strings.TrimSpace(kb.ExternalKBID)
		}
		if name == "" {
			continue
		}
		if kb.ID == 0 {
			continue
		}
		desc := strings.TrimSpace(kb.Description)
		if desc == "" {
			desc = "no description"
		}
		availableKBs = append(availableKBs, fmt.Sprintf("%d: name=%s; description=%s", kb.ID, name, desc))
		if len(availableKBs) >= 8 {
			break
		}
	}
	if len(availableKBs) == 0 {
		return ""
	}

	return fmt.Sprintf(
		"\nKnowledge search rules (tool: search_knowledge):\nAvailable knowledge bases (id: name + description): %s\n"+
			"1. When to call: the user asks for facts, procedures, parameters, rules, definitions, clauses, or comparisons that need a document, or explicitly asks to answer from the knowledge base or the documents.\n"+
			"2. When not to call: small talk, emotional support, pure creative writing, or purely subjective advice.\n"+
			"3. How to call: at most once per turn. query is the core keywords of the user's question. top_k defaults to 5. If a specific knowledge base is clear, pass knowledge_base_ids (one or more).\n"+
			"4. Selection: pass only the knowledge base IDs most relevant to the question. Omit knowledge_base_ids when it is unclear.\n"+
			"5. When evidence is thin: do not invent an answer. Ask the user for more specific keywords.\n"+
			"6. Output: do not mention the knowledge base, retrieval, MCP, tool calls, or hit results.",
		strings.Join(availableKBs, ", "),
	)
}

func trimTrailingUserMessages(messages []*schema.Message) []*schema.Message {
	end := len(messages)
	for end > 0 {
		msg := messages[end-1]
		if msg == nil || msg.Role != schema.User {
			break
		}
		end--
	}
	return messages[:end]
}

func isInterruptedMessage(msg *schema.Message) bool {
	if msg == nil || msg.Extra == nil {
		return false
	}
	v, ok := msg.Extra[interruptExtraKey]
	if !ok || v == nil {
		return false
	}
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return strings.EqualFold(strings.TrimSpace(t), "true")
	default:
		return false
	}
}

func decorateInterruptedContent(content string) string {
	if strings.TrimSpace(content) == "" {
		return content
	}
	if strings.HasSuffix(content, interruptContentSuffix) {
		return content
	}
	return content + interruptContentSuffix
}

func cloneMessagesForRequest(messages []*schema.Message) []*schema.Message {
	if len(messages) == 0 {
		return nil
	}

	cloned := make([]*schema.Message, 0, len(messages))
	for _, msg := range messages {
		if msg == nil {
			continue
		}
		cloned = append(cloned, cloneMessageForRequest(msg))
	}

	return cloned
}

func toolRoundMessagesFromContext(ctx context.Context) []*schema.Message {
	if ctx == nil {
		return nil
	}

	messages, ok := ctx.Value(toolRoundMessagesKey).([]*schema.Message)
	if !ok || len(messages) == 0 {
		return nil
	}

	return cloneMessagesForRequest(messages)
}

func ttsTurnTrackerFromContext(ctx context.Context) *ttsTurnTracker {
	if ctx == nil {
		return nil
	}

	tracker, ok := ctx.Value(ttsTurnTrackerKey).(*ttsTurnTracker)
	if !ok {
		return nil
	}

	return tracker
}

func ensureTTSTurnTrackerInContext(ctx context.Context) context.Context {
	if ttsTurnTrackerFromContext(ctx) != nil {
		return ctx
	}
	return context.WithValue(ctx, ttsTurnTrackerKey, newTTSTurnTracker())
}

func waitForTTSTurnDrainIfRoot(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	if nest, ok := ctx.Value("nest").(int); ok && nest > 1 {
		return nil
	}

	tracker := ttsTurnTrackerFromContext(ctx)
	if tracker == nil {
		return nil
	}

	return tracker.Wait(ctx)
}

func appendToolRoundMessagesToContext(ctx context.Context, messages []*schema.Message) context.Context {
	if len(messages) == 0 {
		return ctx
	}

	combined := toolRoundMessagesFromContext(ctx)
	combined = append(combined, cloneMessagesForRequest(messages)...)
	if len(combined) == 0 {
		return ctx
	}

	return context.WithValue(ctx, toolRoundMessagesKey, combined)
}

func cloneMessageForRequest(msg *schema.Message) *schema.Message {
	if msg == nil {
		return nil
	}
	msgCopy := *msg

	if msg.ToolCalls != nil {
		msgCopy.ToolCalls = append([]schema.ToolCall(nil), msg.ToolCalls...)
	}
	if msg.MultiContent != nil {
		msgCopy.MultiContent = append([]schema.ChatMessagePart(nil), msg.MultiContent...)
	}
	if msg.Extra != nil {
		msgCopy.Extra = make(map[string]any, len(msg.Extra))
		for k, v := range msg.Extra {
			msgCopy.Extra[k] = v
		}
	}
	if msg.ResponseMeta != nil {
		respMetaCopy := *msg.ResponseMeta
		msgCopy.ResponseMeta = &respMetaCopy
	}

	return &msgCopy
}
