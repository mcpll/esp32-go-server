package tts

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"xiaozhi-esp32-server-golang/constants"
	"xiaozhi-esp32-server-golang/internal/domain/tts/cosyvoice"
	"xiaozhi-esp32-server-golang/internal/domain/tts/doubao"
	"xiaozhi-esp32-server-golang/internal/domain/tts/edge"
	"xiaozhi-esp32-server-golang/internal/domain/tts/edge_offline"
	"xiaozhi-esp32-server-golang/internal/domain/tts/minimax"
	"xiaozhi-esp32-server-golang/internal/domain/tts/openai"
	"xiaozhi-esp32-server-golang/internal/domain/tts/qwen"
	"xiaozhi-esp32-server-golang/internal/domain/tts/streaming"
	"xiaozhi-esp32-server-golang/internal/domain/tts/xiaozhi"
	"xiaozhi-esp32-server-golang/internal/domain/tts/xunfei"
	"xiaozhi-esp32-server-golang/internal/domain/tts/xunfei_super_tts"
	"xiaozhi-esp32-server-golang/internal/domain/tts/zhipu"
)

// Base TTS provider interface (no Context methods)
type BaseTTSProvider interface {
	TextToSpeech(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) ([][]byte, error)
	TextToSpeechStream(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) (outputChan chan []byte, err error)
}

// DualStreamProvider optional interface: streaming text in and audio out. Implement if supported.
type DualStreamProvider interface {
	StreamingSynthesize(ctx context.Context, textChan <-chan string, sampleRate int, channels int, frameDuration int) (outputChan chan streaming.SynthesisEvent, err error)
}

// Full TTS provider interface (includes Context methods)
type TTSProvider interface {
	BaseTTSProvider
	// SetVoice dynamically sets voice parameters
	// voiceConfig: map of voice settings, e.g. {"voice": "xxx"} or {"spk_id": "xxx"}
	SetVoice(voiceConfig map[string]interface{}) error
	// Close releases resources/connections
	Close() error
	// IsValid checks whether the resource is valid (connection alive, etc.)
	IsValid() bool
}

// GetTTSProvider returns a full TTS provider (Context-capable)
// providerName: config_id/provider or pool key (e.g. "edge_tts:zh-CN-XiaoxiaoNeural")
// config: map parsed from configs.json_data in the database
// Prefer provider from config; else parse providerName (part before ":")
func GetTTSProvider(providerName string, config map[string]interface{}) (TTSProvider, error) {
	effectiveName := providerName
	if configProvider, ok := config["provider"].(string); ok && configProvider != "" {
		effectiveName = configProvider
	}
	// Pool key format "provider:voiceID"; take left part as provider type
	if idx := strings.Index(effectiveName, ":"); idx > 0 {
		effectiveName = effectiveName[:idx]
	}
	var baseProvider BaseTTSProvider

	switch effectiveName {
	case constants.TtsTypeDoubao:
		baseProvider = doubao.NewDoubaoTTSProvider(config)
	case constants.TtsTypeDoubaoWS:
		baseProvider = doubao.NewDoubaoWSProvider(config)
	case constants.TtsTypeCosyvoice:
		baseProvider = cosyvoice.NewCosyVoiceTTSProvider(config)
	case constants.TtsTypeEdge:
		baseProvider = edge.NewEdgeTTSProvider(config)
	case constants.TtsTypeEdgeOffline:
		baseProvider = edge_offline.NewEdgeOfflineTTSProvider(config)
	case constants.TtsTypeXiaozhi:
		baseProvider = xiaozhi.NewXiaozhiProvider(config)
	case constants.TtsTypeXunfei:
		baseProvider = xunfei.NewXunfeiTTSProvider(config)
	case constants.TtsTypeXunfeiSuper:
		baseProvider = xunfei_super_tts.NewXunfeiSuperTTSProvider(config)
	case constants.TtsTypeOpenAI:
		baseProvider = openai.NewOpenAITTSProvider(config)
	case constants.TtsTypeZhipu:
		baseProvider = zhipu.NewZhipuTTSProvider(config)
	case constants.TtsTypeMinimax:
		baseProvider = minimax.NewMinimaxTTSProvider(config)
	case constants.TtsTypeAliyunQwen:
		baseProvider = qwen.NewQwenTTSProvider(config)
	case constants.TtsTypeIndexTTSVLLM:
		baseProvider = openai.NewOpenAITTSProvider(buildIndexTTSOpenAIConfig(config))
	default:
		return nil, fmt.Errorf("不支持的TTS提供者: %s", effectiveName)
	}

	if baseProvider == nil {
		return nil, fmt.Errorf("无法创建TTS提供者: %s", effectiveName)
	}

	// Wrap base provider with adapter into full TTSProvider
	provider := &ContextTTSAdapter{baseProvider}

	return provider, nil
}

