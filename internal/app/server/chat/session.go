package chat

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/spf13/viper"

	. "xiaozhi-esp32-server-golang/internal/data/client"
	"xiaozhi-esp32-server-golang/internal/data/history"
	. "xiaozhi-esp32-server-golang/internal/data/msg"
	chathooks "xiaozhi-esp32-server-golang/internal/domain/chat/hooks"
	"xiaozhi-esp32-server-golang/internal/domain/chat/streamtransform"
	user_config "xiaozhi-esp32-server-golang/internal/domain/config"
	"xiaozhi-esp32-server-golang/internal/domain/config/types"
	"xiaozhi-esp32-server-golang/internal/domain/eventbus"
	"xiaozhi-esp32-server-golang/internal/domain/llm"
	llm_common "xiaozhi-esp32-server-golang/internal/domain/llm/common"
	"xiaozhi-esp32-server-golang/internal/domain/mcp"
	"xiaozhi-esp32-server-golang/internal/domain/memory"
	"xiaozhi-esp32-server-golang/internal/domain/memory/llm_memory"
	"xiaozhi-esp32-server-golang/internal/domain/openclaw"
	"xiaozhi-esp32-server-golang/internal/domain/speaker"
	"xiaozhi-esp32-server-golang/internal/util"
	log "xiaozhi-esp32-server-golang/logger"
)

type AsrResponseChannelItem struct {
	ctx           context.Context
	text          string
	speakerResult *speaker.IdentifyResult
}

const detectLLMDebounceDuration = 300 * time.Millisecond

type detectAction string

const (
	detectActionSilent  detectAction = "silent"
	detectActionWelcome detectAction = "welcome"
	detectActionLLM     detectAction = "llm"
)

type welcomePlaybackResult struct {
	natural bool
}

const (
	chatSessionCloseReasonManagerShutdown     = "manager_shutdown"
	chatSessionCloseReasonExplicitExit        = "explicit_exit"
	chatSessionCloseReasonFatalError          = "fatal_error"
	chatSessionCloseReasonAudioIdleTimeout    = "audio_idle_timeout"
	chatSessionCloseReasonRetainedIdleTimeout = "retained_idle_timeout"
)

type ChatSession struct {
	clientState     *ClientState
	asrManager      *ASRManager
	ttsManager      *TTSManager
	llmManager      *LLMManager
	speakerManager  *SpeakerManager
	mediaPlayer     *SessionMediaPlayer
	serverTransport *ServerTransport

	ctx    context.Context
	cancel context.CancelFunc

	chatTextQueue *util.Queue[AsrResponseChannelItem]

	// Speaker ID result cache (mutex-protected)
	speakerResultMu        sync.RWMutex
	pendingSpeakerResult   *speaker.IdentifyResult
	speakerResultReady     chan struct{} // ready notify only; does not carry data
	turnSpeakerInterrupted atomic.Bool

	vadLoopStarted              bool
	listenStartSeq              atomic.Uint64
	realtimeListenSessionActive atomic.Bool

	// When unactivated devices trigger often, reuse the recent unactivated result briefly to avoid hammering the API.
	activationCheckMu     sync.Mutex
	lastActivationFalseAt time.Time

	// Close guard against multiple closes
	closeOnce sync.Once
	closing   atomic.Bool

	// stopSpeaking guard against races with AddAsrResultToQueue/HandleWelcome
	stopSpeakingMu sync.Mutex

	welcomePlaybackMu     sync.Mutex
	welcomePlaybackDoneCh chan welcomePlaybackResult

	detectLLMDebounceMu    sync.Mutex
	detectLLMDebounceTimer *time.Timer

	openClawStreamMu sync.Mutex
	openClawStreams  map[string]chan llm_common.LLMResponseStruct

	openClawWarmupMu sync.Mutex
	openClawWarmup   *openClawWarmupTask

	hookHub      *chathooks.Hub
	closeHandler func(session *ChatSession, reason string)
}

type ChatSessionOption func(*ChatSession)

func WithChatSessionCloseHandler(handler func(session *ChatSession, reason string)) ChatSessionOption {
	return func(s *ChatSession) {
		s.closeHandler = handler
	}
}

func NewChatSession(clientState *ClientState, serverTransport *ServerTransport, hookHub *chathooks.Hub, transformRegistry *streamtransform.Registry, opts ...ChatSessionOption) *ChatSession {
	s := &ChatSession{
		clientState:        clientState,
		serverTransport:    serverTransport,
		chatTextQueue:      util.NewQueue[AsrResponseChannelItem](10),
		speakerResultReady: make(chan struct{}, 1), // buffer size 1 to avoid blocking
		openClawStreams:    make(map[string]chan llm_common.LLMResponseStruct),
		hookHub:            hookHub,
	}
	for _, opt := range opts {
		opt(s)
	}

	s.asrManager = NewASRManager(clientState, serverTransport)
	s.asrManager.session = s // set session reference
	s.ttsManager = NewTTSManager(clientState, serverTransport, s)
	s.mediaPlayer = NewSessionMediaPlayer(s)
	s.llmManager = NewLLMManager(clientState, serverTransport, s.ttsManager, s, transformRegistry)

	clientState.OnVoiceSilenceMetricCallback = func(ctx context.Context, ts int64) {
		s.TraceVoiceSilence(ctx, ts)
	}

	// If speaker ID is enabled, create the speaker manager
	if clientState.IsSpeakerEnabled() {
		// Get speaker service URL from system config (viper)
		baseURL := viper.GetString("voice_identify.base_url")
		if baseURL != "" {
			// Set service URL and threshold into config
			speakerConfig := map[string]interface{}{
				"base_url": baseURL,
			}
			// Read threshold; default to 0.6 if unset
			if viper.IsSet("voice_identify.threshold") {
				threshold := viper.GetFloat64("voice_identify.threshold")
				speakerConfig["threshold"] = threshold
			}

			provider, err := speaker.GetSpeakerProvider(speakerConfig)
			if err != nil {
				log.Warnf("Failed to create speaker recognition provider: %v", err)
			} else {
				clientState.SpeakerProvider = provider
				s.speakerManager = NewSpeakerManager(provider)
				log.Debugf("Device %s enable speaker recognition", clientState.DeviceID)

				// Set callback to fetch speaker ID result asynchronously
				clientState.OnVoiceSilenceSpeakerCallback = func(ctx context.Context) {
					log.Debugf("[SpeakerID] OnVoiceSilenceSpeakerCallback called, deviceID: %s", clientState.DeviceID)

					// Fetch speaker ID result asynchronously
					go func() {
						log.Debugf("[SpeakerID] start async speaker recognition result fetch, deviceID: %s", clientState.DeviceID)

						// Check whether speakerManager is active
						if !s.speakerManager.IsActive() {
							//log.Warnf("[SpeakerID] speakerManager inactive, cannot get recognition result")
							return
						}
						// Clear previous result
						s.speakerResultMu.Lock()
						oldResult := s.pendingSpeakerResult
						s.pendingSpeakerResult = nil
						s.speakerResultMu.Unlock()
						if oldResult != nil {
							log.Debugf("[SpeakerID] clear previous recognition result: identified=%v, speaker_id=%s", oldResult.Identified, oldResult.SpeakerID)
						}

						// Drain ready notify (non-blocking)
						select {
						case <-s.speakerResultReady:
							log.Debugf("[SpeakerID] clear ready notify channel")
						default:
							log.Debugf("[SpeakerID] ready notify channel already empty")
						}

						result, err := s.speakerManager.FinishAndIdentify(ctx)
						if err != nil {
							log.Warnf("[SpeakerID] failed to get speaker recognition result: %v, deviceID: %s", err, clientState.DeviceID)
							// Speaker ID failure does not affect main flow; store nil result
							s.speakerResultMu.Lock()
							s.pendingSpeakerResult = nil
							s.speakerResultMu.Unlock()
							log.Debugf("[SpeakerID] stored nil result (recognition failed)")
						} else if result != nil && result.Identified {
							log.Infof("[SpeakerID] identified speaker: %s (confidence: %.4f, threshold: %.4f), deviceID: %s",
								result.SpeakerName, result.Confidence, result.Threshold, clientState.DeviceID)
							log.Debugf("[SpeakerID] recognition result details: speaker_id=%s, speaker_name=%s, confidence=%.4f, threshold=%.4f",
								result.SpeakerID, result.SpeakerName, result.Confidence, result.Threshold)
							s.speakerResultMu.Lock()
							s.pendingSpeakerResult = result
							s.speakerResultMu.Unlock()
							log.Debugf("[SpeakerID] stored recognition result (identified)")
						} else {
							// No speaker matched; still store the result
							if result != nil {
								log.Debugf("[SpeakerID] speaker not identified: identified=%v, confidence=%.4f, threshold=%.4f, deviceID: %s",
									result.Identified, result.Confidence, result.Threshold, clientState.DeviceID)
							} else {
								log.Debugf("[SpeakerID] recognition result is nil, deviceID: %s", clientState.DeviceID)
							}
							s.speakerResultMu.Lock()
							s.pendingSpeakerResult = result
							s.speakerResultMu.Unlock()
							log.Debugf("[SpeakerID] stored recognition result (not identified)")
						}

						// Notify that result is ready
						select {
						case s.speakerResultReady <- struct{}{}:
							log.Debugf("[SpeakerID] sent result-ready notify, deviceID: %s", clientState.DeviceID)
						default:
							log.Warnf("[SpeakerID] result-ready notify channel full, cannot notify, deviceID: %s", clientState.DeviceID)
						}
					}()
				}
			}
		}
	}

	// Set callback for first ASR character
	clientState.OnAsrFirstTextCallback = func(text string, isFinal bool) {
		clientState.Asr.MarkTextReceived()
		clientState.ClearAudioIdleTimeoutPending()
		clientState.PauseAudioIdleWindow(time.Now())
		log.Debugf("ASR first characters returned: device=%s, text=%s, isFinal=%v", clientState.DeviceID, text, isFinal)
		clientState.MarkAsrFirstText()
		s.TraceAsrFirstText(clientState.Ctx, time.Now().UnixMilli())
		if clientState.IsRealTime() && viper.GetInt("chat.realtime_mode") == 4 {
			if s.isRealtimeMcpAudioGateActive() {
				log.Debugf("Device %s realtime media playback gate active, skip ASR first-char interrupt: text=%s", clientState.DeviceID, text)
				return
			}
			s.StopAssistantOutputAfterAsrWithReason(true, "ChatSession.OnAsrFirstTextCallback realtime_mode=4")
		}
	}

	return s
}

