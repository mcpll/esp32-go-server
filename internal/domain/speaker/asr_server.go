package speaker

import (
	"context"
	"fmt"
	"sync"

	log "xiaozhi-esp32-server-golang/logger"
)

// AsrServerProvider asr_server speaker recognition provider
type AsrServerProvider struct {
	streamingClient *StreamingClient
	threshold       float32 // speaker recognition threshold
	isActive        bool
	mutex           sync.Mutex
}

// NewAsrServerProvider creates an asr_server speaker recognition provider
func NewAsrServerProvider(config map[string]interface{}) (*AsrServerProvider, error) {
	baseURL, ok := config["base_url"].(string)
	if !ok || baseURL == "" {
		return nil, fmt.Errorf("配置中缺少 service.base_url 字段")
	}

	// Read threshold config; default 0.4
	threshold := float32(0.4)
	if thresholdVal, ok := config["threshold"]; ok {
		switch v := thresholdVal.(type) {
		case float64:
			threshold = float32(v)
		case float32:
			threshold = v
		case int:
			threshold = float32(v)
		case int64:
			threshold = float32(v)
		}
		// Validate threshold range
		if threshold < 0 || threshold > 1 {
			log.Warnf("threshold %.4f out of range [0.0, 1.0], using default 0.4", threshold)
			threshold = 0.4
		}
	}

	streamingClient := NewStreamingClient(baseURL)
	return &AsrServerProvider{
		streamingClient: streamingClient,
		threshold:       threshold,
		isActive:        false,
	}, nil
}

// StartStreaming starts streaming recognition
func (p *AsrServerProvider) StartStreaming(ctx context.Context, sampleRate int, agentId string) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if p.isActive {
		return nil // Already active; return
	}

	err := p.streamingClient.Connect(sampleRate, agentId, p.threshold)
	if err != nil {
		log.Warnf("failed to start speaker recognition stream: %v", err)
		return err
	}

	p.isActive = true
	log.Debugf("speaker recognition stream started, sample_rate: %d Hz, agent_id: %s, threshold: %.4f", sampleRate, agentId, p.threshold)
	return nil
}

// SendAudioChunk sends an audio chunk
func (p *AsrServerProvider) SendAudioChunk(ctx context.Context, pcmData []float32) error {
	p.mutex.Lock()
	isActive := p.isActive
	streamingClient := p.streamingClient
	p.mutex.Unlock()

	if !isActive {
		return nil // Not active; ignore silently
	}

	err := streamingClient.SendAudioChunk(pcmData)
	if err != nil {
		log.Warnf("failed to send audio chunk to speaker recognition service: %v", err)
		// On send failure, mark inactive
		p.mutex.Lock()
		p.isActive = false
		p.mutex.Unlock()
		return err
	}

	return nil
}

// FinishAndIdentify finishes recognition and returns the result
func (p *AsrServerProvider) FinishAndIdentify(ctx context.Context) (*IdentifyResult, error) {
	p.mutex.Lock()
	if !p.isActive {
		p.mutex.Unlock()
		return nil, nil // Not active; return nil
	}
	p.isActive = false
	streamingClient := p.streamingClient
	p.mutex.Unlock()

	result, err := streamingClient.FinishAndIdentify(ctx)

	if err != nil {
		log.Warnf("failed to get speaker recognition result: %v", err)
		return nil, err
	}

	return result, nil
}

// PeekAndIdentify returns an intermediate result without ending the turn
// Returns: recognition result, whether server-debounced, error
func (p *AsrServerProvider) PeekAndIdentify(ctx context.Context, requestID string) (*IdentifyResult, bool, error) {
	select {
	case <-ctx.Done():
		return nil, false, ctx.Err()
	default:
	}

	p.mutex.Lock()
	isActive := p.isActive
	streamingClient := p.streamingClient
	p.mutex.Unlock()

	if !isActive {
		return nil, false, nil
	}

	result, throttled, err := streamingClient.PeekAndIdentify(ctx, requestID)
	if err != nil {
		if !streamingClient.IsConnected() {
			p.mutex.Lock()
			p.isActive = false
			p.mutex.Unlock()
		}
		log.Warnf("failed to get intermediate speaker recognition result: %v", err)
		return nil, throttled, err
	}

	return result, throttled, nil
}

// Close closes the speaker provider
func (p *AsrServerProvider) Close() error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	p.isActive = false
	if p.streamingClient != nil {
		return p.streamingClient.Close()
	}
	return nil
}

// IsActive reports whether active
func (p *AsrServerProvider) IsActive() bool {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	return p.isActive
}
