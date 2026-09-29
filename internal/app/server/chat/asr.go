package chat

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"
	. "xiaozhi-esp32-server-golang/internal/data/client"
	"xiaozhi-esp32-server-golang/internal/domain/asr"
	asr_types "xiaozhi-esp32-server-golang/internal/domain/asr/types"
	"xiaozhi-esp32-server-golang/internal/domain/audio"
	chathooks "xiaozhi-esp32-server-golang/internal/domain/chat/hooks"
	"xiaozhi-esp32-server-golang/internal/domain/speaker"
	"xiaozhi-esp32-server-golang/internal/domain/vad/inter"
	"xiaozhi-esp32-server-golang/internal/pool"
	log "xiaozhi-esp32-server-golang/logger"

	"github.com/cloudwego/eino/schema"
	"github.com/spf13/viper"
)

type ASRManagerOption func(*ASRManager)

const maxFirstSpeechPreAudioMs = 200

// AsrMessageSaveCallback message-save callback type
type AsrMessageSaveCallback func(userMsg *schema.Message, messageID string, audioData []float32)

type ASRManager struct {
	clientState     *ClientState
	serverTransport *ServerTransport
	session         *ChatSession // Used to access speakerManager

	// ASR resources managed as private fields
	asrResource *pool.ResourceWrapper[asr.AsrProvider]
	resourceMu  sync.RWMutex // Protects resource access
}

func NewASRManager(clientState *ClientState, serverTransport *ServerTransport, opts ...ASRManagerOption) *ASRManager {
	asr := &ASRManager{
		clientState:     clientState,
		serverTransport: serverTransport,
		session:         nil, // Set later via SetSession
	}
	for _, opt := range opts {
		opt(asr)
	}
	return asr
}

func (a *ASRManager) runAudioIdleTimeoutWatchdog(ctx context.Context) {
	state := a.clientState
	if state == nil {
		return
	}

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !state.UsesAudioIdleClock() || !state.AudioIdleStarted() || state.AudioIdlePaused() {
				continue
			}
			if !state.ShouldCountAudioIdleTimeout() || state.Asr.HasReceivedText() {
				continue
			}
			if state.GetClientVoiceStop() || state.AudioIdleTimeoutPending() {
				continue
			}

			elapsed := state.GetAudioIdleElapsed(time.Now())
			threshold := time.Duration(state.GetMaxIdleDuration()) * time.Millisecond
			if elapsed < threshold {
				continue
			}
			if !state.MarkAudioIdleTimeoutPending() {
				continue
			}

			if !state.Asr.HasOpenAudioInput() {
				log.Infof(
					"Audio idle timeout, no active ASR stream, close session directly: device=%s, mode=%s, elapsed=%dms, threshold=%dms",
					state.DeviceID,
					state.ListenMode,
					elapsed.Milliseconds(),
					state.GetMaxIdleDuration(),
				)
				if a.session != nil {
					a.session.CloseWithReason(chatSessionCloseReasonAudioIdleTimeout)
				} else {
					state.ClearAudioIdleTimeoutPending()
				}
				continue
			}

			log.Infof(
				"Audio idle timeout, trigger ASR close: device=%s, mode=%s, elapsed=%dms, threshold=%dms",
				state.DeviceID,
				state.ListenMode,
				elapsed.Milliseconds(),
				state.GetMaxIdleDuration(),
			)
			state.OnVoiceSilence()
		}
	}
}