func (s *ChatSession) Start(pctx context.Context) error {
	s.ctx, s.cancel = context.WithCancel(pctx)

	if s.clientState.InputAudioFormat.SampleRate <= 0 || s.clientState.InputAudioFormat.Channels <= 0 {
		return fmt.Errorf("输入音频格式未初始化，请先完成 hello 握手")
	}

	err := s.InitAsrLlmTts()
	if err != nil {
		log.Errorf("Failed to init ASR/LLM/TTS: %v", err)
		return err
	}

	// Load history asynchronously without blocking session start
	go func() {
		err := s.initHistoryMessages()
		if err != nil {
			log.Errorf("Failed to init dialog history: %v", err)
		}
	}()

	if !s.vadLoopStarted {
		// Session-level idle watchdog must outlive a single ASR loop,
		// so auto mode can keep tracking connection idle time after a successful turn.
		go s.asrManager.runAudioIdleTimeoutWatchdog(s.ctx)
		s.asrManager.ProcessVadAudio(s.ctx)
		s.vadLoopStarted = true
	}

	go s.processChatText(s.ctx)  //Handle post-ASR chat messages
	go s.llmManager.Start(s.ctx) //Handle post-LLM response messages
	go s.ttsManager.Start(s.ctx) //Handle the TTS message queue
	if s.mediaPlayer != nil {
		s.mediaPlayer.AttachSession()
	}

	return nil
}

// Load chat history into memory
func (s *ChatSession) initHistoryMessages() error {
	var historyMessages []*schema.Message
	var err error

	if s.clientState.GetMemoryMode() == MemoryModeNone {
		log.Debugf("Device %s memory mode=none, skip loading history messages", s.clientState.DeviceID)
		return nil
	}

	// Pick data source from config (no priority; direct select)
	useRedis := s.shouldUseRedis()
	useManager := s.shouldUseManager()

	// Validate required field: DeviceID must not be empty
	if s.clientState.DeviceID == "" {
		log.Debugf("DeviceID empty, skip loading history messages (may be called before hello)")
		return nil
	}

	// Pick data source from config (no priority; direct select)
	if useRedis {
		// Load from Redis
		historyMessages, err = llm_memory.Get().GetMessages(
			s.ctx,
			s.clientState.DeviceID,
			s.clientState.AgentID,
			20)
		if err != nil {
			log.Warnf("Failed to load history messages from Redis: %v", err)
			return err
		}
		log.Infof("Loaded %d history messages from Redis", len(historyMessages))
	} else if useManager {
		// Load from Manager
		historyMessages, err = s.loadFromManager()
		if err != nil {
			log.Warnf("Failed to load history messages from Manager: %v", err)
			return err
		}
		log.Infof("Loaded %d history messages from Manager", len(historyMessages))
	} else {
		// Neither data source configured; skip loading history
		log.Debugf("Neither Redis nor Manager configured, skip loading history messages")
		return nil
	}

	if len(historyMessages) > 0 {
		s.clientState.InitMessages(historyMessages)
		log.Infof("Successfully loaded %d history messages", len(historyMessages))
	} else {
		log.Debugf("No history messages loaded (may have none)")
	}

	return nil
}

// shouldUseRedis reports whether Redis is the data source
func (s *ChatSession) shouldUseRedis() bool {
	// Decide from config_provider.type
	providerType := viper.GetString("config_provider.type")
	return providerType == "redis"
}

// shouldUseManager reports whether Manager is the data source
func (s *ChatSession) shouldUseManager() bool {
	// Decide from config_provider.type
	providerType := viper.GetString("config_provider.type")
	return providerType == "manager"
}

// loadFromManager loads history from the Manager DB
func (s *ChatSession) loadFromManager() ([]*schema.Message, error) {
	// Create HistoryClient
	historyCfg := history.HistoryClientConfig{
		BaseURL:   util.GetBackendURL(),
		AuthToken: util.GetManagerAuthToken(),
		Timeout:   viper.GetDuration("manager.history_timeout"),
		Enabled:   true,
	}
	client := history.NewHistoryClient(historyCfg)

	if s.clientState.DeviceID == "" || s.clientState.AgentID == "" {
		return []*schema.Message{}, nil
	}

	req := &history.GetMessagesRequest{
		DeviceID:  s.clientState.DeviceID,
		AgentID:   s.clientState.AgentID,
		SessionID: s.clientState.SessionID,
		Limit:     20,
	}

	resp, err := client.GetMessages(s.ctx, req)
	if err != nil {
		return nil, err
	}

	// Convert to schema.Message format
	messages := make([]*schema.Message, 0, len(resp.Messages))
	for _, item := range resp.Messages {
		var msg *schema.Message
		switch item.Role {
		case "user":
			msg = schema.UserMessage(item.Content)
		case "assistant":
			msg = schema.AssistantMessage(item.Content, item.ToolCalls)
		case "tool":
			msg = schema.ToolMessage(item.Content, item.ToolCallID)
		case "system":
			msg = schema.SystemMessage(item.Content)
		default:
			log.Warnf("Unknown message role: %s", item.Role)
			continue
		}

		messages = append(messages, msg)
	}

	for _, msg := range messages {
		log.Debugf("History messages: %+v", msg)
	}

	return messages, nil
}

// Run after MQTT receives type: listen, state: start
func (c *ChatSession) InitAsrLlmTts() error {
	//Initialize ASR struct
	c.clientState.InitAsr()

	// Initialize memory (memory is not from the resource pool)
	memoryMode := c.clientState.GetMemoryMode()
	memoryConfig := c.clientState.DeviceConfig.Memory
	memoryType := memory.MemoryType(memoryConfig.Provider)
	if memoryMode != MemoryModeLong {
		memoryType = memory.MemoryTypeNone
	}

	memoryProvider, err := memory.GetProvider(memoryType, memoryConfig.Config)
	if err != nil {
		return fmt.Errorf("创建 Memory 提供者失败: %v", err)
	}
	c.clientState.MemoryProvider = memoryProvider

	if memoryMode == MemoryModeLong {
		// Initialize memory context (long-term memory mode only)
		context, err := memoryProvider.GetContext(c.ctx, c.clientState.GetDeviceIDOrAgentID(), 500)
		if err != nil {
			log.Warnf("Failed to init memory context: %v", err)
		}
		c.clientState.MemoryContext = context
	} else {
		c.clientState.MemoryContext = ""
	}

	return nil
}

// HandleAudioMessage handles audio messages
func (c *ChatSession) HandleAudioMessage(data []byte) bool {
	select {
	case c.clientState.OpusAudioBuffer <- data:
		return true
	default:
		log.Warnf("Audio buffer full, drop audio data")
	}
	return false
}

// handleListenMessage handles listen messages
func (s *ChatSession) HandleListenMessage(msg *ClientMessage) error {
	// Handle by state
	switch msg.State {
	case MessageStateStart:
		s.HandleListenStart(msg)
	case MessageStateStop:
		s.HandleListenStop()
	case MessageStateDetect:
		s.HandleListenDetect(msg)
	}

	// Log
	log.Infof("Device %s update audio listen state: %s", msg.DeviceID, msg.State)
	return nil
}

