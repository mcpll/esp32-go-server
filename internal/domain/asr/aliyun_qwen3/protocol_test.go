package aliyun_qwen3

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestServerEventUnmarshalConversationItemCreatedStringID(t *testing.T) {
	raw := []byte(`{"event_id":"event_1","type":"conversation.item.created","item":{"id":"item_123","object":"realtime.item","type":"message","status":"in_progress","role":"assistant","content":[{"type":"input_audio"}]}}`)

	var event ServerEvent
	if err := json.Unmarshal(raw, &event); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}

	if event.Item == nil {
		t.Fatal("expected item to be parsed")
	}
	if event.Item.ID != "item_123" {
		t.Fatalf("expected string item id, got %q", event.Item.ID)
	}
}

func TestGetTranscriptionTextPrefersTranscript(t *testing.T) {
	event := &ServerEvent{
		Transcript: "are you looking at the flowers?",
		Stash:      "are you looking at the flowers",
	}

	if got := GetTranscriptionText(event); got != "are you looking at the flowers?" {
		t.Fatalf("expected transcript text, got %q", got)
	}
}

func TestGetTranscriptionTextFallsBackToStash(t *testing.T) {
	event := &ServerEvent{
		Text:  "",
		Stash: "are you looking at the flowers",
	}

	if got := GetTranscriptionText(event); got != "are you looking at the flowers" {
		t.Fatalf("expected stash fallback, got %q", got)
	}
}

func TestStreamingRecognizeSendsSessionUpdateWithLanguageBeforeAudio(t *testing.T) {
	firstMessage := make(chan []byte, 1)
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade failed: %v", err)
			return
		}
		defer conn.Close()

		_, message, err := conn.ReadMessage()
		if err != nil {
			t.Errorf("read first message failed: %v", err)
			return
		}
		firstMessage <- message
		_ = conn.WriteJSON(ServerEvent{Type: "session.updated"})
	}))
	defer server.Close()

	asr, err := NewAliyunQwen3ASR(Config{
		APIKey:     "test-key",
		WsURL:      "ws" + strings.TrimPrefix(server.URL, "http"),
		Model:      "qwen3-asr-flash-realtime",
		Format:     "pcm",
		SampleRate: 16000,
		Language:   "zh",
		Timeout:    time.Second,
	})
	if err != nil {
		t.Fatalf("create asr failed: %v", err)
	}
	defer asr.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	audioStream := make(chan []float32, 1)
	audioStream <- []float32{0.1, 0.2}

	resultChan, err := asr.StreamingRecognize(ctx, audioStream)
	if err != nil {
		t.Fatalf("streaming recognize failed: %v", err)
	}
	defer func() {
		cancel()
		for range resultChan {
		}
	}()

	select {
	case message := <-firstMessage:
		var event ClientEvent
		if err := json.Unmarshal(message, &event); err != nil {
			t.Fatalf("unmarshal first message failed: %v, raw=%s", err, string(message))
		}
		if event.Type != "session.update" {
			t.Fatalf("first websocket message = %q, want session.update; raw=%s", event.Type, string(message))
		}
		if event.Session == nil || event.Session.InputAudioTranscription == nil {
			t.Fatalf("session.update missing input_audio_transcription: raw=%s", string(message))
		}
		if got := event.Session.InputAudioTranscription.Language; got != "zh" {
			t.Fatalf("session.update language = %q, want zh; raw=%s", got, string(message))
		}
	case <-ctx.Done():
		t.Fatalf("timed out waiting for first websocket message: %v", ctx.Err())
	}
}

// A finished Qwen session stays open and repeats the first transcript.
// A second utterance must open a new websocket, or the same question is sent again.
func TestSecondUtteranceDoesNotReplayTheFirstTranscript(t *testing.T) {
	var dials atomic.Int32
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := dials.Add(1)
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		label := "prima"
		if n > 1 {
			label = "seconda"
		}
		var finished bool
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var event ClientEvent
			if err := json.Unmarshal(message, &event); err != nil {
				continue
			}
			switch event.Type {
			case "session.update":
				_ = conn.WriteJSON(ServerEvent{Type: "session.updated"})
			case "input_audio_buffer.commit":
				text := label
				if finished {
					text = "prima"
				}
				_ = conn.WriteJSON(ServerEvent{Type: "input_audio_buffer.committed"})
				_ = conn.WriteJSON(ServerEvent{
					Type:       "conversation.item.input_audio_transcription.completed",
					Transcript: text,
				})
			case "session.finish":
				_ = conn.WriteJSON(ServerEvent{Type: "session.finished"})
				finished = true
			}
		}
	}))
	defer server.Close()

	asr, err := NewAliyunQwen3ASR(Config{
		APIKey:     "test-key",
		WsURL:      "ws" + strings.TrimPrefix(server.URL, "http"),
		Model:      "qwen3-asr-flash-realtime",
		Format:     "pcm",
		SampleRate: 16000,
		Language:   "it",
		Timeout:    2 * time.Second,
	})
	if err != nil {
		t.Fatalf("create asr failed: %v", err)
	}
	defer asr.Close()

	if got := recognizeOnce(t, asr, 0.1); got != "prima" {
		t.Fatalf("first transcript = %q, want prima", got)
	}
	if got := recognizeOnce(t, asr, 0.9); got != "seconda" {
		t.Fatalf("second transcript = %q, want seconda", got)
	}
}

func recognizeOnce(t *testing.T, asr *AliyunQwen3ASR, sample float32) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	audio := make(chan []float32, 1)
	audio <- []float32{sample, sample}
	close(audio)

	results, err := asr.StreamingRecognize(ctx, audio)
	if err != nil {
		t.Fatalf("streaming recognize failed: %v", err)
	}
	var text string
	for result := range results {
		if result.Error != nil {
			t.Fatalf("asr error: %v", result.Error)
		}
		if result.IsFinal {
			text = result.Text
		}
	}
	return text
}
