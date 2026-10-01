package speaker

import (
	"context"
)

// SpeakerProvider is the speaker recognition provider interface
type SpeakerProvider interface {
	// StartStreaming starts streaming recognition
	StartStreaming(ctx context.Context, sampleRate int, agentId string) error

	// SendAudioChunk sends an audio data chunk
	SendAudioChunk(ctx context.Context, audioData []float32) error

	// FinishAndIdentify finishes input and returns the recognition result
	FinishAndIdentify(ctx context.Context) (*IdentifyResult, error)

	// IsActive reports whether the provider is active
	IsActive() bool

	// Close closes the connection
	Close() error
}

// GetSpeakerProvider returns a speaker recognition provider
func GetSpeakerProvider(config map[string]interface{}) (SpeakerProvider, error) {
	return NewAsrServerProvider(config)
}