func (s *ChatSession) beginListenStart() uint64 {
	startSeq := s.listenStartSeq.Add(1)
	if s.clientState.IsRealTime() {
		s.realtimeListenSessionActive.Store(true)
	}
	s.clientState.SetListenPhase(ListenPhaseStarting)
	return startSeq
}

func (s *ChatSession) invalidateListenStart() {
	s.listenStartSeq.Add(1)
	s.realtimeListenSessionActive.Store(false)
	s.clientState.SetListenPhase(ListenPhaseIdle)
}

func (s *ChatSession) isCurrentListenStart(startSeq uint64) bool {
	return startSeq == s.listenStartSeq.Load()
}

func (s *ChatSession) isRealtimeListenSessionActive() bool {
	return s.realtimeListenSessionActive.Load()
}

func (s *ChatSession) shouldIgnoreListenStartError(startSeq uint64, ctx context.Context, err error) bool {
	if !s.isCurrentListenStart(startSeq) {
		return true
	}
	if ctx != nil && ctx.Err() != nil {
		return true
	}
	if s.clientState.Ctx.Err() != nil {
		return true
	}
	return errors.Is(err, context.Canceled)
}

func (s *ChatSession) shouldIgnoreAsrLoopError(startSeq uint64, ctx context.Context, err error) bool {
	if !s.isCurrentListenStart(startSeq) {
		return true
	}
	if ctx != nil && ctx.Err() != nil {
		return true
	}
	if s.clientState.Ctx.Err() != nil {
		return true
	}
	return errors.Is(err, context.Canceled)
}

func isAutoListenActive(state *ClientState) bool {
	if state == nil || state.ListenMode != "auto" {
		return false
	}
	phase := state.GetListenPhase()
	return phase == ListenPhaseStarting || phase == ListenPhaseListening
}

func shouldIgnoreListenStartDuringWelcome(mode string, welcomePlaying bool) bool {
	return mode != "realtime" && welcomePlaying
}

func shouldWaitRealtimeListenStartDuringWelcome(mode string, welcomePlaying bool) bool {
	return false
}

func shouldInterruptOutputOnListenStart(mode string, welcomePlaying bool) bool {
	if mode == "realtime" && welcomePlaying {
		return false
	}
	return true
}

func completeWelcomePlaybackWaitCh(ch chan welcomePlaybackResult, natural bool) {
	if ch == nil {
		return
	}
	select {
	case ch <- welcomePlaybackResult{natural: natural}:
	default:
	}
	close(ch)
}

func (s *ChatSession) beginWelcomePlaybackWait() {
	if s == nil {
		return
	}

	s.welcomePlaybackMu.Lock()
	staleCh := s.welcomePlaybackDoneCh
	s.welcomePlaybackDoneCh = make(chan welcomePlaybackResult, 1)
	s.welcomePlaybackMu.Unlock()

	if staleCh != nil {
		completeWelcomePlaybackWaitCh(staleCh, false)
	}
}

func (s *ChatSession) completeWelcomePlaybackWait(natural bool) {
	if s == nil {
		return
	}

	s.welcomePlaybackMu.Lock()
	ch := s.welcomePlaybackDoneCh
	s.welcomePlaybackDoneCh = nil
	s.welcomePlaybackMu.Unlock()

	completeWelcomePlaybackWaitCh(ch, natural)
}

func (s *ChatSession) currentWelcomePlaybackWaitCh() <-chan welcomePlaybackResult {
	if s == nil {
		return nil
	}

	s.welcomePlaybackMu.Lock()
	ch := s.welcomePlaybackDoneCh
	s.welcomePlaybackMu.Unlock()
	return ch
}

func (s *ChatSession) waitForWelcomePlaybackCompletion() bool {
	if s == nil {
		return true
	}

	doneCh := s.currentWelcomePlaybackWaitCh()
	if doneCh == nil {
		return true
	}

	var sessionDone <-chan struct{}
	if s.ctx != nil {
		sessionDone = s.ctx.Done()
	}

	log.Infof("Device %s realtime listen start waiting for welcome TTS to finish", s.clientState.DeviceID)

	select {
	case result, ok := <-doneCh:
		if !ok {
			log.Infof("Device %s welcome wait channel closed, cancel realtime listen start", s.clientState.DeviceID)
			return false
		}
		if !result.natural {
			log.Infof("Device %s welcome interrupted, cancel this realtime listen start", s.clientState.DeviceID)
			return false
		}
		log.Infof("Device %s welcome finished, continue realtime listen start", s.clientState.DeviceID)
		return true
	case <-s.clientState.Ctx.Done():
		log.Debugf("Device %s client ctx canceled, stop waiting for realtime listen start", s.clientState.DeviceID)
		return false
	case <-sessionDone:
		log.Debugf("Device %s session ctx canceled, stop waiting for realtime listen start", s.clientState.DeviceID)
		return false
	}
}

func resolveDetectAction(text string, enableGreeting bool, welcomeAlreadySpoken bool, autoListenActive bool) detectAction {
	if text == "" {
		return detectActionSilent
	}
	if enableGreeting && isWakeupWord(text) {
		if !welcomeAlreadySpoken {
			return detectActionWelcome
		}
		if autoListenActive {
			return detectActionSilent
		}
		return detectActionLLM
	}
	return detectActionLLM
}

func (s *ChatSession) cancelPendingDetectLLM() {
	if s == nil {
		return
	}

	s.detectLLMDebounceMu.Lock()
	timer := s.detectLLMDebounceTimer
	s.detectLLMDebounceTimer = nil
	s.detectLLMDebounceMu.Unlock()

	if timer != nil {
		timer.Stop()
	}
}

func (s *ChatSession) scheduleDetectLLM(text string) {
	if s == nil {
		return
	}

	text = strings.TrimSpace(text)
	if text == "" {
		return
	}

	s.cancelPendingDetectLLM()

	var timer *time.Timer
	timer = time.AfterFunc(detectLLMDebounceDuration, func() {
		s.detectLLMDebounceMu.Lock()
		if s.detectLLMDebounceTimer != timer {
			s.detectLLMDebounceMu.Unlock()
			return
		}
		s.detectLLMDebounceTimer = nil
		s.detectLLMDebounceMu.Unlock()

		if s.IsClosing() || s.clientState == nil {
			return
		}
		if s.clientState.Ctx != nil && s.clientState.Ctx.Err() != nil {
			return
		}

		if phase := s.clientState.GetListenPhase(); phase != ListenPhaseIdle {
			log.Debugf("Detect LLM debounce skipped because listen phase=%s", phase)
			return
		}

		if err := s.AddAsrResultToQueue(text, nil); err != nil {
			log.Errorf("Detect LLM debounce enqueue failed: %v", err)
		}
	})

	s.detectLLMDebounceMu.Lock()
	s.detectLLMDebounceTimer = timer
	s.detectLLMDebounceMu.Unlock()
}

func (s *ChatSession) HandleListenDetect(msg *ClientMessage) error {
	// On a new detect, cancel any pending detect->LLM debounce,
	// so old preamble text is not queued to the LLM again later.
	s.cancelPendingDetectLLM()

	// Check device activation status
	isActivated, err := s.CheckDeviceActivated()
	if err != nil {
		log.Errorf("Failed to check device activation status: %v", err)
		return err
	}
	if !isActivated {
		return nil
	}

	// Snapshot command history from before this detect, then record the detect,
	// so later logs show the previous command, not this detect itself.
	now := time.Now()
	prevHistory := s.clientState.GetCommandHistorySnapshot()
	s.clientState.RecordCommandArrival(CommandTypeDetect, now)

	// listen detect means the device saw possible preamble text,
	// do not enter formal listen yet; decide welcome, silent ignore, or delayed LLM.
	if msg.Text != "" {
		text := removePunctuation(msg.Text)
		enableGreeting := viper.GetBool("enable_greeting")
		autoListenActive := isAutoListenActive(s.clientState)
		// Wake-word handling has three cases:
		// 1. First wake with welcome allowed: play welcome;
		// 2. Welcome already played and already in auto listen: ignore repeat wake;
		// 3. Otherwise: buffer as normal detect -> LLM text.
		action := resolveDetectAction(text, enableGreeting, s.clientState.IsWelcomeSpeaking, autoListenActive)

		log.Debugf(
			"Detect recv: device=%s text=%q action=%s autoListenActive=%v history={%s} welcomeSpeaking=%v welcomePlaying=%v",
			msg.DeviceID,
			text,
			action,
			autoListenActive,
			prevHistory.DebugString(now),
			s.clientState.IsWelcomeSpeaking,
			s.clientState.IsWelcomePlaying,
		)

		if action == detectActionSilent {
			return nil
		}

		// When detect will play welcome or take over, stop residual output first,
		// so prior TTS/LLM does not overlap the new detect action.
		s.StopSpeakingWithReason(true, fmt.Sprintf("HandleListenDetect action=%s text=%q", action, text))

		if action == detectActionWelcome {
			s.HandleWelcome()
			return nil
		}

		if action == detectActionLLM {
			// Debounce detect-stage text briefly;
			// if listen start arrives soon, formal listen takes over.
			s.scheduleDetectLLM(text)
		}
	}
	return nil
}

