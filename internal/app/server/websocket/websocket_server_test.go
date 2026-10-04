package websocket

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/spf13/viper"
)

func withWSToken(t *testing.T, token string) {
	t.Helper()
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("websocket.token", token)
}

func TestChatRejectsMissingAuthorization(t *testing.T) {
	withWSToken(t, "secret-token")

	s := NewWebSocketServer(0)
	req := httptest.NewRequest(http.MethodGet, "/xiaozhi/v1/", nil)
	req.Header.Set("Device-Id", "AA:BB:CC:DD:EE:01")
	rec := httptest.NewRecorder()
	s.handleChat(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401", rec.Code)
	}
}

func TestChatRejectsWrongAuthorization(t *testing.T) {
	withWSToken(t, "secret-token")

	s := NewWebSocketServer(0)
	req := httptest.NewRequest(http.MethodGet, "/xiaozhi/v1/", nil)
	req.Header.Set("Device-Id", "AA:BB:CC:DD:EE:01")
	req.Header.Set("Authorization", "Bearer other-token")
	rec := httptest.NewRecorder()
	s.handleChat(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401", rec.Code)
	}
}

func TestChatAcceptsBearerToken(t *testing.T) {
	withWSToken(t, "secret-token")

	s := NewWebSocketServer(0)
	ts := httptest.NewServer(http.HandlerFunc(s.handleChat))
	t.Cleanup(ts.Close)

	h := http.Header{}
	h.Set("Device-Id", "AA:BB:CC:DD:EE:01")
	h.Set("Authorization", "Bearer secret-token")
	conn, resp, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http"), h)
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		t.Fatalf("dial: %v status=%d", err, status)
	}
	conn.Close()
}

func TestOtaWebsocketTokenMatchesConfiguredToken(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("auth.enable", false)
	viper.Set("websocket.token", "secret-token")
	viper.Set("ota.test.websocket.url", "ws://127.0.0.1:8989/xiaozhi/v1/")

	req := httptest.NewRequest(http.MethodGet, "/xiaozhi/ota/", nil)
	req.Header.Set("Device-Id", "AA:BB:CC:DD:EE:01")
	req.Header.Set("Client-Id", "client-1")
	req.RemoteAddr = "127.0.0.1:1234"
	rec := httptest.NewRecorder()
	NewWebSocketServer(0).handleOta(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	body, _ := io.ReadAll(rec.Body)
	var resp OtaResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode: %v body=%s", err, body)
	}
	if resp.Websocket.Token != "secret-token" {
		t.Fatalf("websocket.token %q, want secret-token", resp.Websocket.Token)
	}
}

func TestInjectRouteRemoved(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/admin/inject_msg", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	NewWebSocketServer(0).Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d, want 404", rec.Code)
	}
}
