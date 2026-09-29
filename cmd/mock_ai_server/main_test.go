package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleTTSSpeechReturnsOpus(t *testing.T) {
	cfg := &serverConfig{
		ttsMode:       "beep",
		ttsDurationMs: 400,
		ttsSampleRate: 16000,
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/audio/speech", strings.NewReader(`{
		"model":"tts-1",
		"input":"hello",
		"voice":"alloy",
		"response_format":"opus"
	}`))
	rec := httptest.NewRecorder()

	cfg.handleTTSSpeech(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("期望状态码 200，实际为 %d, body=%s", rec.Code, rec.Body.String())
	}

	if got := rec.Header().Get("Content-Type"); got != "audio/ogg" {
		t.Fatalf("期望 Content-Type=audio/ogg，实际为 %s", got)
	}

	body := rec.Body.Bytes()
	if len(body) == 0 {
		t.Fatal("未返回任何音频数据")
	}
	if !bytes.HasPrefix(body, []byte("OggS")) {
		t.Fatalf("期望返回 Ogg Opus 数据，前 4 字节实际为 %q", body[:minInt(len(body), 4)])
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