func (s *ChatSession) HandleNotActivated() {
	configProvider, err := user_config.GetProvider(viper.GetString("config_provider.type"))
	if err != nil {
		log.Errorf("Failed to get config provider: %v", err)
		return
	}

	code, challenge, message, timeoutMs := configProvider.GetActivationInfo(s.clientState.Ctx, s.clientState.DeviceID, "client_id")
	if code == "" {
		log.Errorf("Failed to get activation info: %v", err)
		return
	}

	log.Infof("Activation code: %s, challenge: %s, message: %s, timeout: %d", code, challenge, message, timeoutMs)

	s.ttsManager.EnqueueTtsStartWithReason(s.clientState.Ctx, "HandleNotActivated")
	defer s.ttsManager.EnqueueTtsStopWithReason(s.clientState.Ctx, "HandleNotActivated")

	sessionCtx := s.clientState.SessionCtx.Get(s.clientState.Ctx)
	ctx := s.clientState.AfterAsrSessionCtx.Get(sessionCtx)
	err = s.ttsManager.handleTextResponse(ctx, llm_common.LLMResponseStruct{
		Text: fmt.Sprintf("请在后台添加设备，激活码: %s", code),
	}, false)
	s.ttsManager.RequestTurnEnd(ctx, err)

}

func (s *ChatSession) HandleWelcome() {
	greetingText := s.GetRandomGreeting()

	s.stopSpeakingMu.Lock()
	defer s.stopSpeakingMu.Unlock()

	if s.clientState.Ctx.Err() != nil {
		log.Debugf("HandleWelcome client ctx canceled, skip welcome")
		return
	}

	sessionCtx := s.clientState.SessionCtx.Get(s.clientState.Ctx)
	ctx := s.clientState.AfterAsrSessionCtx.Get(sessionCtx)
	if ctx.Err() != nil {
		log.Debugf("HandleWelcome afterAsr ctx canceled, skip welcome")
		return
	}

	s.clientState.IsWelcomeSpeaking = true
	s.clientState.IsWelcomePlaying = true
	s.beginWelcomePlaybackWait()

	go func(ctx context.Context, greetingText string) {
		if ctx.Err() != nil || s.clientState.Ctx.Err() != nil {
			s.completeWelcomePlaybackWait(false)
			return
		}

		s.ttsManager.EnqueueTtsStartWithReason(s.clientState.Ctx, "HandleWelcome")
		err := s.ttsManager.handleTextResponse(ctx, llm_common.LLMResponseStruct{Text: greetingText}, true)
		s.ttsManager.EnqueueTtsStopWithReason(s.clientState.Ctx, "HandleWelcome natural end")
		s.ttsManager.RequestTurnEnd(ctx, err)
	}(ctx, greetingText)
}

func (a *ChatSession) checkExitWords(text string) bool {
	exitWords := []string{"再见", "退下吧", "退出", "退出对话"}
	for _, word := range exitWords {
		if strings.Contains(text, word) {
			return true
		}
	}
	return false
}

func normalizeOpenClawKeywordText(text string) string {
	return removePunctuation(strings.ToLower(strings.TrimSpace(text)))
}

func containsOpenClawKeyword(text string, keywords []string) bool {
	normalizedText := normalizeOpenClawKeywordText(text)
	if normalizedText == "" {
		return false
	}
	for _, keyword := range keywords {
		normalizedKeyword := normalizeOpenClawKeywordText(keyword)
		if normalizedKeyword == "" {
			continue
		}
		if strings.Contains(normalizedText, normalizedKeyword) {
			return true
		}
	}
	return false
}

func (s *ChatSession) isOpenClawEnterKeyword(text string) bool {
	return containsOpenClawKeyword(text, s.clientState.DeviceConfig.OpenClaw.EnterKeywords)
}

func (s *ChatSession) isOpenClawExitKeyword(text string) bool {
	return containsOpenClawKeyword(text, s.clientState.DeviceConfig.OpenClaw.ExitKeywords)
}

func openClawLogSnippet(text string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	runes := []rune(trimmed)
	if len(runes) <= maxRunes {
		return string(runes)
	}
	return string(runes[:maxRunes]) + "..."
}

func (s *ChatSession) GetRandomGreeting() string {
	greetingList := viper.GetStringSlice("greeting_list")
	if len(greetingList) == 0 {
		return "你好，有啥好玩的."
	}
	rand.Seed(time.Now().UnixNano())
	return greetingList[rand.Intn(len(greetingList))]
}

func (s *ChatSession) AddTextToTTSQueue(text string) error {
	return s.llmManager.AddTextToTTSQueue(text)
}

func (s *ChatSession) AddTextToTTSQueueWithOptions(text string, options llmResponseChannelOptions) error {
	return s.llmManager.AddTextToTTSQueueWithOptions(text, options)
}

func (s *ChatSession) IsTTSActive() bool {
	if s == nil || s.ttsManager == nil {
		return false
	}
	return s.ttsManager.ttsActive.Load()
}

func (s *ChatSession) getOrCreateOpenClawStream(correlationID string) (chan llm_common.LLMResponseStruct, bool, error) {
	correlationID = strings.TrimSpace(correlationID)
	if correlationID == "" {
		return nil, false, fmt.Errorf("missing correlation_id")
	}

	s.openClawStreamMu.Lock()
	if existing, ok := s.openClawStreams[correlationID]; ok {
		s.openClawStreamMu.Unlock()
		return existing, false, nil
	}
	streamChan := make(chan llm_common.LLMResponseStruct, 16)
	s.openClawStreams[correlationID] = streamChan
	s.openClawStreamMu.Unlock()

	sessionCtx := s.clientState.SessionCtx.Get(s.clientState.Ctx)
	ctx := s.clientState.AfterAsrSessionCtx.Get(sessionCtx)
	options := llmResponseChannelOptions{}
	hasWarmup := s.getOpenClawWarmupTask(correlationID) != nil
	if hasWarmup {
		options.disableTTSCommands = true
		options.onEndFunc = func(err error, args ...any) {
			// Warmup took the start; send stop here when the real OpenClaw reply finishes;
			// do not send it at the warmup switch or the main reply is cut mid-stream.
			if !s.clientState.IsRealTime() {
				s.ttsManager.EnqueueTtsStopWithReason(ctx, fmt.Sprintf("OpenClaw stream end correlation_id=%s", correlationID))
			}
			s.ttsManager.RequestTurnEnd(ctx, err)
			s.finishOpenClawWarmup(correlationID, false)
		}
	}
	log.Infof("OpenClaw stream created: device=%s correlation_id=%s warmup_attached=%v", s.clientState.DeviceID, correlationID, hasWarmup)
	if err := s.llmManager.HandleLLMResponseChannelAsyncWithOptions(ctx, nil, streamChan, options); err != nil {
		if hasWarmup && !s.clientState.IsRealTime() {
			s.ttsManager.EnqueueTtsStopWithReason(ctx, fmt.Sprintf("OpenClaw stream setup failed correlation_id=%s", correlationID))
		}
		if hasWarmup {
			s.ttsManager.RequestTurnEnd(ctx, err)
		}
		s.openClawStreamMu.Lock()
		delete(s.openClawStreams, correlationID)
		s.openClawStreamMu.Unlock()
		close(streamChan)
		return nil, false, err
	}

	return streamChan, true, nil
}

func (s *ChatSession) closeOpenClawStream(correlationID string) {
	correlationID = strings.TrimSpace(correlationID)
	if correlationID == "" {
		return
	}
	s.openClawStreamMu.Lock()
	delete(s.openClawStreams, correlationID)
	s.openClawStreamMu.Unlock()
}

func (s *ChatSession) clearOpenClawStreams() {
	s.openClawStreamMu.Lock()
	s.openClawStreams = make(map[string]chan llm_common.LLMResponseStruct)
	s.openClawStreamMu.Unlock()
}

func (s *ChatSession) clearPendingSpeakerResult() {
	if s == nil {
		return
	}

	s.speakerResultMu.Lock()
	s.pendingSpeakerResult = nil
	s.speakerResultMu.Unlock()

	for {
		select {
		case <-s.speakerResultReady:
		default:
			return
		}
	}
}

