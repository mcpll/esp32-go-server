package asr

import (
	"context"

	"xiaozhi-esp32-server-golang/internal/domain/asr/groq_whisper"
	"xiaozhi-esp32-server-golang/internal/domain/asr/types"
	log "xiaozhi-esp32-server-golang/logger"
)

// GroqWhisperAdapter adapts Groq Whisper to AsrProvider.
type GroqWhisperAdapter struct {
	engine *groq_whisper.ASR
}

// NewGroqWhisperAdapter creates the adapter.
func NewGroqWhisperAdapter(config map[string]interface{}) (AsrProvider, error) {
	cfg := groq_whisper.ConfigFromMap(config)
	log.Log().Infof("groq whisper asr config: model=%s language=%s api_url=%s", cfg.Model, cfg.Language, cfg.APIURL)
	engine, err := groq_whisper.NewASR(cfg)
	if err != nil {
		return nil, err
	}
	return &GroqWhisperAdapter{engine: engine}, nil
}

func (a *GroqWhisperAdapter) Process(pcmData []float32) (string, error) {
	return a.engine.Process(pcmData)
}

func (a *GroqWhisperAdapter) StreamingRecognize(ctx context.Context, audioStream <-chan []float32) (chan types.StreamingResult, error) {
	return a.engine.StreamingRecognize(ctx, audioStream)
}

func (a *GroqWhisperAdapter) Close() error {
	if a.engine != nil {
		return a.engine.Close()
	}
	return nil
}

func (a *GroqWhisperAdapter) IsValid() bool {
	return a != nil && a.engine != nil && a.engine.IsValid()
}
