package client

import (
	"bytes"
	"context"
	"strings"
	"sync"
	asr_types "xiaozhi-esp32-server-golang/internal/domain/asr/types"
	log "xiaozhi-esp32-server-golang/logger"
)

type Asr struct {
	lock sync.RWMutex
	// ASR context and channels
	Ctx              context.Context
	Cancel           context.CancelFunc
	AsrEnd           chan bool
	AsrAudioChannel  chan []float32                 //streaming audio input channel
	AsrResultChannel chan asr_types.StreamingResult //streaming channel for ASR result fragments
	AsrResult        bytes.Buffer                   //stores the final recognized text for this turn
	Statue           int                            //0: init 1: recognizing 2: finished
	AutoEnd          bool                           //auto_end means ASR decides end-of-speech; VAD is not used

	// ASR type and mode
	AsrType string // ASR type, e.g. "funasr", "doubao"
	Mode    string // ASR mode, e.g. "online", "offline"

	// ClientState reference for callbacks
	ClientState *ClientState

	// chat-history audio cache: accumulates audio sent to ASR
	HistoryAudioBuffer []float32

	// whether this ASR turn has received the first non-empty text
	ReceivedTextInTurn bool
}

func (a *Asr) Reset() {
	a.AsrResult.Reset()
}

func (a *Asr) CancelWithReason(reason string) {
	a.lock.RLock()
	cancel := a.Cancel
	a.lock.RUnlock()

	if cancel != nil {
		log.Debugf("Asr.CancelWithReason: reason=%s", reason)
		cancel()
	}
}

func (a *Asr) RetireAsrResult(ctx context.Context) (asr_types.StreamingResult, bool, error) {
	defer func() {
		a.Reset()
	}()

	log.Log().Debugf("asr type: %s, mode: %s", a.AsrType, a.Mode)

	// local flag tracking whether the first-character event was sent
	firstTextSent := false
	var emptyResult asr_types.StreamingResult

	for {
		select {
		case <-ctx.Done():
			log.Debugf("RetireAsrResult: ctx done, exit")
			return emptyResult, false, nil
		default:
			// avoid racing on channel select after ctx cancel and using a canceled context result
			select {
			case result, ok := <-a.AsrResultChannel:
				log.Debugf("asr result: %s, ok: %+v, isFinal: %+v, emptyReason: %s, error: %+v", result.Text, ok, result.IsFinal, result.EmptyReason, result.Error)
				if result.Error != nil {
					if result.RetryReason != "" {
						log.Warnf("ASR returned recoverable error(%s), handing to upper layer: %v", result.RetryReason, result.Error)
						return result, true, nil
					}
					return emptyResult, false, result.Error
				}

				// detect first returned character (non-empty text, not yet sent)
				if result.Text != "" && !firstTextSent && a.ClientState != nil && a.ClientState.OnAsrFirstTextCallback != nil {
					firstTextSent = true
					// invoke the callback for the first character
					a.ClientState.OnAsrFirstTextCallback(result.Text, result.IsFinal)
				}

				if a.AsrType == "funasr" &&
					strings.EqualFold(a.Mode, "2pass") &&
					strings.EqualFold(result.Mode, "2pass-online") {
					if result.IsFinal {
						log.Debugf("funasr 2pass-online result mislabeled as final, waiting for 2pass-offline final result")
					}
					continue
				}

				if result.IsFinal {
					return result, true, nil
				}

				if !ok {
					log.Debugf("asr result channel closed")
					return emptyResult, true, nil
				}
			}
		}
	}
}

func (a *Asr) MarkTextReceived() {
	a.lock.Lock()
	defer a.lock.Unlock()
	a.ReceivedTextInTurn = true
}

func (a *Asr) HasReceivedText() bool {
	a.lock.RLock()
	defer a.lock.RUnlock()
	return a.ReceivedTextInTurn
}

func (a *Asr) ResetReceivedText() {
	a.lock.Lock()
	defer a.lock.Unlock()
	a.ReceivedTextInTurn = false
}

func (a *Asr) StopWithReason(reason string) {
	a.lock.Lock()
	defer a.lock.Unlock()

	if a.AsrAudioChannel != nil {
		log.Debugf("Asr.StopWithReason: reason=%s", reason)
		close(a.AsrAudioChannel) // close the ASR audio input channel to stop ASR and return results
		a.AsrAudioChannel = nil  // already closed, so nil it out
	}
}