func (s *ChatSession) InjectOpenClawResponse(event openclaw.ResponseDelivery) error {
	correlationID := strings.TrimSpace(event.CorrelationID)
	text := strings.TrimSpace(event.Text)

	// Non-streaming fallback: inject as a single sentence when there is no correlation_id.
	if correlationID == "" {
		if text == "" {
			return nil
		}
		return s.AddTextToTTSQueue(text)
	}

	// Skip empty mid-fragments; keep empty end fragments for finish.
	if text == "" && !event.IsEnd {
		return nil
	}

	streamChan, created, err := s.getOrCreateOpenClawStream(correlationID)
	if err != nil {
		return err
	}

	isStart := event.IsStart
	if created && !isStart {
		// If the first fragment lacks start, start the first segment as fallback.
		isStart = true
	}
	if isStart {
		if task := s.getOpenClawWarmupTask(correlationID); task != nil {
			if text != "" {
				// Stop warmup only when the first playable body arrives, not on a too-short preamble fragment.
				// Warmup's own first-segment flag is only for warmup TTS; do not swallow IsStart of the real reply,
				// or the real reply degrades to single-sentence TTS and later snapshots replay as a second sentence.
				s.cancelOpenClawWarmup(correlationID, false)
				s.beginOpenClawSpeech(task)
			} else {
				isStart = false
			}
		}
	} else if event.IsEnd {
		s.cancelOpenClawWarmup(correlationID, false)
	}

	resp := llm_common.LLMResponseStruct{
		Text:    text,
		IsStart: isStart,
		IsEnd:   event.IsEnd,
	}

	select {
	case <-s.ctx.Done():
		return fmt.Errorf("chat session closed")
	case streamChan <- resp:
	}

	if event.IsEnd {
		s.closeOpenClawStream(correlationID)
	}

	return nil
}

// InterruptAndClearTTSQueue interrupts TTS and clears the send queue (e.g. realtime VAD barge-in)
func (s *ChatSession) InterruptAndClearTTSQueue() {
	s.InterruptAndClearTTSQueueWithReason("ChatSession.InterruptAndClearTTSQueue")
}

func (s *ChatSession) InterruptAndClearTTSQueueWithReason(reason string) {
	log.Infof("interrupt and clear tts queue requested: device=%s reason=%s state={%s}", s.clientState.DeviceID, normalizeTTSReason(reason), s.ttsManager.debugState())
	if s.mediaPlayer != nil {
		if err := s.mediaPlayer.Suspend(); err != nil && !errors.Is(err, context.Canceled) {
			log.Warnf("Failed to suspend media playback: %v", err)
		}
	}
	s.ttsManager.ClearTTSQueue()
	s.ttsManager.InterruptAndStopWithReason(s.clientState.Ctx, true, context.Canceled, reason)
}

// handleAbortMessage handles abort messages
func (s *ChatSession) HandleAbortMessage(msg *ClientMessage) error {
	s.cancelPendingDetectLLM()

	// Set interrupt state
	s.clientState.Abort = true

	if s.clientState.IsRealTime() {
		s.StopAssistantOutputAfterAsrWithReason(true, "HandleAbortMessage realtime")
	} else {
		s.StopSpeakingWithReason(true, "HandleAbortMessage auto")
	}

	// Log
	log.Infof("Device %s abort session", msg.DeviceID)
	return nil
}

func (s *ChatSession) CheckDeviceActivated() (bool, error) {
	if viper.GetBool("auth.enable") {
		if !s.clientState.IsActivated {
			const falseCheckThrottle = time.Second
			s.activationCheckMu.Lock()
			lastFalseAt := s.lastActivationFalseAt
			s.activationCheckMu.Unlock()
			if !lastFalseAt.IsZero() && time.Since(lastFalseAt) < falseCheckThrottle {
				log.Debugf("Device %s still inactive, skip duplicate realtime activation check", s.clientState.DeviceID)
				return false, nil
			}

			configProvider, err := user_config.GetProvider(viper.GetString("config_provider.type"))
			if err != nil {
				log.Errorf("Failed to get config provider: %v", err)
				return false, err
			}
			//Call API again to confirm activation status
			isActivated, err := configProvider.IsDeviceActivated(s.clientState.Ctx, s.clientState.DeviceID, "client_id")
			if err != nil {
				log.Errorf("Failed to get activation status: %v", err)
				return false, err
			}
			if isActivated {
				s.clientState.IsActivated = true
				s.activationCheckMu.Lock()
				s.lastActivationFalseAt = time.Time{}
				s.activationCheckMu.Unlock()
			} else {
				s.activationCheckMu.Lock()
				s.lastActivationFalseAt = time.Now()
				s.activationCheckMu.Unlock()
				s.HandleNotActivated()
				return false, nil
			}
		}
	}
	return true, nil
}

func (s *ChatSession) HandleListenStart(msg *ClientMessage) error {
	s.cancelPendingDetectLLM()

	// Check activation status first
	isActivated, err := s.CheckDeviceActivated()
	if err != nil {
		log.Errorf("Failed to check device activation status: %v", err)
		return err
	}
	if !isActivated {
		return nil
	}

	now := time.Now()
	prevHistory := s.clientState.GetCommandHistorySnapshot()

	// In auto/manual, the device may auto-send listen start during welcome playback;
	// those packets must not preempt welcome, so ignore them while welcome is playing.
	if shouldIgnoreListenStartDuringWelcome(msg.Mode, s.clientState.IsWelcomePlaying) {
		log.Infof("Device %s welcome playing, ignore listen start: history={%s}", msg.DeviceID, prevHistory.DebugString(now))
		return nil
	}

	log.Debugf(
		"ListenStart recv: device=%s mode=%s history={%s} welcomeSpeaking=%v welcomePlaying=%v phase=%s",
		msg.DeviceID,
		msg.Mode,
		prevHistory.DebugString(now),
		s.clientState.IsWelcomeSpeaking,
		s.clientState.IsWelcomePlaying,
		s.clientState.GetListenPhase(),
	)

	// realtime vs auto/manual goals differ:
	// realtime is a long-lived listen session and avoids breaking the current chain;
	// auto/manual starts a new formal capture round, resetting output and restarting ASR.
	if msg.Mode == "realtime" {
		// While the realtime listen session has not reached listen stop / session cancel / close,
		// silently ignore duplicate listen start packets to avoid breaking the chain.
		if s.clientState.IsRealTime() && s.isRealtimeListenSessionActive() {
			return nil
		}

		// On first realtime entry, if welcome is still playing, wait for it to finish;
		// enter realtime listen only after welcome finishes fully.
		if shouldWaitRealtimeListenStartDuringWelcome(msg.Mode, s.clientState.IsWelcomePlaying) {
			if !s.waitForWelcomePlaybackCompletion() {
				return nil
			}
		}

		s.clientState.RecordCommandArrival(CommandTypeListenStart, now)
		if shouldInterruptOutputOnListenStart(msg.Mode, s.clientState.IsWelcomePlaying) {
			// Outside welcome protection, listen start means a new listen takeover,
			// so stop current TTS/LLM to avoid talking and listening at once.
			s.StopSpeakingWithReason(true, fmt.Sprintf("HandleListenStart mode=%s", msg.Mode))
		}

		s.clientState.ListenMode = msg.Mode
		log.Infof("Device %s listen mode: %s", msg.DeviceID, msg.Mode)

		shouldStartAudioIdleWindow := s.clientState.GetListenPhase() != ListenPhaseListening
		startSeq := s.beginListenStart()
		go func() {
			if err := s.OnListenStart(startSeq, shouldStartAudioIdleWindow); err != nil {
				log.Errorf("Device %s listen start failed: %v", msg.DeviceID, err)
			}
		}()
		return nil
	}

	if s.clientState.GetListenPhase() == ListenPhaseStarting {
		log.Infof("Device %s listen start already starting, ignore duplicate listen start", msg.DeviceID)
		return nil
	}

	s.clientState.RecordCommandArrival(CommandTypeListenStart, now)

	// Entering here in auto/manual means explicitly starting a new capture flow:
	// update mode, stop old output, then async OnListenStart for ASR init.
	s.clientState.ListenMode = msg.Mode
	log.Infof("Device %s listen mode: %s", msg.DeviceID, msg.Mode)
	s.StopSpeakingWithReason(true, fmt.Sprintf("HandleListenStart mode=%s", msg.Mode))

	startSeq := s.beginListenStart()
	go func() {
		if err := s.OnListenStart(startSeq, true); err != nil {
			log.Errorf("Device %s listen start failed: %v", msg.DeviceID, err)
		}
	}()

	return nil
}

