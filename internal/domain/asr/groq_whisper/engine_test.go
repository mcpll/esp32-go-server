package groq_whisper

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestConfigFromMapUsesGROQEnvWhenKeyEmpty(t *testing.T) {
	t.Setenv("GROQ_API_KEY", "gsk-test-from-env")
	cfg := ConfigFromMap(map[string]interface{}{
		"model":    "whisper-large-v3-turbo",
		"language": "it",
	})
	if cfg.APIKey != "gsk-test-from-env" {
		t.Fatalf("APIKey = %q", cfg.APIKey)
	}
	if cfg.APIURL != defaultAPIURL {
		t.Fatalf("APIURL = %q", cfg.APIURL)
	}
	if cfg.Language != "it" {
		t.Fatalf("Language = %q", cfg.Language)
	}
}

func TestStreamingRecognizePostsWavAndReturnsText(t *testing.T) {
	var gotAuth, gotModel, gotLang string
	var gotFileName string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("ParseMultipartForm: %v", err)
			http.Error(w, err.Error(), 400)
			return
		}
		gotModel = r.FormValue("model")
		gotLang = r.FormValue("language")
		file, hdr, err := r.FormFile("file")
		if err != nil {
			t.Errorf("FormFile: %v", err)
			http.Error(w, err.Error(), 400)
			return
		}
		defer file.Close()
		gotFileName = hdr.Filename
		body, _ := io.ReadAll(file)
		if len(body) < 44 || string(body[:4]) != "RIFF" {
			t.Errorf("expected WAV, got %d bytes", len(body))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":" ciao mondo "}`))
	}))
	defer srv.Close()

	asr, err := NewASR(Config{
		APIKey:         "gsk-test",
		APIURL:         srv.URL,
		Model:          "whisper-large-v3-turbo",
		Language:       "it",
		ResponseFormat: "json",
		SampleRate:     16000,
	})
	if err != nil {
		t.Fatal(err)
	}

	audio := make(chan []float32, 1)
	audio <- []float32{0.1, -0.1, 0.2}
	close(audio)

	results, err := asr.StreamingRecognize(context.Background(), audio)
	if err != nil {
		t.Fatal(err)
	}
	r := <-results
	if r.Error != nil {
		t.Fatalf("result error: %v", r.Error)
	}
	if !r.IsFinal || r.Text != "ciao mondo" {
		t.Fatalf("result = %+v", r)
	}
	if _, ok := <-results; ok {
		t.Fatal("expected closed channel")
	}

	if !strings.HasPrefix(gotAuth, "Bearer gsk-test") {
		t.Fatalf("Authorization = %q", gotAuth)
	}
	if gotModel != "whisper-large-v3-turbo" || gotLang != "it" {
		t.Fatalf("model=%q lang=%q", gotModel, gotLang)
	}
	if gotFileName != "audio.wav" {
		t.Fatalf("filename = %q", gotFileName)
	}
}

func TestNewASRRequiresAPIKey(t *testing.T) {
	os.Unsetenv("GROQ_API_KEY")
	if _, err := NewASR(Config{}); err == nil {
		t.Fatal("expected error for empty api_key")
	}
}
