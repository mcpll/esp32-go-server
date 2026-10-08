package websocket

import (
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"xiaozhi-esp32-server-golang/internal/domain/config/store"

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

func TestOtaUrlChangesWithoutRestart(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("auth.enable", false)
	viper.Set("websocket.token", "secret-token")

	ask := func() string {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/xiaozhi/ota/", nil)
		req.Header.Set("Device-Id", "AA:BB:CC:DD:EE:01")
		req.Header.Set("Client-Id", "client-1")
		req.RemoteAddr = "127.0.0.1:1234"
		rec := httptest.NewRecorder()
		NewWebSocketServer(0).handleOta(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d, want 200: %s", rec.Code, rec.Body.String())
		}
		var resp OtaResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return resp.Websocket.Url
	}

	if err := store.Merge(map[string]any{
		"ota": map[string]any{"test": map[string]any{"websocket": map[string]any{"url": "ws://old/xiaozhi/v1/"}}},
	}); err != nil {
		t.Fatal(err)
	}
	if got := ask(); got != "ws://old/xiaozhi/v1/" {
		t.Fatalf("url = %q", got)
	}
	if err := store.Merge(map[string]any{
		"ota": map[string]any{"test": map[string]any{"websocket": map[string]any{"url": "ws://new/xiaozhi/v1/"}}},
	}); err != nil {
		t.Fatal(err)
	}
	if got := ask(); got != "ws://new/xiaozhi/v1/" {
		t.Fatalf("url = %q", got)
	}
	if got := store.GetString("websocket.token"); got != "secret-token" {
		t.Fatalf("token overridden: %q", got)
	}
}

func TestOtaUsesRequestHostWhenUrlEmpty(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("auth.enable", false)
	viper.Set("websocket.token", "secret-token")

	ask := func(host string, tlsOn bool) string {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "http://"+host+"/xiaozhi/ota/", nil)
		req.Host = host
		if tlsOn {
			req.TLS = &tls.ConnectionState{}
		}
		req.Header.Set("Device-Id", "AA:BB:CC:DD:EE:01")
		req.Header.Set("Client-Id", "client-1")
		req.RemoteAddr = "192.168.1.20:1234"
		rec := httptest.NewRecorder()
		NewWebSocketServer(0).handleOta(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d, want 200: %s", rec.Code, rec.Body.String())
		}
		var resp OtaResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return resp.Websocket.Url
	}

	if got := ask("192.168.1.20:8989", false); got != "ws://192.168.1.20:8989/xiaozhi/v1/" {
		t.Fatalf("url = %q", got)
	}
	if got := ask("lan.example:8989", true); got != "wss://lan.example:8989/xiaozhi/v1/" {
		t.Fatalf("tls url = %q", got)
	}

	viper.Set("ota.test.websocket.url", "ws://configured/xiaozhi/v1/")
	if got := ask("192.168.1.20:8989", false); got != "ws://configured/xiaozhi/v1/" {
		t.Fatalf("explicit url = %q", got)
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