func (s *ChatSession) HandleListenStop() error {
	s.cancelPendingDetectLLM()
	s.clientState.RecordCommandArrival(CommandTypeListenStop, time.Now())
	/*if s.clientState.ListenMode == "auto" {
		s.clientState.CancelSessionCtx()
	}*/

	//Invoke
	if s.clientState.IsRealTime() {
		s.invalidateListenStart()
	}
	s.clientState.OnManualStop()

	return nil
}

func (s *ChatSession) OnListenStart(startSeq uint64, shouldStartAudioIdleWindow bool) error {
	log.Debugf("OnListenStart start")
	defer log.Debugf("OnListenStart end")

	if !s.isCurrentListenStart(startSeq) {
		log.Debugf("OnListenStart stale before init, skip")
		return nil
	}

	select {
	case <-s.clientState.Ctx.Done():
		log.Debugf("OnListenStart Ctx done, return")
		if s.isCurrentListenStart(startSeq) {
			s.clientState.SetListenPhase(ListenPhaseIdle)
		}
		return nil
	default:
	}

	// realtime mode: skip Destroy, keep ASR running, but clear AudioBuffer
	var ctx context.Context
	if s.clientState.IsRealTime() {
		s.clientState.AsrAudioBuffer.ClearAsrAudioData()
	} else {
		s.stopSpeakingMu.Lock()
		if !s.isCurrentListenStart(startSeq) {
			s.stopSpeakingMu.Unlock()
			log.Debugf("OnListenStart stale before destroy, skip")
			return nil
		}
		s.clientState.Destroy()
		if !s.isCurrentListenStart(startSeq) {
			s.stopSpeakingMu.Unlock()
			log.Debugf("OnListenStart stale after destroy, skip")
			return nil
		}

		s.clientState.SetListenPhase(ListenPhaseStarting)
		s.clientState.SetStatus(ClientStatusListening)
		ctx = s.clientState.SessionCtx.Get(s.clientState.Ctx)

		// ASR state init must stay consistent with session context rebuild.
		if s.clientState.ListenMode == "manual" {
			s.clientState.VoiceStatus.SetClientHaveVoice(true)
		}
		s.stopSpeakingMu.Unlock()
	}

	if s.clientState.IsRealTime() {
		s.clientState.SetListenPhase(ListenPhaseStarting)
		s.clientState.SetStatus(ClientStatusListening)
		ctx = s.clientState.SessionCtx.Get(s.clientState.Ctx)

		//Initialize ASR-related state
		if s.clientState.ListenMode == "manual" {
			s.clientState.VoiceStatus.SetClientHaveVoice(true)
		}
	}

	// Start streaming ASR; reuse restartAsrRecognition
	if !s.isCurrentListenStart(startSeq) {
		log.Debugf("OnListenStart stale before ASR restart, skip")
		return nil
	}
	err := s.asrManager.RestartAsrRecognition(ctx)
	if err != nil {
		if s.shouldIgnoreListenStartError(startSeq, ctx, err) {
			log.Infof("OnListenStart interrupted during ASR restart, ignore err: %v", err)
			if s.isCurrentListenStart(startSeq) {
				s.clientState.SetListenPhase(ListenPhaseIdle)
			}
			return nil
		}

		log.Errorf("ASR streaming recognition failed: %v", err)
		if s.isCurrentListenStart(startSeq) {
			s.clientState.SetListenPhase(ListenPhaseIdle)
		}
		s.CloseWithReason(chatSessionCloseReasonFatalError)
		return err
	}

	if !s.isCurrentListenStart(startSeq) {
		log.Debugf("OnListenStart stale after ASR restart, cancel current start")
		s.clientState.Asr.CancelWithReason("ChatSession.OnListenStart: stale listen start after ASR restart")
		return nil
	}

	s.clientState.SetListenPhase(ListenPhaseListening)
	if shouldStartAudioIdleWindow {
		s.clientState.StartAudioIdleWindow(time.Now())
	}

	// Define message-save callback
	onMessageSave := func(userMsg *schema.Message, messageID string, audioData []float32) {
		// ASR text and audio arrive together; save once (no two-phase)
		eventbus.Get().Publish(eventbus.TopicAddMessage, &eventbus.AddMessageEvent{
			ClientState: s.clientState,
			Msg:         *userMsg,
			MessageID:   messageID,
			AudioData:   [][]byte{util.Float32SliceToBytes(audioData)}, // convert to byte slice
			AudioSize:   len(audioData) * 4,                            // float32 = 4 bytes
			SampleRate:  s.clientState.InputAudioFormat.SampleRate,
			Channels:    s.clientState.InputAudioFormat.Channels,
			IsUpdate:    false, // one-shot save (text+audio)
			Timestamp:   time.Now(),
		})
	}

	// Define error-handling callback
	onError := func(err error) {
		if s.shouldIgnoreAsrLoopError(startSeq, ctx, err) {
			log.Infof("ASR recognition loop ended during reset/exit, ignore err: %v", err)
			return
		}
		log.Errorf("ASR recognition loop error: %v", err)
		s.CloseWithReason(chatSessionCloseReasonFatalError)
	}

	// Start ASR result loop (resource mgmt inside ASRManager)
	s.asrManager.StartAsrRecognitionLoop(ctx, onMessageSave, onError)

	return nil
}

// startChat starts the conversation
func (s *ChatSession) AddAsrResultToQueue(text string, speakerResult *speaker.IdentifyResult) error {
	return s.AddAsrResultToQueueWithOptions(text, speakerResult, llmResponseChannelOptions{})
}

func (s *ChatSession) AddAsrResultToQueueWithOptions(text string, speakerResult *speaker.IdentifyResult, options llmResponseChannelOptions) error {
	log.Debugf("AddAsrResultToQueue text: %s", text)
	if speakerResult != nil && speakerResult.Identified {
		log.Debugf("AddAsrResultToQueue speaker: %s (confidence: %.2f)", speakerResult.SpeakerName, speakerResult.Confidence)
	}

	// Check whether session was stopped (via try-lock)
	// If StopSpeaking is running, wait; if done, tryLock returns immediately
	if !s.stopSpeakingMu.TryLock() {
		log.Debugf("AddAsrResultToQueue StopSpeaking in progress, drop message")
		return nil
	}
	s.stopSpeakingMu.Unlock()

	sessionCtx := s.clientState.SessionCtx.Get(s.clientState.Ctx)
	// Check whether sessionCtx was canceled
	if sessionCtx.Err() != nil {
		log.Debugf("AddAsrResultToQueue sessionCtx canceled, drop message")
		return nil
	}
	ctx := s.clientState.AfterAsrSessionCtx.Get(sessionCtx)
	ctx = withTTSPlaybackStartHook(ctx, options.onTTSPlaybackStart)
	ctx = withTTSTurnEndPolicy(ctx, options.ttsTurnEndPolicy)

	item := AsrResponseChannelItem{
		ctx:           ctx,
		text:          text,
		speakerResult: speakerResult,
	}
	err := s.chatTextQueue.Push(item)
	if err != nil {
		log.Warnf("chatTextQueue full or closed, drop message")
	}
	return nil
}

func (s *ChatSession) processChatText(ctx context.Context) {
	log.Debugf("processChatText start")
	defer log.Debugf("processChatText end")

	for {
		item, err := s.chatTextQueue.Pop(ctx, 0)
		if err != nil {
			if err == util.ErrQueueCtxDone {
				return
			}
			continue
		}

		err = s.actionDoChat(item.ctx, item.text, item.speakerResult)
		if err != nil {
			log.Errorf("Failed to handle dialog: %v", err)
			continue
		}
	}
}

func (s *ChatSession) ClearChatTextQueue() {
	s.chatTextQueue.Clear()
}

// DoExitChat exits chat (sends goodbye and closes the session)
func (s *ChatSession) DoExitChat() {
	// Friendly goodbye text
	goodbyeText := "好的，再见！期待下次与您聊天～"

	// Save an assistant-role message
	goodbyeMsg := schema.AssistantMessage(goodbyeText, nil)
	if err := s.llmManager.AddLlmMessage(s.clientState.Ctx, goodbyeMsg); err != nil {
		log.Errorf("Failed to save goodbye message: %v", err)
	}

	// Get context
	sessionCtx := s.clientState.SessionCtx.Get(s.clientState.Ctx)
	ctx := s.clientState.AfterAsrSessionCtx.Get(sessionCtx)

	// Send TTS goodbye
	s.ttsManager.EnqueueTtsStartWithReason(ctx, "ChatSession.processGoodbye")

	err := s.ttsManager.handleTextResponse(ctx, llm_common.LLMResponseStruct{
		Text:    goodbyeText,
		IsStart: true,
		IsEnd:   true,
	}, true) // synchronous; wait for TTS to finish

	if err != nil {
		log.Errorf("Failed to send goodbye speech: %v", err)
	}

	s.ttsManager.RequestTurnEnd(ctx, err)
	s.ttsManager.EnqueueTtsStopWithReason(ctx, "ChatSession.processGoodbye")
	// Close session
	s.CloseWithReason(chatSessionCloseReasonExplicitExit)
}

