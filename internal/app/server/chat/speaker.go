package chat

import (
	"context"

	"xiaozhi-esp32-server-golang/internal/domain/speaker"
)

// SpeakerManager speaker recognition manager (wraps SpeakerProvider)
type SpeakerManager struct {
	provider speaker.SpeakerProvider
}

type peekableSpeakerProvider interface {
	PeekAndIdentify(ctx context.Context, requestID string) (*speaker.IdentifyResult, bool, error)
}

// NewSpeakerManager creates a speaker manager
func NewSpeakerManager(provider speaker.SpeakerProvider) *SpeakerManager {
	return &SpeakerManager{
		provider: provider,
	}
}

// StartStreaming starts streaming recognition
func (sm *SpeakerManager) StartStreaming(ctx context.Context, sampleRate int, agentId string) error {
	return sm.provider.StartStreaming(ctx, sampleRate, agentId)
}

// SendAudioChunk sends an audio chunk
func (sm *SpeakerManager) SendAudioChunk(ctx context.Context, pcmData []float32) error {
	return sm.provider.SendAudioChunk(ctx, pcmData)
}

// FinishAndIdentify finishes recognition and returns the result
func (sm *SpeakerManager) FinishAndIdentify(ctx context.Context) (*speaker.IdentifyResult, error) {
	return sm.provider.FinishAndIdentify(ctx)
}

// Close closes the speaker manager
func (sm *SpeakerManager) Close() error {
	return sm.provider.Close()
}

// IsActive reports whether it is active
func (sm *SpeakerManager) IsActive() bool {
	return sm.provider.IsActive()
}

// PeekAndIdentify returns an intermediate speaker result without ending the turn
// returns: result, whether server debounce applied, error
func (sm *SpeakerManager) PeekAndIdentify(ctx context.Context, requestID string) (*speaker.IdentifyResult, bool, error) {
	if sm == nil || sm.provider == nil {
		return nil, false, nil
	}
	peekProvider, ok := sm.provider.(peekableSpeakerProvider)
	if !ok {
		return nil, false, nil
	}
	return peekProvider.PeekAndIdentify(ctx, requestID)
}
