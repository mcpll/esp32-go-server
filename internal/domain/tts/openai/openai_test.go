package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"xiaozhi-esp32-server-golang/internal/util"
)

func TestOpenAITTS(t *testing.T) {
	// Skip live network tests unless the env var is set
	if os.Getenv("RUN_OPENAI_TEST") != "1" {
		t.Skip("skip OpenAI API test; set RUN_OPENAI_TEST=1 to enable")
	}

	// Get API key from environment
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		t.Skip("skip OpenAI API test; set OPENAI_API_KEY")
	}

	config := map[string]interface{}{
		"api_key":         apiKey,
		"api_url":         "https://api.openai.com/v1/audio/speech",
		"model":           "tts-1",
		"voice":           "alloy",
		"response_format": "mp3",
		"speed":           1.0,
		"frame_duration":  float64(60),
	}

	provider := NewOpenAITTSProvider(config)

	// Test text-to-speech
	t.Run("TestTextToSpeech", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		frames, err := provider.TextToSpeech(ctx, "Hello, this is a test of OpenAI text to speech.", 16000, 1, 60)
		if err != nil {
			t.Fatalf("TextToSpeech failed: %v", err)
		}

		if len(frames) == 0 {
			t.Error("no audio frames returned")
		}

		t.Logf("generated %d audio frames", len(frames))
	})

	// Test streaming text-to-speech
	t.Run("TestTextToSpeechStream", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		outputChan, err := provider.TextToSpeechStream(ctx, "Hello, this is a test of OpenAI streaming text to speech.", 16000, 1, 60)
		if err != nil {
			t.Fatalf("TextToSpeechStream failed: %v", err)
		}

		// Receive all frames
		var receivedFrames [][]byte
		timeout := time.After(20 * time.Second)

	receiveLoop:
		for {
			select {
			case frame, ok := <-outputChan:
				if !ok {
					break receiveLoop
				}
				receivedFrames = append(receivedFrames, frame)
			case <-timeout:
				t.Error("timed out waiting for audio frames")
				break receiveLoop
			}
		}

		if len(receivedFrames) == 0 {
			t.Error("received no audio frames")
		}

		t.Logf("received %d audio frames", len(receivedFrames))
	})

	// Test different voices
	t.Run("TestDifferentVoices", func(t *testing.T) {
		voices := []string{"alloy", "echo", "fable", "onyx", "nova", "shimmer"}

		for _, voice := range voices {
			t.Run(voice, func(t *testing.T) {
				config := map[string]interface{}{
					"api_key":         apiKey,
					"model":           "tts-1",
					"voice":           voice,
					"response_format": "mp3",
					"speed":           1.0,
				}

				provider := NewOpenAITTSProvider(config)
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()

				frames, err := provider.TextToSpeech(ctx, "Testing voice: "+voice, 16000, 1, 60)
				if err != nil {
					t.Errorf("voice %s failed: %v", voice, err)
					return
				}

				if len(frames) == 0 {
					t.Errorf("voice %s returned no audio frames", voice)
				}

				t.Logf("voice %s generated %d audio frames", voice, len(frames))
			})
		}
	})

	// Test different speeds
	t.Run("TestDifferentSpeeds", func(t *testing.T) {
		speeds := []float64{0.5, 1.0, 1.5, 2.0}

		for _, speed := range speeds {
			t.Run(string(rune(speed)), func(t *testing.T) {
				config := map[string]interface{}{
					"api_key":         apiKey,
					"model":           "tts-1",
					"voice":           "alloy",
					"response_format": "mp3",
					"speed":           speed,
				}

				provider := NewOpenAITTSProvider(config)
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()

				frames, err := provider.TextToSpeech(ctx, "Testing speed", 16000, 1, 60)
				if err != nil {
					t.Errorf("speed %.1f failed: %v", speed, err)
					return
				}

				if len(frames) == 0 {
					t.Errorf("speed %.1f returned no audio frames", speed)
				}

				t.Logf("speed %.1f generated %d audio frames", speed, len(frames))
			})
		}
	})
}

// TestOpenAITTSProviderDefaults tests default values
func TestOpenAITTSProviderDefaults(t *testing.T) {
	config := map[string]interface{}{
		"api_key": "test-key",
	}

	provider := NewOpenAITTSProvider(config)

	if provider.APIURL != "https://api.openai.com/v1/audio/speech" {
		t.Errorf("default API URL = %s, want https://api.openai.com/v1/audio/speech", provider.APIURL)
	}

	if provider.Model != "tts-1" {
		t.Errorf("default model = %s, want tts-1", provider.Model)
	}

	if provider.Voice != "alloy" {
		t.Errorf("default voice = %s, want alloy", provider.Voice)
	}

	if provider.ResponseFormat != "mp3" {
		t.Errorf("default response format = %s, want mp3", provider.ResponseFormat)
	}

	if provider.Speed != 1.0 {
		t.Errorf("default speed = %.1f, want 1.0", provider.Speed)
	}
}

func TestOpenAITTSProviderSupportsOpusResponse(t *testing.T) {
	sampleRate := 16000
	pcm := make([]int16, sampleRate/2)
	for i := range pcm {
		if i%32 < 16 {
			pcm[i] = 2400
		} else {
			pcm[i] = -2400
		}
	}

	opusBytes, err := util.PCM16ToOggOpus(pcm, sampleRate, 1, 20)
	if err != nil {
		t.Fatalf("failed to build test Ogg Opus: %v", err)
	}

	requestErrCh := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		var req openAIRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			requestErrCh <- err
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if req.ResponseFormat != "opus" {
			requestErrCh <- fmt.Errorf("response_format = %s, want opus", req.ResponseFormat)
			http.Error(w, "unexpected response_format", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "audio/ogg")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(opusBytes)
	}))
	defer server.Close()

	provider := NewOpenAITTSProvider(map[string]interface{}{
		"api_url":         server.URL,
		"model":           "tts-1",
		"voice":           "alloy",
		"response_format": "opus",
		"speed":           1.0,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	outputChan, err := provider.TextToSpeechStream(ctx, "opus output test", sampleRate, 1, 60)
	if err != nil {
		t.Fatalf("TextToSpeechStream returned an error: %v", err)
	}

	frameCount := 0
	for frame := range outputChan {
		if len(frame) == 0 {
			t.Fatal("empty Opus frame")
		}
		frameCount++
	}

	if frameCount == 0 {
		t.Fatal("no Opus frames")
	}

	select {
	case err := <-requestErrCh:
		t.Fatalf("mock server check failed: %v", err)
	default:
	}
}

func TestRequestBodySendsStyleAndOmitsDefaultSpeed(t *testing.T) {
	provider := NewOpenAITTSProvider(map[string]interface{}{
		"model": "google/gemini-3.8-flash-tts",
		"voice": "Algenib",
		"style": "very slow, gravelly",
		"speed": 1.0,
	})

	raw, err := json.Marshal(provider.requestBody("ciao"))
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if _, ok := body["speed"]; ok {
		t.Fatalf("default speed was sent: %s", raw)
	}
	options := body["provider"].(map[string]any)["options"].(map[string]any)
	studio := options["google-ai-studio"].(map[string]any)
	meta := studio["speech_metadata"].(map[string]any)
	if meta["style"] != "very slow, gravelly" {
		t.Fatalf("style = %#v", meta["style"])
	}
}