// ProcessVadAudio starts VAD audio processing
func (a *ASRManager) ProcessVadAudio(ctx context.Context) {
	state := a.clientState
	go func() {
		hasTriggeredCancel := true // Flag: whether cancel was already triggered (when voiceDuration > 120)
		hasLoggedFirstTextExtendedWait := false
		speakerInterruptTriggered := atomic.Bool{}
		speakerPeekInFlight := atomic.Bool{}
		lastSpeakerPeekDoneAt := atomic.Int64{}
		var speakerPeekAudioMs int64
		var speakerPeekRequestSeq uint64
		const speakerPeekInterval = 200 * time.Millisecond
		const firstSpeakerPeekAudioThresholdMs int64 = 400
		audioFormat := state.InputAudioFormat
		// Large enough decode buffer (assume max frame 120ms)
		maxFrameSize := audioFormat.SampleRate * audioFormat.Channels * 120 / 1000
		audioProcesser, err := audio.GetAudioProcesser(audioFormat.SampleRate, audioFormat.Channels, 20) // Default value to create the decoder
		if err != nil {
			log.Errorf("Failed to get decoder: %v", err)
			return
		}

		// Get frame size and duration from the first real frame
		var frameSize int
		var frameDurationMs int
		var vadNeedGetCount int // Frames needed for VAD; computed after the first frame

		// Lazy-load VAD and release when idle to avoid holding pool instances long-term.
		var vadWrapper *pool.ResourceWrapper[inter.VAD]
		var vadProvider inter.VAD
		var vadLastUseAt time.Time
		const vadIdleReleaseTimeout = 2 * time.Second
		vadIdleTicker := time.NewTicker(time.Second)
		defer vadIdleTicker.Stop()
		needVad := !(state.Asr.AutoEnd || state.ListenMode == "manual")
		vadProviderName := state.DeviceConfig.Vad.Provider
		vadProviderConfig := state.DeviceConfig.Vad.Config
		effectiveVadProviderName := vadProviderName
		if configProvider, ok := vadProviderConfig["provider"].(string); ok && configProvider != "" {
			effectiveVadProviderName = configProvider
		}
		isSileroVAD := effectiveVadProviderName == "silero_vad"
		releaseVad := func(reason string) {
			if vadWrapper == nil {
				return
			}
			pool.Release(vadWrapper)
			vadWrapper = nil
			vadProvider = nil
			vadLastUseAt = time.Time{}
			log.Debugf("Release VAD resource: device=%s, reason=%s", state.DeviceID, reason)
		}
		defer releaseVad("process_exit")
		ensureVad := func() bool {
			if !needVad {
				return false
			}
			if vadProvider != nil {
				return true
			}

			// Warn if provider is nil
			if vadProviderName == "" {
				log.Warnf("VAD provider empty, trying to get from config")
			} else {
				log.Debugf("Get VAD resource: provider=%s", vadProviderName)
			}

			wrapper, err := pool.Acquire[inter.VAD](
				"vad",
				vadProviderName,
				vadProviderConfig,
			)
			if err != nil {
				log.Errorf("Failed to get VAD resource: provider=%s, config=%+v, error=%v", vadProviderName, vadProviderConfig, err)
				return false
			}
			vadWrapper = wrapper
			vadProvider = wrapper.GetProvider()
			vadLastUseAt = time.Now()
			return true
		}
		for {
			// Buffer sized to max frame; actual size known after decode
			pcmFrame := make([]float32, maxFrameSize)

			select {
			case <-vadIdleTicker.C:
				if vadWrapper != nil && !vadLastUseAt.IsZero() && time.Since(vadLastUseAt) >= vadIdleReleaseTimeout {
					releaseVad("idle_timeout")
				}
				continue
			case opusFrame, ok := <-state.OpusAudioBuffer:
				//log.Debugf("processAsrAudio received audio data, len: %d", len(opusFrame))
				if !ok {
					log.Debugf("processAsrAudio audio channel closed")
					return
				}

				var skipVad bool
				var haveVoice bool
				clientHaveVoice := state.GetClientHaveVoice()
				if state.ListenMode == "manual" {
					skipVad = true         // Skip VAD
					clientHaveVoice = true // Had voice before
					haveVoice = true       // Has voice this time
				} else if state.Asr.AutoEnd {
					skipVad = true   // Still provider-controlled stop, without changing idle semantics
					haveVoice = true // This audio goes straight to ASR
				}

				if state.GetClientVoiceStop() { // Already stopped speaking; do not accept audio
					//log.Infof("Client stopped speaking, skip audio data")
					continue
				}

				//log.Debugf("clientVoiceStop: %+v, asrDataSize: %d, listenMode: %s, isSkipVad: %v\n", state.GetClientVoiceStop(), state.AsrAudioBuffer.GetAsrDataSize(), state.ListenMode, skipVad)

				n, err := audioProcesser.DecoderFloat32(opusFrame, pcmFrame)
				if err != nil {
					log.Errorf("Decode failed: %v", err)
					continue
				}

				// Dynamically compute frame size/duration from decoded data
				if frameSize == 0 {
					// First frame: compute frame info from decoded data
					frameSize = n
					samplesPerChannel := n / audioFormat.Channels
					frameDurationMs = samplesPerChannel * 1000 / audioFormat.SampleRate
					audioFormat.FrameDuration = frameDurationMs

					// Compute frames needed for VAD
					vadNeedGetCount = 1
					log.Debugf("Compute frame info from actual audio: frameSize=%d, frameDurationMs=%d, vadNeedGetCount=%d", frameSize, frameDurationMs, vadNeedGetCount)
				}

				var vadPcmData []float32
				pcmData := pcmFrame[:n]
				speakerPcmData := pcmFrame[:n]

				// Check frame size consistency (usually matches; use actual if not)
				if n != frameSize {
					log.Debugf("Frame size mismatch: expected=%d, actual=%d, using actual", frameSize, n)
					// Recompute this frame's duration
					samplesPerChannel := n / audioFormat.Channels
					currentFrameDurationMs := samplesPerChannel * 1000 / audioFormat.SampleRate
					frameSize = n
					frameDurationMs = currentFrameDurationMs
					audioFormat.FrameDuration = frameDurationMs
				}

				if !skipVad && needVad {
					if !ensureVad() {
						continue
					}
					//decode opus to pcm
					state.AsrAudioBuffer.AddAsrAudioData(pcmData)

					// Minimum data amount VAD needs
					vadNeedMinSize := frameSize

					if state.AsrAudioBuffer.GetAsrDataSize() >= vadNeedMinSize {
						if isSileroVAD {
							vadPcmData = pcmData
						} else {
							vadPcmData = state.AsrAudioBuffer.GetAsrData(vadNeedGetCount, frameSize)
						}

						// If speech already detected, skip VAD and pass pcmData to ASR
						// Run VAD with the resource acquired outside the loop
						if !isSileroVAD {
							vadLastUseAt = time.Now()
							if err := vadProvider.Reset(); err != nil {
								log.Errorf("Failed to reset VAD: %v", err)
								continue
							}
						}

						// Run VAD detection
						vadLastUseAt = time.Now()
						haveVoice, err = vadProvider.IsVADExt(vadPcmData, audioFormat.SampleRate, frameSize)
						if err != nil {
							log.Errorf("processAsrAudio VAD detect failed: %v", err)
							continue
						}

						// On first speech hit, assign vadPcmData to pcmData for continuity; later audio all goes to ASR
						if haveVoice && !clientHaveVoice {
							// On first speech, keep at most 200ms of leading silence
							currentFrameSamples := len(pcmData)
							allData := state.AsrAudioBuffer.GetAndClearAllData()
							pcmData = trimFirstSpeechAudio(allData, currentFrameSamples, audioFormat.SampleRate, audioFormat.Channels)
						}
					}
					//log.Debugf("isVad, pcmData len: %d, vadPcmData len: %d, haveVoice: %v", len(pcmData), len(vadPcmData), haveVoice)
				}

				if haveVoice {
					hasLoggedFirstTextExtendedWait = false
					//log.Infof("Speech detected, len: %d", len(pcmData))
					state.SetClientHaveVoice(true)
					state.SetClientHaveVoiceLastTime(time.Now().UnixMilli())
					state.Vad.ResetIdleDuration()
					// Accumulate detected voice duration (also update mid-process duration)
					state.Vad.AddVoiceDuration(int64(frameDurationMs))

					continuousVoiceDuration := state.Vad.GetVoiceContinuousDuration()
					if state.IsRealTime() && viper.GetInt("chat.realtime_mode") == 1 && continuousVoiceDuration > 360 {
						// Run only if not yet triggered; ensure once only
						if !hasTriggeredCancel {
							if a.session != nil && a.session.isRealtimeMcpAudioGateActive() {
								log.Debugf("Device %s realtime media playback gate active, skip VAD interrupt", state.DeviceID)
								hasTriggeredCancel = true
							} else {
								// In realtime mode, cancel in-flight LLM and TTS
								log.Debugf("Realtime VAD barge-in and voice duration > %d ms: cancel in-progress LLM and TTS if any", continuousVoiceDuration)
								if a.session != nil {
									a.session.StopAssistantOutputAfterAsrWithReason(true, "ASRManager.ProcessVadAudio realtime_mode=1 VAD interrupt")
								} else {
									state.AfterAsrSessionCtx.CancelWithReason("ASRManager.ProcessVadAudio: realtime_mode=1 VAD interrupt")
								}
								hasTriggeredCancel = true // Mark as triggered
							}
						}
					}
				} else {
					state.Vad.AddIdleDuration(int64(frameDurationMs))
					state.Vad.ResetVoiceContinuousDuration()

					// No voice now and none before: reset accumulated voice duration
					// Had voice before but not now: keep duration for later reset logic
					if !clientHaveVoice {
						speakerInterruptTriggered.Store(false)
						lastSpeakerPeekDoneAt.Store(0)
						speakerPeekAudioMs = 0
						// Keep ~10 recent frames
						/*
							if state.AsrAudioBuffer.GetFrameCount(frameSize) > vadNeedGetCount*3 {
								state.AsrAudioBuffer.RemoveAsrAudioData(1, frameSize)
							}*/
						continue
					}
				}

				if clientHaveVoice || haveVoice {
					// On first speech hit, forward current cached frames immediately so very short speech is not dropped from ASR.

					// VAD succeeded; send data to ASR audio channel
					//log.Infof("VAD detected speech, send data to ASR audio channel, len: %d", len(pcmData))
					state.Asr.AddAudioData(pcmData)

					// Speaker ID only gets voiced frames; avoid leading/trailing silence in the stream.
					if haveVoice &&
						state.IsSpeakerEnabled() && state.HasSpeakerGroups() &&
						a.session != nil && a.session.speakerManager != nil {
						// On first speech, start streaming recognition
						if !a.session.speakerManager.IsActive() {
							sampleRate := audioFormat.SampleRate
							agentId := a.session.clientState.AgentID
							if err := a.session.speakerManager.StartStreaming(ctx, sampleRate, agentId); err != nil {
								log.Warnf("Failed to start speaker recognition stream: %v", err)
							} else {
								speakerInterruptTriggered.Store(false)
								lastSpeakerPeekDoneAt.Store(0)
								speakerPeekAudioMs = 0
							}
						}

						// Send audio chunk
						if err := a.session.speakerManager.SendAudioChunk(ctx, speakerPcmData); err != nil {
							log.Warnf("Failed to send audio chunk to speaker recognition service: %v", err)
						} else if a.session.speakerManager.IsActive() {
							if audioFormat.Channels > 0 && audioFormat.SampleRate > 0 {
								speakerPeekAudioMs += int64(len(speakerPcmData)/audioFormat.Channels) * 1000 / int64(audioFormat.SampleRate)
							}

							if state.IsRealTime() &&
								viper.GetInt("chat.realtime_mode") == 3 &&
								!speakerInterruptTriggered.Load() &&
								speakerPeekAudioMs >= firstSpeakerPeekAudioThresholdMs {
								now := time.Now()
								lastDoneAt := lastSpeakerPeekDoneAt.Load()
								if (lastDoneAt <= 0 || now.Sub(time.Unix(0, lastDoneAt)) >= speakerPeekInterval) &&
									speakerPeekInFlight.CompareAndSwap(false, true) {
									reqSeq := atomic.AddUint64(&speakerPeekRequestSeq, 1)
									requestID := fmt.Sprintf("peek_%d_%d", now.UnixMilli(), reqSeq)

									go func(reqID string) {
										defer func() {
											lastSpeakerPeekDoneAt.Store(time.Now().UnixNano())
											speakerPeekInFlight.Store(false)
										}()

										if a.session == nil || a.session.speakerManager == nil || !a.session.speakerManager.IsActive() {
											return
										}

										peekCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
										defer cancel()

										peekResult, throttled, err := a.session.speakerManager.PeekAndIdentify(peekCtx, reqID)
										if err != nil {
											if ctx.Err() == nil {
												log.Debugf("Speaker peek failed: device=%s, request_id=%s, err=%v", state.DeviceID, reqID, err)
											}
											return
										}
										if throttled {
											return
										}
										if peekResult == nil || !peekResult.Identified {
											return
										}
										if !speakerInterruptTriggered.CompareAndSwap(false, true) {
											return
										}

										log.Infof(
											"Realtime speaker peek hit, interrupt immediately: device=%s, speaker=%s, confidence=%.4f, threshold=%.4f",
											state.DeviceID,
											peekResult.SpeakerName,
											peekResult.Confidence,
											peekResult.Threshold,
										)
										if a.session != nil && a.session.isRealtimeMcpAudioGateActive() {
											log.Debugf("Device %s realtime media playback gate active, skip speaker peek interrupt", state.DeviceID)
											return
										}
										a.session.MarkTurnSpeakerInterrupted()
										if a.session != nil {
											a.session.StopAssistantOutputAfterAsrWithReason(true, "ASRManager.ProcessVadAudio realtime_mode=3 speaker peek interrupt")
										} else {
											state.AfterAsrSessionCtx.CancelWithReason("ASRManager.ProcessVadAudio: realtime_mode=3 speaker peek interrupt")
										}
									}(requestID)
								}
							}
						}
					}
				}

				// Had speech before but none this time; decide if speaking stopped
				lastHaveVoiceTime := state.GetClientHaveVoiceLastTime()

				if clientHaveVoice && lastHaveVoiceTime > 0 && !haveVoice {
					// If voiced duration < 300ms, reset clientHaveVoice to avoid short-noise false positives
					voiceDurationInSession := state.Vad.GetVoiceDurationInSession()
					if voiceDurationInSession < 100 {
						log.Debugf("Voice duration too short (%dms < 300ms), reset clientHaveVoice", voiceDurationInSession)
						state.SetClientHaveVoice(false)
						state.Vad.ResetVoiceDuration()
						speakerInterruptTriggered.Store(false)
						lastSpeakerPeekDoneAt.Store(0)
						speakerPeekAudioMs = 0
						continue
					}

					idleDuration := state.Vad.GetIdleDuration()
					if state.IsRealTime() && !state.Asr.HasReceivedText() {
						preTextSilenceDuration := state.GetPreAsrTextSilenceDuration()
						if idleDuration <= preTextSilenceDuration {
							log.Debugf(
								"Realtime mode waiting for first ASR text, delay silence-threshold close: status=%s, idle=%dms, pre_text_timeout=%dms, voice_duration=%dms, voice_duration_in_session=%dms, history_audio_samples=%d",
								state.Status,
								idleDuration,
								preTextSilenceDuration,
								state.Vad.GetVoiceDuration(),
								voiceDurationInSession,
								state.Asr.GetHistoryAudioLen(),
							)
							continue
						}

						if !hasLoggedFirstTextExtendedWait {
							log.Debugf(
								"Realtime silence timeout without ASR text yet, keep current ASR stream and forward audio: status=%s, idle=%dms, pre_text_timeout=%dms, voice_duration=%dms, voice_duration_in_session=%dms, history_audio_samples=%d",
								state.Status,
								idleDuration,
								preTextSilenceDuration,
								state.Vad.GetVoiceDuration(),
								voiceDurationInSession,
								state.Asr.GetHistoryAudioLen(),
							)
							hasLoggedFirstTextExtendedWait = true
						}
						continue
					}

					if state.IsSilence(idleDuration) { // Transition from voice to silence
						log.Debugf(
							"Voice end detected, preparing to stop ASR: status=%s, idle=%dms, voice_duration=%dms, voice_duration_in_session=%dms, history_audio_samples=%d, pending_restart=%v",
							state.Status,
							idleDuration,
							state.Vad.GetVoiceDuration(),
							state.Vad.GetVoiceDurationInSession(),
							state.Asr.GetHistoryAudioLen(),
							state.AudioIdleTimeoutPending(),
						)
						// Reset flag before OnVoiceSilence so it can trigger again next time
						hasTriggeredCancel = false
						speakerInterruptTriggered.Store(false)
						lastSpeakerPeekDoneAt.Store(0)
						speakerPeekAudioMs = 0
						state.OnVoiceSilence()
						//state.VoiceStatus.Reset()
						continue
					}
				}

			case <-ctx.Done():
				return
			}
		}
	}()
}

