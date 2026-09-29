package webrtc_vad

import (
	"fmt"
	"time"
	"xiaozhi-esp32-server-golang/internal/util"
)

// WebRTCVADConfig WebRTC VAD config
type WebRTCVADConfig struct {
	SampleRate int
	Mode       int
}

// WebRTCVADFactory WebRTC VAD factory implementing ResourceFactory
type WebRTCVADFactory struct {
	config WebRTCVADConfig
}

// NewWebRTCVADFactory creates a WebRTC VAD factory
func NewWebRTCVADFactory(config WebRTCVADConfig) *WebRTCVADFactory {
	if config.SampleRate == 0 {
		config.SampleRate = DefaultSampleRate
	}
	if config.Mode < 0 || config.Mode > 3 {
		config.Mode = DefaultMode
	}

	return &WebRTCVADFactory{
		config: config,
	}
}

// Create creates a new WebRTC VAD resource
func (f *WebRTCVADFactory) Create() (util.Resource, error) {
	vad := &WebRTCVAD{
		sampleRate: f.config.SampleRate,
		mode:       f.config.Mode,
		lastUsed:   time.Now(),
	}

	// initialize the instance
	if err := vad.init(); err != nil {
		return nil, fmt.Errorf("failed to initialize WebRTC VAD: %w", err)
	}

	return vad, nil
}

// Validate validates whether the resource is valid
func (f *WebRTCVADFactory) Validate(resource util.Resource) bool {
	vad, ok := resource.(*WebRTCVAD)
	if !ok {
		return false
	}
	return vad.IsValid()
}

// Reset resets resource state
func (f *WebRTCVADFactory) Reset(resource util.Resource) error {
	vad, ok := resource.(*WebRTCVAD)
	if !ok {
		return fmt.Errorf("invalid resource type")
	}
	return vad.Reset()
}