func (s *ChatSession) Close() {
	s.CloseWithReason(chatSessionCloseReasonManagerShutdown)
}

func (s *ChatSession) IsClosing() bool {
	if s == nil {
		return true
	}
	return s.closing.Load()
}

func (s *ChatSession) CloseWithReason(reason string) {
	s.closing.Store(true)
	s.closeOnce.Do(func() {
		// Clean ASR resources (resource mgmt inside ASRManager)
		if s.asrManager != nil {
			s.asrManager.Cleanup()
		}
		deviceID := ""
		if s.clientState != nil {
			deviceID = s.clientState.DeviceID
		}
		log.Debugf("ChatSession.Close() starting session resource cleanup, device %s", deviceID)

		if s.mediaPlayer != nil {
			s.mediaPlayer.DetachSession(true)
		}

		s.cancelPendingDetectLLM()

		// Cancel session-level context
		if s.cancel != nil {
			s.cancel()
		}
		s.finishOpenClawWarmup("", false)

		// Clear chat text queue
		s.ClearChatTextQueue()
		s.clearOpenClawStreams()

		// Stop speaking and clean audio resources. Close path already called DetachSession(true),
		// do not Suspend media again here or resumeOnAttach is cleared.
		s.stopSpeakingWithLock(true, true, false, "ChatSession.Close")

		if s.speakerManager != nil {
			s.speakerManager.Close()
		}

		if s.clientState != nil {
			eventbus.Get().Publish(eventbus.TopicSessionEnd, s.clientState)
		}

		log.Debugf("ChatSession.Close() session resource cleanup done, device %s", deviceID)

		if s.closeHandler != nil {
			s.closeHandler(s, reason)
		}
	})
}

func (s *ChatSession) actionDoChat(ctx context.Context, text string, speakerResult *speaker.IdentifyResult) error {
	select {
	case <-ctx.Done():
		log.Debugf("actionDoChat ctx done, return")
		return nil
	default:
	}

	agentID := strings.TrimSpace(s.clientState.AgentID)
	deviceID := strings.TrimSpace(s.clientState.DeviceID)
	openclawSessionID := strings.TrimSpace(s.clientState.SessionID)
	trimmedText := strings.TrimSpace(text)

	handledByRealtimeGate, gateErr := s.tryHandleRealtimeMcpAudioASR(ctx, trimmedText)
	if handledByRealtimeGate {
		return gateErr
	}

	openclawManager := openclaw.GetManager()
	if s.clientState.DeviceConfig.OpenClaw.Allowed {
		isOpenClawMode := openclawManager.IsModeEnabled(agentID, deviceID)
		isEnterKeyword := s.isOpenClawEnterKeyword(text)
		isExitKeyword := false
		if isOpenClawMode {
			isExitKeyword = s.isOpenClawExitKeyword(text)
		}
		log.Debugf(
			"OpenClaw route decision: agent=%s device=%s session=%s allowed=%v mode=%v enter_keyword=%v exit_keyword=%v text_len=%d text_trim_len=%d text_snippet=%q",
			agentID,
			deviceID,
			openclawSessionID,
			s.clientState.DeviceConfig.OpenClaw.Allowed,
			isOpenClawMode,
			isEnterKeyword,
			isExitKeyword,
			len(text),
			len(trimmedText),
			openClawLogSnippet(trimmedText, 64),
		)
		if isOpenClawMode {
			if isExitKeyword {
				s.finishOpenClawWarmup("", true)
				exited := openclawManager.ExitMode(agentID, deviceID)
				_ = s.AddTextToTTSQueue("已退出OpenClaw模式")
				log.Infof("Device %s exit OpenClaw mode: agent=%s exited=%v", deviceID, agentID, exited)
				return nil
			}

			log.Infof(
				"OpenClaw send STT: agent=%s device=%s session=%s text_len=%d text_snippet=%q",
				agentID,
				deviceID,
				openclawSessionID,
				len(trimmedText),
				openClawLogSnippet(trimmedText, 64),
			)
			s.finishOpenClawWarmup("", true)
			messageID, err := openclawManager.SendMessage(
				agentID,
				deviceID,
				text,
				openclawSessionID,
			)
			if err != nil {
				log.Warnf(
					"Device %s OpenClaw message send failed, fell back to normal mode: agent=%s session=%s text_snippet=%q err=%v",
					deviceID,
					agentID,
					openclawSessionID,
					openClawLogSnippet(trimmedText, 64),
					err,
				)
				openclawManager.ExitMode(agentID, deviceID)
				_ = s.AddTextToTTSQueue("OpenClaw当前不可用，已退出OpenClaw模式")
			} else {
				s.startOpenClawWarmup(messageID, text)
				log.Infof("OpenClaw send STT succeeded: agent=%s device=%s session=%s message_id=%s", agentID, deviceID, openclawSessionID, messageID)
			}
			return nil
		}

		if isEnterKeyword {
			if !openclawManager.EnterMode(agentID, deviceID) {
				_ = s.AddTextToTTSQueue("OpenClaw当前不可用，请稍后再试")
				log.Warnf("Device %s enter OpenClaw mode failed: agent=%s agent session not ready", deviceID, agentID)
				return nil
			}
			_ = s.AddTextToTTSQueue("已进入OpenClaw模式，请继续说")
			log.Infof("Device %s enter OpenClaw mode: agent=%s trigger=%q", deviceID, agentID, openClawLogSnippet(trimmedText, 32))
			return nil
		}
		log.Debugf(
			"OpenClaw did not take over current STT: agent=%s device=%s mode=%v enter_keyword=%v text_snippet=%q",
			agentID,
			deviceID,
			isOpenClawMode,
			isEnterKeyword,
			openClawLogSnippet(trimmedText, 64),
		)
	} else {
		s.finishOpenClawWarmup("", false)
		if openclawManager.ExitMode(agentID, deviceID) {
			log.Debugf("OpenClaw config disabled, forced exit mode: agent=%s device=%s", agentID, deviceID)
		}
	}

	if s.checkExitWords(text) {
		// Publish exit-chat event
		eventbus.Get().Publish(eventbus.TopicExitChat, &eventbus.ExitChatEvent{
			ClientState: s.clientState,
			Reason:      "用户主动退出",
			TriggerType: "exit_words",
			UserText:    text,
			Timestamp:   time.Now(),
		})
		return nil
	}

	clientState := s.clientState

	sessionID := clientState.SessionID

	// After speaker ID, switch TTS dynamically (restore default TTS if unmatched)
	if err := s.switchTTSForSpeaker(speakerResult); err != nil {
		log.Warnf("Failed to switch TTS: %v", err)
		// Do not break the flow; keep current TTS
	}

	// Create Eino-native messages directly
	userMessage := &schema.Message{
		Role:    schema.User,
		Content: text,
	}

	// Get global MCP tool list
	mcpTools, err := mcp.GetToolsByDeviceIdWithTransport(
		clientState.DeviceID,
		clientState.AgentID,
		s.serverTransport.GetTransportType(),
		clientState.DeviceConfig.MCPServiceNames,
	)
	if err != nil {
		log.Errorf("Failed to get tools for device %s: %v", clientState.DeviceID, err)
		mcpTools = make(map[string]tool.InvokableTool)
	}
	if !hasAvailableKnowledgeBase(clientState.DeviceConfig.KnowledgeBases) {
		if _, ok := mcpTools["search_knowledge"]; ok {
			delete(mcpTools, "search_knowledge")
			log.Infof("Device %s has no usable knowledge base, removed tool search_knowledge", clientState.DeviceID)
		}
	}

	// Convert MCP tools to interface form for the converter
	mcpToolsInterface := make(map[string]interface{})
	for name, tool := range mcpTools {
		mcpToolsInterface[name] = tool
	}

	// Convert MCP tools to Eino ToolInfo format
	einoTools, err := llm.ConvertMCPToolsToEinoTools(ctx, mcpToolsInterface)
	if err != nil {
		log.Errorf("Failed to convert MCP tools: %v", err)
		einoTools = nil
	}

	toolNameList := make([]string, 0)
	for _, tool := range einoTools {
		toolNameList = append(toolNameList, tool.Name)
	}

	// Send LLM request with tools
	log.Infof("Send LLM request with %d MCP tools, tools: %+v", len(einoTools), toolNameList)

	err = s.llmManager.DoLLmRequest(ctx, userMessage, einoTools, true, speakerResult)
	if err != nil {
		log.Errorf("Failed to send LLM request with tools, seesionID: %s, error: %v", sessionID, err)
		return fmt.Errorf("发送带工具的 LLM 请求失败: %v", err)
	}
	return nil
}