// releaseResource frees ASR resources (internal)
func (a *ASRManager) releaseResource() {
	a.resourceMu.Lock()
	defer a.resourceMu.Unlock()
	if a.asrResource != nil {
		pool.Release(a.asrResource)
		a.asrResource = nil
		log.Debugf("ASR resource returned")
	}
}

// Cleanup frees ASR resources (for external callers)
func (a *ASRManager) Cleanup() {
	a.releaseResource()
}

// restartAsrRecognition restarts ASR recognition
func (a *ASRManager) RestartAsrRecognition(ctx context.Context) error {
	state := a.clientState
	log.Debugf("Restart ASR recognition start")
	if a.session != nil {
		a.session.ResetTurnSpeakerInterrupted()
	}

	// Cancel current ASR context
	state.Asr.CancelWithReason("ASRManager.RestartAsrRecognition: cancel previous ASR context before restart")

	state.Asr.ResetReceivedText()
	state.VoiceStatus.Reset()
	state.AsrAudioBuffer.ClearAsrAudioData()
	state.Asr.ClearHistoryAudio() // Clear historical audio cache

	// If no resource yet, acquire one
	a.resourceMu.Lock()
	var asrProvider asr.AsrProvider
	if a.asrResource == nil {
		// Need a new resource
		a.resourceMu.Unlock()

		asrWrapper, err := pool.Acquire[asr.AsrProvider](
			"asr",
			state.DeviceConfig.Asr.Provider,
			state.DeviceConfig.Asr.Config,
		)
		if err != nil {
			log.Errorf("Failed to get ASR resource: %v", err)
			return fmt.Errorf("获取ASR资源失败: %w", err)
		}

		// Store resource ref in private fields
		a.resourceMu.Lock()
		a.asrResource = asrWrapper
		asrProvider = asrWrapper.GetProvider()
		a.resourceMu.Unlock()
		log.Debugf("Getting new ASR resource")
	} else {
		// Reuse existing resource
		asrProvider = a.asrResource.GetProvider()
		a.resourceMu.Unlock()
		log.Debugf("Reusing existing ASR resource")
	}

	// Recreate ASR context and channels
	state.Asr.Ctx, state.Asr.Cancel = context.WithCancel(ctx)
	state.Asr.AsrAudioChannel = make(chan []float32, 100)

	// Restart streaming recognition
	asrResultChannel, err := asrProvider.StreamingRecognize(state.Asr.Ctx, state.Asr.AsrAudioChannel)
	if err != nil {
		// Recognition failed; return resource (may be broken)
		a.releaseResource()
		log.Errorf("Failed to restart ASR streaming recognition: %v", err)
		return fmt.Errorf("重启ASR流式识别失败: %w", err)
	}

	state.AsrResultChannel = asrResultChannel
	// Reset stats time for this conversation round's total latency
	state.MarkTurnStart()
	if a.session != nil {
		a.session.TraceTurnStart(state.Asr.Ctx, state.Statistic.TurnStartTs)
	}
	log.Debugf("Restart ASR recognition succeeded")
	return nil
}