func buildIndexTTSOpenAIConfig(config map[string]interface{}) map[string]interface{} {
	const (
		defaultIndexTTSURL   = "http://127.0.0.1:7860/audio/speech"
		defaultIndexTTSModel = "indextts-vllm"
	)

	normalized := make(map[string]interface{}, len(config)+4)
	for k, v := range config {
		normalized[k] = v
	}

	apiURL, _ := normalized["api_url"].(string)
	apiURL = strings.TrimSpace(apiURL)
	if apiURL == "" {
		apiURL = defaultIndexTTSURL
	} else {
		parsed, err := url.Parse(apiURL)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			trimmed := strings.TrimRight(apiURL, "/")
			if !strings.HasSuffix(strings.ToLower(trimmed), "/audio/speech") {
				trimmed += "/audio/speech"
			}
			apiURL = trimmed
		} else {
			if strings.TrimSpace(parsed.Path) == "" || parsed.Path == "/" {
				parsed.Path = "/audio/speech"
				parsed.RawPath = ""
				apiURL = parsed.String()
			}
		}
	}
	normalized["api_url"] = strings.TrimRight(apiURL, "/")

	if model, _ := normalized["model"].(string); strings.TrimSpace(model) == "" {
		normalized["model"] = defaultIndexTTSModel
	}
	if responseFormat, _ := normalized["response_format"].(string); strings.TrimSpace(responseFormat) == "" {
		normalized["response_format"] = "wav"
	}
	if _, exists := normalized["stream"]; !exists {
		normalized["stream"] = false
	}
	if _, exists := normalized["speed"]; !exists {
		normalized["speed"] = float64(1.0)
	}

	return normalized
}

// ContextTTSAdapter adds Context support to a base TTS provider
type ContextTTSAdapter struct {
	Provider BaseTTSProvider
}

// StreamingSynthesize proxies to the dual-stream synthesize API
func (a *ContextTTSAdapter) StreamingSynthesize(ctx context.Context, textChan <-chan string, sampleRate int, channels int, frameDuration int) (outputChan chan streaming.SynthesisEvent, err error) {
	// Check whether the underlying Provider supports dual-stream
	if dsProvider, ok := a.Provider.(DualStreamProvider); ok {
		return dsProvider.StreamingSynthesize(ctx, textChan, sampleRate, channels, frameDuration)
	}
	return nil, fmt.Errorf("底层 Provider 不支持双流式合成")
}

// TextToSpeech proxies to the original provider
func (a *ContextTTSAdapter) TextToSpeech(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) ([][]byte, error) {
	return a.Provider.TextToSpeech(ctx, text, sampleRate, channels, frameDuration)
}

// TextToSpeechStream proxies to the original provider
func (a *ContextTTSAdapter) TextToSpeechStream(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) (outputChan chan []byte, err error) {
	return a.Provider.TextToSpeechStream(ctx, text, sampleRate, channels, frameDuration)
}

// SetVoice proxies to the underlying Provider SetVoice
func (a *ContextTTSAdapter) SetVoice(voiceConfig map[string]interface{}) error {
	// If underlying Provider implements SetVoice, call it
	if setter, ok := a.Provider.(interface {
		SetVoice(map[string]interface{}) error
	}); ok {
		return setter.SetVoice(voiceConfig)
	}
	// Otherwise return unsupported error
	return fmt.Errorf("底层 Provider 不支持 SetVoice 方法")
}

// TextToSpeechWithContext Context-aware text-to-speech
func (a *ContextTTSAdapter) TextToSpeechWithContext(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) ([][]byte, error) {
	// Check whether provider supports Context version directly
	if provider, ok := a.Provider.(interface {
		TextToSpeechWithContext(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) ([][]byte, error)
	}); ok {
		// Provider supports Context version directly
		return provider.TextToSpeechWithContext(ctx, text, sampleRate, channels, frameDuration)
	}

	// Else use standard version with goroutine/channel for context control
	resultChan := make(chan struct {
		frames [][]byte
		err    error
	})

	go func() {
		frames, err := a.Provider.TextToSpeech(ctx, text, sampleRate, channels, frameDuration)
		select {
		case <-ctx.Done():
			// Context canceled; do not send result
			return
		case resultChan <- struct {
			frames [][]byte
			err    error
		}{frames, err}:
			// Result sent
		}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-resultChan:
		return result.frames, result.err
	}
}

// TextToSpeechStreamWithContext Context-aware streaming TTS
func (a *ContextTTSAdapter) TextToSpeechStreamWithContext(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) (outputChan chan []byte, cancelFunc func(), err error) {
	// Check whether provider supports Context version directly
	if provider, ok := a.Provider.(interface {
		TextToSpeechStreamWithContext(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) (chan []byte, func(), error)
	}); ok {
		// Provider supports Context version directly
		return provider.TextToSpeechStreamWithContext(ctx, text, sampleRate, channels, frameDuration)
	}

	// Else use standard version with a wrapper for context cancel
	streamCtx, cancel := context.WithCancel(ctx)
	streamChan, err := a.Provider.TextToSpeechStream(streamCtx, text, sampleRate, channels, frameDuration)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	cancelFunc = cancel

	// New output channel for forward and cancel handling
	outputChan = make(chan []byte, 10)

	// Goroutine to forward data and watch context cancel
	go func() {
		defer close(outputChan)

		for {
			select {
			case <-streamCtx.Done():
				// Context canceled; call original cancel and exit
				cancelFunc()
				return
			case frame, ok := <-streamChan:
				if !ok {
					// Original channel closed
					return
				}
				// Forward data
				select {
				case <-streamCtx.Done():
					// Context canceled
					cancelFunc()
					return
				case outputChan <- frame:
					// Data forwarded successfully
				}
			}
		}
	}()

	return outputChan, cancelFunc, nil
}

// Close releases resources
func (a *ContextTTSAdapter) Close() error {
	// If underlying Provider implements Close, call it
	if closer, ok := a.Provider.(interface {
		Close() error
	}); ok {
		return closer.Close()
	}
	return nil
}

// IsValid checks whether the resource is valid
func (a *ContextTTSAdapter) IsValid() bool {
	// If underlying Provider implements IsValid, call it
	if validator, ok := a.Provider.(interface {
		IsValid() bool
	}); ok {
		return validator.IsValid()
	}
	// Else check whether Provider is nil
	return a.Provider != nil
}
