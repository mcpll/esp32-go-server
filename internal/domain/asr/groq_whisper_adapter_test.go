package asr

import (
	"testing"

	"xiaozhi-esp32-server-golang/constants"
)

func TestNewAsrProviderGroqWhisper(t *testing.T) {
	p, err := NewAsrProvider(constants.AsrTypeGroqWhisper, map[string]interface{}{
		"api_key":  "gsk-test",
		"api_url":  "http://example.test/v1/audio/transcriptions",
		"model":    "whisper-large-v3-turbo",
		"language": "it",
	})
	if err != nil {
		t.Fatalf("NewAsrProvider: %v", err)
	}
	if !p.IsValid() {
		t.Fatal("provider should be valid")
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
}
