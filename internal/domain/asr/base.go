package asr

import (
	"context"
	"fmt"

	"xiaozhi-esp32-server-golang/constants"
	"xiaozhi-esp32-server-golang/internal/domain/asr/doubao"
	"xiaozhi-esp32-server-golang/internal/domain/asr/types"
	log "xiaozhi-esp32-server-golang/logger"
)

// Asr speech recognition interface
type AsrProvider interface {
	// Process handles a full audio segment at once and returns the complete result
	Process(pcmData []float32) (string, error)

	// StreamingRecognize streaming recognition interface
	// Input audio via audioStream; results via the returned channel
	// When audioStream is closed, input ends; the final result is sent then the channel is closed
	// Use ctx to cancel or time out recognition
	StreamingRecognize(ctx context.Context, audioStream <-chan []float32) (chan types.StreamingResult, error)
	// Close releases resources/connections
	Close() error
	// IsValid checks whether the resource is still valid
	IsValid() bool
}

// NewAsrProvider creates a new ASR instance
// asrType: ASR engine type, currently supports "funasr"
// config: ASR engine config as map[string]interface{}
func NewAsrProvider(asrType string, config map[string]interface{}) (AsrProvider, error) {
	// Prefer provider from config; otherwise use the parameter
	if configProvider, ok := config["provider"].(string); ok && configProvider != "" {
		asrType = configProvider
	}
	switch asrType {
	case constants.AsrTypeFunAsr:
		return NewFunasrAdapter(config)
	case constants.AsrTypeAliyunFunASR:
		return NewAliyunFunASRAdapter(config)
	case constants.AsrTypeDoubao:
		log.Info("using Doubao ASR provider")
		provider, err := doubao.NewDoubaoV2Adapter(config)
		if err != nil {
			log.Errorf("failed to create Doubao ASR adapter: %v", err)
		} else {
			log.Info("Doubao ASR adapter created successfully")
		}
		return provider, err
	case constants.AsrTypeAliyunQwen3:
		log.Info("using Alibaba Cloud Qwen3 ASR provider")
		provider, err := NewAliyunQwen3Adapter(config)
		if err != nil {
			log.Errorf("failed to create Alibaba Cloud Qwen3 ASR adapter: %v", err)
		} else {
			log.Info("Alibaba Cloud Qwen3 ASR adapter created successfully")
		}
		return provider, err
	case constants.AsrTypeXunfei:
		log.Info("using Xunfei ASR provider")
		provider, err := NewXunfeiAdapter(config)
		if err != nil {
			log.Errorf("failed to create Xunfei ASR adapter: %v", err)
		} else {
			log.Info("Xunfei ASR adapter created successfully")
		}
		return provider, err
	default:
		return nil, fmt.Errorf("不支持的ASR引擎类型: %s，目前仅支持 'funasr', 'aliyun_funasr', 'doubao', 'aliyun_qwen3', 'xunfei'", asrType)
	}
}