func hasAvailableKnowledgeBase(knowledgeBases []types.KnowledgeBaseRef) bool {
	for _, kb := range knowledgeBases {
		if strings.EqualFold(strings.TrimSpace(kb.Status), "inactive") {
			continue
		}
		if strings.TrimSpace(kb.ExternalKBID) == "" {
			continue
		}
		return true
	}
	return false
}

func (s *ChatSession) MarkTurnSpeakerInterrupted() {
	if s == nil {
		return
	}
	s.turnSpeakerInterrupted.Store(true)
}

func (s *ChatSession) ConsumeTurnSpeakerInterrupted() bool {
	if s == nil {
		return false
	}
	return s.turnSpeakerInterrupted.Swap(false)
}

func (s *ChatSession) ResetTurnSpeakerInterrupted() {
	if s == nil {
		return
	}
	s.turnSpeakerInterrupted.Store(false)
}

func (s *ChatSession) ShouldAllowSpeakerChat(speakerResult *speaker.IdentifyResult, speakerInterrupted bool) (bool, string) {
	if s == nil || s.clientState == nil {
		return true, ""
	}

	matchedConfiguredSpeaker := s.clientState.HasMatchedConfiguredSpeaker(speakerResult)
	if speakerInterrupted && !matchedConfiguredSpeaker {
		return false, "speaker_interrupt_without_identify"
	}

	if s.clientState.RequireMatchedSpeakerForChat() && !matchedConfiguredSpeaker {
		return false, "speaker_chat_mode_identified_only_not_matched"
	}

	return true, ""
}

// switchTTSForSpeaker switches TTS for the identified speaker
func (s *ChatSession) switchTTSForSpeaker(speakerResult *speaker.IdentifyResult) error {
	s.clientState.SpeakerTTSConfig = nil

	// 1. Check whether speakerResult is nil
	if speakerResult == nil {
		log.Debug("speakerResult is nil, clear speaker TTS config")
		return nil
	}

	// 2. Look up speaker-group config
	speakerGroupInfo, found := s.clientState.DeviceConfig.VoiceIdentify[speakerResult.SpeakerName]
	if !found {
		// Config not found; clear speaker TTS config
		log.Debugf("Speaker group %s config not found, clear speaker TTS config", speakerResult.SpeakerName)
		return nil
	}

	// 3. Check whether a custom voice is configured
	if speakerGroupInfo.TTSConfigID == nil || *speakerGroupInfo.TTSConfigID == "" {
		// No custom voice; clear speaker TTS config
		log.Debugf("Speaker group %s has no custom TTS, clear speaker TTS config", speakerResult.SpeakerName)
		return nil
	}

	// 4. Find matching TTS config in system config (viper)
	var targetTTSConfig *types.TtsConfigItem
	ttsConfigsRaw := viper.Get("tts")
	if ttsConfigsRaw == nil {
		return fmt.Errorf("系统配置中未找到 tts")
	}

	// Parse tts config (map keyed by config_id)
	if ttsConfigsMap, ok := ttsConfigsRaw.(map[string]interface{}); ok {
		// Find matching config_id
		if configItem, exists := ttsConfigsMap[*speakerGroupInfo.TTSConfigID]; exists {
			if configMap, ok := configItem.(map[string]interface{}); ok {
				// Parse config entry
				ttsItem := &types.TtsConfigItem{
					ConfigID: *speakerGroupInfo.TTSConfigID,
				}
				if name, ok := configMap["name"].(string); ok {
					ttsItem.Name = name
				}
				if provider, ok := configMap["provider"].(string); ok {
					ttsItem.Provider = provider
				}
				if isDefault, ok := configMap["is_default"].(bool); ok {
					ttsItem.IsDefault = isDefault
				}
				// Other entry fields become config as-is
				ttsItem.Config = make(map[string]interface{})
				for k, v := range configMap {
					if k != "name" && k != "provider" && k != "is_default" && k != "config_id" {
						ttsItem.Config[k] = v
					}
				}
				targetTTSConfig = ttsItem
			}
		}
	}

	if targetTTSConfig == nil {
		return fmt.Errorf("未找到TTS配置 %s", *speakerGroupInfo.TTSConfigID)
	}

	// 5. Copy TTS config to avoid mutating the original
	ttsConfig := make(map[string]interface{})
	for k, v := range targetTTSConfig.Config {
		ttsConfig[k] = v
	}

	// 6. If a voice value is set, override it in TTS config
	if speakerGroupInfo.Voice != nil && *speakerGroupInfo.Voice != "" {
		// Set the voice field for the provider
		if targetTTSConfig.Provider == "cosyvoice" {
			ttsConfig["spk_id"] = *speakerGroupInfo.Voice
		} else {
			ttsConfig["voice"] = *speakerGroupInfo.Voice
		}
		log.Debugf("Set voice for speaker %s: %s", speakerResult.SpeakerName, *speakerGroupInfo.Voice)
	}

	// 7. Save full TTS config (deep copy)
	s.clientState.SpeakerTTSConfig = make(map[string]interface{})
	for k, v := range ttsConfig {
		s.clientState.SpeakerTTSConfig[k] = v
	}
	// Ensure provider is present in config
	s.clientState.SpeakerTTSConfig["provider"] = targetTTSConfig.Provider

	log.Infof("Switched TTS config for speaker %s successfully - Provider: %s, ConfigID: %s, Voice: %v",
		speakerResult.SpeakerName,
		targetTTSConfig.Provider,
		targetTTSConfig.ConfigID,
		speakerGroupInfo.Voice)

	return nil
}

func (s *ChatSession) hookContext(ctx context.Context) chathooks.Context {
	sessionID := ""
	deviceID := ""
	if s != nil && s.clientState != nil {
		sessionID = s.clientState.SessionID
		deviceID = s.clientState.DeviceID
	}

	return chathooks.Context{
		Ctx:       ctx,
		SessionID: sessionID,
		DeviceID:  deviceID,
	}
}

func (s *ChatSession) emitMetricStage(ctx context.Context, stage chathooks.MetricStage, ts int64, err error) {
	if s == nil {
		return
	}

	hookErr := s.hookHub.EmitMetric(s.hookContext(ctx), chathooks.MetricData{Stage: stage, Ts: ts, Err: err})
	if hookErr != nil {
		log.Warnf("METRIC hook failed: stage=%s err=%v", stage, hookErr)
	}
}

func (s *ChatSession) TraceTurnStart(ctx context.Context, ts int64) {
	s.emitMetricStage(ctx, chathooks.MetricTurnStart, ts, nil)
}

func (s *ChatSession) TraceTurnEnd(ctx context.Context, ts int64, err error) {
	s.emitMetricStage(ctx, chathooks.MetricTurnEnd, ts, err)
}

func (s *ChatSession) TraceVoiceSilence(ctx context.Context, ts int64) {
	s.emitMetricStage(ctx, chathooks.MetricVoiceSilence, ts, nil)
}

func (s *ChatSession) TraceAsrFirstText(ctx context.Context, ts int64) {
	s.emitMetricStage(ctx, chathooks.MetricAsrFirstText, ts, nil)
}

func (s *ChatSession) TraceAsrFinalText(ctx context.Context, ts int64) {
	s.emitMetricStage(ctx, chathooks.MetricAsrFinalText, ts, nil)
}

func (s *ChatSession) TraceLlmStart(ctx context.Context, ts int64) {
	s.emitMetricStage(ctx, chathooks.MetricLlmStart, ts, nil)
}

func (s *ChatSession) TraceLlmFirstToken(ctx context.Context, ts int64) {
	s.emitMetricStage(ctx, chathooks.MetricLlmFirstToken, ts, nil)
}

func (s *ChatSession) TraceLlmFirstSentence(ctx context.Context, ts int64) {
	s.emitMetricStage(ctx, chathooks.MetricLlmFirstSentence, ts, nil)
}

func (s *ChatSession) TraceLlmEnd(ctx context.Context, ts int64, err error) {
	s.emitMetricStage(ctx, chathooks.MetricLlmEnd, ts, err)
}

func (s *ChatSession) TraceTtsStart(ctx context.Context, ts int64) {
	s.emitMetricStage(ctx, chathooks.MetricTtsStart, ts, nil)
}

func (s *ChatSession) TraceTtsFirstFrame(ctx context.Context, ts int64) {
	s.emitMetricStage(ctx, chathooks.MetricTtsFirstFrame, ts, nil)
}

func (s *ChatSession) TraceTtsStop(ctx context.Context, ts int64, err error) {
	s.emitMetricStage(ctx, chathooks.MetricTtsStop, ts, err)
}