func (a *Asr) Stop() {
	a.StopWithReason("Asr.Stop")
}

func (a *Asr) HasOpenAudioInput() bool {
	a.lock.RLock()
	defer a.lock.RUnlock()

	return a.AsrAudioChannel != nil
}

func (a *Asr) AddAudioData(pcmFrameData []float32) error {
	a.lock.Lock()
	defer a.lock.Unlock()
	if a.AsrAudioChannel != nil {
		// non-blocking send via select to avoid deadlock when the channel is full
		select {
		case a.AsrAudioChannel <- pcmFrameData:
			// sent successfully; also cache audio for chat history
			a.HistoryAudioBuffer = append(a.HistoryAudioBuffer, pcmFrameData...)
		default:
			// channel full; skip this chunk to avoid blocking/deadlock
			log.Warnf("AsrAudioChannel is full, skipping this audio data")
		}
	}
	return nil
}

// GetHistoryAudio returns a copy of the history audio cache without clearing it
func (a *Asr) GetHistoryAudio() []float32 {
	a.lock.Lock()
	defer a.lock.Unlock()
	if len(a.HistoryAudioBuffer) == 0 {
		return nil
	}
	// return a copy so callers cannot mutate the original
	result := make([]float32, len(a.HistoryAudioBuffer))
	copy(result, a.HistoryAudioBuffer)
	return result
}

// GetHistoryAudioLen returns history audio cache length (sample count)
func (a *Asr) GetHistoryAudioLen() int {
	a.lock.RLock()
	defer a.lock.RUnlock()
	return len(a.HistoryAudioBuffer)
}

// ClearHistoryAudio clears the history audio cache
func (a *Asr) ClearHistoryAudio() {
	a.lock.Lock()
	defer a.lock.Unlock()
	a.HistoryAudioBuffer = nil
}

type AsrAudioBuffer struct {
	PcmData          []float32
	AudioBufferMutex sync.RWMutex
}

func (a *AsrAudioBuffer) AddAsrAudioData(pcmFrameData []float32) error {
	a.AudioBufferMutex.Lock()
	defer a.AudioBufferMutex.Unlock()
	a.PcmData = append(a.PcmData, pcmFrameData...)
	return nil
}

func (a *AsrAudioBuffer) GetAsrDataSize() int {
	a.AudioBufferMutex.RLock()
	defer a.AudioBufferMutex.RUnlock()
	return len(a.PcmData)
}

// GetFrameCount returns frame count (needs frame size for the calculation)
func (a *AsrAudioBuffer) GetFrameCount(frameSize int) int {
	a.AudioBufferMutex.RLock()
	defer a.AudioBufferMutex.RUnlock()
	if frameSize == 0 {
		return 0
	}
	return len(a.PcmData) / frameSize
}

func (a *AsrAudioBuffer) GetAndClearAllData() []float32 {
	a.AudioBufferMutex.Lock()
	defer a.AudioBufferMutex.Unlock()
	pcmData := make([]float32, len(a.PcmData))
	copy(pcmData, a.PcmData)
	a.PcmData = []float32{}
	return pcmData
}

// GetAsrData sliding-window read (needs frame size for the calculation)
func (a *AsrAudioBuffer) GetAsrData(frameCount int, frameSize int) []float32 {
	a.AudioBufferMutex.Lock()
	defer a.AudioBufferMutex.Unlock()
	pcmDataLen := len(a.PcmData)
	retSize := frameCount * frameSize
	if pcmDataLen < retSize {
		retSize = pcmDataLen
	}
	pcmData := make([]float32, retSize)
	copy(pcmData, a.PcmData[pcmDataLen-retSize:])
	return pcmData
}

// RemoveAsrAudioData removes a given number of frames (needs frame size for the calculation)
func (a *AsrAudioBuffer) RemoveAsrAudioData(frameCount int, frameSize int) {
	a.AudioBufferMutex.Lock()
	defer a.AudioBufferMutex.Unlock()
	removeSize := frameCount * frameSize
	if removeSize > len(a.PcmData) {
		removeSize = len(a.PcmData)
	}
	a.PcmData = a.PcmData[removeSize:]
}

func (a *AsrAudioBuffer) ClearAsrAudioData() {
	a.AudioBufferMutex.Lock()
	defer a.AudioBufferMutex.Unlock()
	a.PcmData = nil
}
