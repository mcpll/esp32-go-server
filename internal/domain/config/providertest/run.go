// Package providertest runs one ASR, LLM or TTS provider the way the upstream
// config test did, and returns the vendor's own error text when the call fails.
package providertest

import (
	"context"
	"errors"
	"fmt"
	"time"

	"xiaozhi-esp32-server-golang/internal/domain/asr"
	"xiaozhi-esp32-server-golang/internal/domain/config/pocketbase"
	"xiaozhi-esp32-server-golang/internal/domain/llm"
	"xiaozhi-esp32-server-golang/internal/domain/tts"

	"github.com/cloudwego/eino/schema"
)

const stageTimeout = 20 * time.Second

// FromCommand runs a provider_test payload. The config is merged onto the viper
// section first, so the API key stays in the server config.
func FromCommand(ctx context.Context, payload map[string]any) (any, error) {
	stage, _ := payload["stage"].(string)
	provider, _ := payload["provider"].(string)
	config, _ := payload["config"].(map[string]any)
	if err := Run(ctx, stage, provider, pocketbase.MergeStage(stage, provider, config)); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true}, nil
}

// Run calls one stage and returns nil when the provider answers.
func Run(ctx context.Context, stage, provider string, config map[string]any) error {
	if config == nil {
		config = map[string]any{}
	}
	switch stage {
	case "asr":
		return testASR(ctx, provider, config)
	case "llm":
		return testLLM(ctx, provider, config)
	case "tts":
		return testTTS(ctx, provider, config)
	default:
		return fmt.Errorf("unknown stage %q", stage)
	}
}

func testASR(ctx context.Context, provider string, config map[string]any) error {
	engine, err := asr.NewAsrProvider(provider, config)
	if err != nil {
		return err
	}
	defer engine.Close()

	ctx, cancel := context.WithTimeout(ctx, stageTimeout)
	defer cancel()
	audio := make(chan []float32, 1)
	audio <- make([]float32, 3200)
	close(audio)
	results, err := engine.StreamingRecognize(ctx, audio)
	if err != nil {
		return err
	}
	for result := range results {
		if result.Error != nil {
			return result.Error
		}
	}
	return nil
}

func testLLM(ctx context.Context, provider string, config map[string]any) error {
	engine, err := llm.GetLLMProvider(provider, config)
	if err != nil {
		return err
	}
	defer engine.Close()

	ctx, cancel := context.WithTimeout(ctx, stageTimeout)
	defer cancel()
	messages := engine.ResponseWithContext(ctx, "provider_test", []*schema.Message{
		{Role: schema.User, Content: "ciao"},
	}, nil)
	for msg := range messages {
		if msg == nil {
			continue
		}
		if llm.IsLLMErrorMessage(msg) {
			text := llm.LLMErrorMessage(msg)
			if text == "" {
				return errors.New("llm returned an empty error")
			}
			return errors.New(text)
		}
		return nil
	}
	if ctx.Err() != nil {
		return fmt.Errorf("llm timed out")
	}
	return errors.New("llm returned no response")
}

func testTTS(ctx context.Context, provider string, config map[string]any) error {
	engine, err := tts.GetTTSProvider(provider, config)
	if err != nil {
		return err
	}
	defer engine.Close()

	ctx, cancel := context.WithTimeout(ctx, stageTimeout)
	defer cancel()
	frames, err := engine.TextToSpeech(ctx, "ciao", 24000, 1, 60)
	if err != nil {
		return err
	}
	total := 0
	for _, frame := range frames {
		total += len(frame)
	}
	if total == 0 {
		return errors.New("tts returned no audio")
	}
	return nil
}