// StartAsrRecognitionLoop starts the ASR result processing loop
// onMessageSave: message-save callback
// onError: error callback (e.g. close session)
func (a *ASRManager) StartAsrRecognitionLoop(
	ctx context.Context,
	onMessageSave AsrMessageSaveCallback,
	onError func(error),
) {
	state := a.clientState

	// Goroutine to process ASR results
	go func() {
		// defer ensures ASR resources are released when the goroutine exits
		defer func() {
			if r := recover(); r != nil {
				log.Errorf("ASR result handler goroutine panic: %v, stack: %s", r, string(debug.Stack()))
			}
			// Release resources on normal exit or panic
			a.releaseResource()
		}()

		// Max idle 60s
		var startIdleTime, maxIdleTime int64
		startIdleTime = time.Now().Unix()
		maxIdleTime = 60

		// Wait count when status disallows restart (avoid infinite loop)
		var invalidStatusWaitCount int64
		maxInvalidStatusWaitCount := int64(10) // Wait at most 10 times (~1 second)

		// Empty-result short-window guard: avoid main-loop spin if ASR keeps returning empty
		const emptyResultProtectWindow = 3 * time.Second
		const maxEmptyResultInWindow = 3
		emptyResultWindowStart := time.Now()
		emptyResultCount := 0

		// Recoverable-error short-window guard: avoid infinite reconnect when upstream keeps failing
		const recoverableErrorProtectWindow = 10 * time.Second
		const maxRecoverableErrorInWindow = 3
		recoverableErrorWindowStart := time.Now()
		recoverableErrorCount := 0

		isAllowedToRestart := func() bool {
			allowed := state.Status == ClientStatusListening || state.Status == ClientStatusListenStop
			if state.IsRealTime() {
				allowed = state.Status != ClientStatusInit
			}
			return allowed
		}
		resumeAudioIdle := func() {
			state.ResumeAudioIdleWindow(time.Now())
		}
		startAudioIdle := func() {
			state.StartAudioIdleWindow(time.Now())
		}
		closeAudioIdleTimeout := func(reason string) {
			if !state.AudioIdleTimeoutPending() {
				return
			}

			state.ClearAudioIdleTimeoutPending()
			log.Infof("Audio idle timeout close completed: device=%s, reason=%s", state.DeviceID, reason)
			if a.session != nil {
				a.session.CloseWithReason(chatSessionCloseReasonAudioIdleTimeout)
				return
			}
			if onError != nil {
				onError(fmt.Errorf("audio idle timeout: %s", reason))
			}
		}

		for {
			select {
			case <-ctx.Done():
				log.Debugf("asr ctx done")
				return
			default:
			}

			result, isRetry, err := state.RetireAsrResult(ctx)
			if err != nil {
				if ctx.Err() != nil || errors.Is(err, context.Canceled) {
					log.Debugf("Failed to handle ASR result, ASR canceled: %v", err)
				} else {
					log.Errorf("Failed to handle ASR result: %v", err)
				}
				if onError != nil {
					onError(err)
				}
				return
			}
			if !isRetry {
				log.Debugf("asrResult is not retry, return")
				return
			}
			text := result.Text

			if result.RetryReason != "" {
				if state.AudioIdleTimeoutPending() {
					closeAudioIdleTimeout(result.RetryReason)
					return
				}

				now := time.Now()
				if now.Sub(recoverableErrorWindowStart) > recoverableErrorProtectWindow {
					recoverableErrorWindowStart = now
					recoverableErrorCount = 0
				}
				recoverableErrorCount++
				log.Warnf(
					"ASR recoverable error: reason=%s, count=%d/%d, status=%s",
					result.RetryReason,
					recoverableErrorCount,
					maxRecoverableErrorInWindow,
					state.Status,
				)

				if recoverableErrorCount >= maxRecoverableErrorInWindow {
					err := fmt.Errorf("ASR短时间内连续触发可恢复错误(%d次/%s)，停止重试并断开连接", recoverableErrorCount, recoverableErrorProtectWindow)
					log.Errorf("%v", err)
					if onError != nil {
						onError(err)
					}
					return
				}

				switch result.RetryReason {
				case asr_types.RetryReasonDoubaoResponseCode45000081, asr_types.RetryReasonXunfeiServiceInstanceInvalid, asr_types.RetryReasonAliyunQwen3ConnectionClosed:
					a.releaseResource()
					if isAllowedToRestart() {
						invalidStatusWaitCount = 0
						if restartErr := a.RestartAsrRecognition(ctx); restartErr != nil {
							log.Errorf("Failed to restart recognition after ASR recoverable error: reason=%s, err=%v", result.RetryReason, restartErr)
							if onError != nil {
								onError(restartErr)
							}
							return
						}
						resumeAudioIdle()
						continue
					}

					log.Warnf("ASR recoverable error while status disallows immediate restart: reason=%s, status=%s, realtime=%v", result.RetryReason, state.Status, state.IsRealTime())
					state.Asr.CancelWithReason("ASRManager.StartAsrRecognitionLoop: recoverable error but restart not allowed yet")
					resumeAudioIdle()
					continue
				case asr_types.RetryReasonDoubaoWaitingNextPacketTimeout:
					log.Warnf("Doubao ASR session idle timeout, suspend current stream and rebuild on next speech")
					state.Asr.CancelWithReason("ASRManager.StartAsrRecognitionLoop: doubao waiting next packet timeout")
					resumeAudioIdle()
					continue
				}
			}

			if text != "" {
				asrFinalTs := time.Now().UnixMilli()
				state.MarkAsrFinalTextAt(asrFinalTs)
				if a.session != nil {
					a.session.TraceAsrFinalText(ctx, asrFinalTs)
				}
				log.Debugf("Handle ASR result: %s, duration: %d ms", text, state.GetAsrDuration())

				state.ClearAudioIdleTimeoutPending()
				// Reset empty-result count after successful recognition
				emptyResultWindowStart = time.Now()
				emptyResultCount = 0
				recoverableErrorWindowStart = time.Now()
				recoverableErrorCount = 0

				// In realtime mode, stop current LLM and TTS
				if state.IsRealTime() && viper.GetInt("chat.realtime_mode") == 2 {
					shouldInterrupt := true
					if a.session != nil && a.session.isRealtimeMcpAudioGateActive() {
						shouldInterrupt = false
						log.Debugf("Device %s realtime media playback gate active, defer to ASR final gate, skip ASR-result interrupt", state.DeviceID)
					}
					if shouldInterrupt {
						log.Debugf("OnListenStart in realtime mode, stop current LLM and TTS")
						if a.session != nil {
							a.session.StopAssistantOutputAfterAsrWithReason(true, "ASRManager.StartAsrRecognitionLoop realtime_mode=2 ASR result interrupt")
						} else {
							state.AfterAsrSessionCtx.CancelWithReason("ASRManager.StartAsrRecognitionLoop: realtime_mode=2 ASR result interrupt")
						}
					}
				}

				// Reset retry counter
				startIdleTime = time.Now().Unix()

				// On ASR result, end voice input (OnVoiceSilence fetches speaker result async)
				state.OnVoiceSilence()

				// Get cached speaker result (with timeout)
				speakerResult := a.getSpeakerResult()
				speakerInterrupted := false
				if a.session != nil {
					speakerInterrupted = a.session.ConsumeTurnSpeakerInterrupted()
				}

				if a.session != nil {
					payload, stop, hookErr := a.session.hookHub.EmitASROutput(a.session.hookContext(ctx), chathooks.ASROutputData{Text: text, SpeakerResult: speakerResult})
					if hookErr != nil {
						log.Warnf("ASR_OUTPUT hook failed: %v", hookErr)
					}
					text = payload.Text
					speakerResult = payload.SpeakerResult
					if stop {
						log.Infof("ASR_OUTPUT hook requested stopping current flow")
						state.Asr.ClearHistoryAudio()
						if state.UsesAudioIdleClock() {
							startAudioIdle()
						} else {
							state.ResetAudioIdleWindow()
						}
						continue
					}
				}

				if a.session != nil {
					allowChat, denyReason := a.session.ShouldAllowSpeakerChat(speakerResult, speakerInterrupted)
					if !allowChat {
						log.Infof(
							"Discard ASR result and skip STT/LLM: device=%s, reason=%s, speaker_interrupted=%v, speaker_result=%+v, text=%q",
							state.DeviceID,
							denyReason,
							speakerInterrupted,
							speakerResult,
							text,
						)
						state.Asr.ClearHistoryAudio()

						if !state.IsRealTime() {
							startAudioIdle()
							return
						}
						if restartErr := a.RestartAsrRecognition(ctx); restartErr != nil {
							log.Errorf("Failed to restart recognition after discarding ASR result: %v", restartErr)
							if onError != nil {
								onError(restartErr)
							}
							return
						}
						startAudioIdle()
						continue
					}
				}

				// Build user message; use hook-rewritten text for the side-effect chain
				userMsg := &schema.Message{
					Role:    schema.User,
					Content: text,
				}

				// Build MessageID (MD5 shortens to fit DB varchar(64))
				// Original format: {SessionID}-{Role}-{Timestamp}
				rawMessageID := fmt.Sprintf("%s-%s-%d",
					state.SessionID,
					userMsg.Role,
					time.Now().UnixMilli())
				// MD5 hex string fixed at 32 chars
				hash := md5.Sum([]byte(rawMessageID))
				messageID := hex.EncodeToString(hash[:])

				// Sync into memory (for LLM context)
				state.AddMessage(userMsg)

				// Get audio data (ASR history audio)
				audioData := state.Asr.GetHistoryAudio()
				state.Asr.ClearHistoryAudio()

				// Save message via callback
				if onMessageSave != nil {
					onMessageSave(userMsg, messageID, audioData)
				}

				// ASR result sent to client also uses hook-rewritten text
				err = a.serverTransport.SendAsrResult(text)
				if err != nil {
					log.Errorf("Failed to send ASR message: %v", err)
					if onError != nil {
						onError(err)
					}
					return
				}

				if a.session != nil {
					handledByRealtimeGate, gateErr := a.session.tryHandleRealtimeMcpAudioASR(ctx, text)
					if gateErr != nil {
						log.Warnf("Realtime media playback quick control failed: device=%s text=%q err=%v", state.DeviceID, text, gateErr)
					}
					if handledByRealtimeGate {
						if !state.IsRealTime() {
							return
						}
						if restartErr := a.RestartAsrRecognition(ctx); restartErr != nil {
							log.Errorf("Failed to restart ASR after realtime media control: %v", restartErr)
							if onError != nil {
								onError(restartErr)
							}
							return
						}
						startAudioIdle()
						continue
					}
				}

				// Add to queue (moved into ASRManager)
				if err := a.addAsrResultToQueue(text, speakerResult); err != nil {
					log.Errorf("Failed to start dialog: %v", err)
					if onError != nil {
						onError(err)
					}
					return
				}

				// Non-realtime: return resource after ASR completes
				// Realtime: RestartAsrRecognition manages resources (return old then get new)
				if !state.IsRealTime() {
					return
				}

				// Realtime: restart ASR (RestartAsrRecognition returns old then gets new)
				if restartErr := a.RestartAsrRecognition(ctx); restartErr != nil {
					log.Errorf("Failed to restart ASR recognition: %v", restartErr)
					if onError != nil {
						onError(restartErr)
					}
					return
				}
				// Realtime: continue loop for the next ASR result
				continue
			} else {
				log.Debugf(
					"ASR empty result details: status=%s, emptyReason=%s, client_voice_stop=%v, history_audio_samples=%d, voice_duration=%dms, voice_duration_in_session=%dms, idle_duration=%dms, realtime=%v",
					state.Status,
					result.EmptyReason,
					state.GetClientVoiceStop(),
					state.Asr.GetHistoryAudioLen(),
					state.Vad.GetVoiceDuration(),
					state.Vad.GetVoiceDurationInSession(),
					state.Vad.GetIdleDuration(),
					state.IsRealTime(),
				)
				if state.AudioIdleTimeoutPending() {
					closeAudioIdleTimeout(result.EmptyReason)
					return
				}
				if result.EmptyReason != "" {
					log.Debugf("ASR empty result classified: reason=%s, status=%s", result.EmptyReason, state.Status)
					emptyResultWindowStart = time.Now()
					emptyResultCount = 0

					if result.EmptyReason == asr_types.EmptyReasonNoServerResponse ||
						result.EmptyReason == asr_types.EmptyReasonProviderEmptyFinal {
						state.Asr.CancelWithReason("ASRManager.StartAsrRecognitionLoop: empty final result from provider")
						resumeAudioIdle()
						continue
					}
				}

				now := time.Now()
				if now.Sub(emptyResultWindowStart) > emptyResultProtectWindow {
					emptyResultWindowStart = now
					emptyResultCount = 0
				}

				// Count empty results only when a single ASR round is very short (e.g. <500ms) to avoid CPU busy-loop on ASR faults;
				// Normal VAD + silence flow usually takes >500ms and should not trip disconnect protection.
				turnDuration := now.UnixMilli() - state.Statistic.TurnStartTs
				if turnDuration < 500 {
					emptyResultCount++
					if emptyResultCount >= maxEmptyResultInWindow {
						err := fmt.Errorf("ASR短时间内连续返回空结果(%d次/%s)，触发保护并断开连接", emptyResultCount, emptyResultProtectWindow)
						log.Errorf("%v", err)
						if onError != nil {
							onError(err)
						}
						return
					}
				} else {
					// Duration normal: silent recognition OK; reset protection counters
					emptyResultCount = 0
					emptyResultWindowStart = now
				}

				// Empty text case
				select {
				case <-ctx.Done():
					log.Debugf("asr ctx done")
					return
				default:
				}

				log.Debugf("ready Restart Asr, state.Status: %s", state.Status)
				// Realtime: keep listening even in LLMStart/TTSStart (allow ASR restart)
				// Non-realtime: only Listening or ListenStop may restart ASR
				if isAllowedToRestart() {
					// Status allows restart; reset wait count
					invalidStatusWaitCount = 0
					// Empty text; check whether to restart ASR
					diffTs := time.Now().Unix() - startIdleTime
					if startIdleTime > 0 && diffTs <= maxIdleTime {
						log.Warnf("ASR result empty, trying to restart ASR, diff ts: %d", diffTs)
						if restartErr := a.RestartAsrRecognition(ctx); restartErr != nil {
							log.Errorf("Failed to restart ASR recognition: %v", restartErr)
							if onError != nil {
								onError(restartErr)
							}
							return
						}
						resumeAudioIdle()
						continue
					} else {
						log.Warnf("ASR result empty, max idle time reached: %d", maxIdleTime)
						if onError != nil {
							onError(fmt.Errorf("ASR识别结果为空，已达到最大空闲时间: %d", maxIdleTime))
						}
						return
					}
				} else {
					// Status disallows restart; brief wait then loop so status can recover
					invalidStatusWaitCount++
					if invalidStatusWaitCount >= maxInvalidStatusWaitCount {
						// Wait timed out; exit loop
						log.Debugf("Status is %s, realtime: %v, no change after %d waits, exit ASR recognition loop", state.Status, state.IsRealTime(), maxInvalidStatusWaitCount)
						return
					}
					// Brief wait then continue; wait for status recovery
					log.Debugf("Status is %s, realtime: %v, restart not allowed, waiting for status recovery (wait count: %d/%d)", state.Status, state.IsRealTime(), invalidStatusWaitCount, maxInvalidStatusWaitCount)
					time.Sleep(200 * time.Millisecond) // Wait 100ms
					continue
				}
			}
		}
	}()
}

func trimFirstSpeechAudio(allData []float32, currentFrameSamples, sampleRate, channels int) []float32 {
	if len(allData) == 0 {
		return nil
	}
	if currentFrameSamples <= 0 || currentFrameSamples > len(allData) || sampleRate <= 0 || channels <= 0 {
		return allData
	}

	maxPreSpeechSamples := sampleRate * channels * maxFirstSpeechPreAudioMs / 1000
	keepSamples := currentFrameSamples + maxPreSpeechSamples
	if keepSamples >= len(allData) {
		return allData
	}

	audio := make([]float32, keepSamples)
	copy(audio, allData[len(allData)-keepSamples:])
	return audio
}

// getSpeakerResult gets cached speaker result (with timeout)
func (a *ASRManager) getSpeakerResult() *speaker.IdentifyResult {
	if a.session == nil || a.session.speakerManager == nil {
		return nil
	}

	// Speaker streaming not active this round (no speech / start failed / previous round finished),
	// Async callback will not signal ready; return immediately to avoid a useless 200ms wait.
	if !a.session.speakerManager.IsActive() {
		return nil
	}

	log.Debugf("speakerManager: %+v, IsActive: %+v", a.session.speakerManager, a.session.speakerManager.IsActive())

	timeout := time.NewTimer(200 * time.Millisecond)
	defer timeout.Stop()

	var speakerResult *speaker.IdentifyResult
	select {
	case <-a.session.speakerResultReady:
		a.session.speakerResultMu.RLock()
		speakerResult = a.session.pendingSpeakerResult
		a.session.speakerResultMu.RUnlock()
	case <-timeout.C:
		// After timeout, read current result (may be nil)
		a.session.speakerResultMu.RLock()
		speakerResult = a.session.pendingSpeakerResult
		a.session.speakerResultMu.RUnlock()
		log.Debugf("Speaker recognition result timed out, using current result")
	}
	log.Debugf("Got speaker recognition result: %+v", speakerResult)
	return speakerResult
}

// addAsrResultToQueue adds ASR result to queue (moved into ASRManager)
func (a *ASRManager) addAsrResultToQueue(text string, speakerResult *speaker.IdentifyResult) error {
	if a.session == nil {
		return fmt.Errorf("session is nil")
	}
	return a.session.AddAsrResultToQueue(text, speakerResult)
}
