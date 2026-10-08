package chat

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"xiaozhi-esp32-server-golang/internal/data/client"
	"xiaozhi-esp32-server-golang/internal/domain/config/types"
	"xiaozhi-esp32-server-golang/internal/domain/openclaw"
)

func TestItalianEnterPhraseRoutesToOpenClawAndExitPhraseLeaves(t *testing.T) {
	agentID := "agent-italian"
	deviceID := "device-italian"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	frames, cleanup := connectOpenClawAgent(t, agentID)
	defer cleanup()

	session := newOpenClawRouteSession(ctx, agentID, deviceID)
	manager := openclaw.GetManager()
	t.Cleanup(func() {
		manager.ExitMode(agentID, deviceID)
		manager.UnregisterAgentConnection(agentID, manager.GetAgentSession(agentID))
	})

	if err := session.actionDoChat(ctx, "apri openclaw", nil); err != nil {
		t.Fatalf("enter phrase: %v", err)
	}
	if !manager.IsModeEnabled(agentID, deviceID) {
		t.Fatal("enter phrase did not open OpenClaw")
	}
	if msg := takeOpenClawFrame(frames, 50*time.Millisecond); msg != nil {
		t.Fatalf("enter phrase was forwarded: %+v", msg)
	}

	if err := session.actionDoChat(ctx, "che tempo fa", nil); err != nil {
		t.Fatalf("routed phrase: %v", err)
	}
	if !manager.IsModeEnabled(agentID, deviceID) {
		t.Fatal("OpenClaw closed while routing a phrase")
	}
	msg := takeOpenClawFrame(frames, 2*time.Second)
	if msg == nil || msg.Type != "message" || !strings.Contains(string(msg.Payload), "che tempo fa") {
		t.Fatalf("phrase was not routed to OpenClaw: %+v", msg)
	}

	if err := session.actionDoChat(ctx, "chiudi openclaw", nil); err != nil {
		t.Fatalf("exit phrase: %v", err)
	}
	if manager.IsModeEnabled(agentID, deviceID) {
		t.Fatal("exit phrase left OpenClaw open")
	}
	if extra := takeOpenClawFrame(frames, 50*time.Millisecond); extra != nil {
		t.Fatalf("exit phrase was forwarded: %+v", extra)
	}
}

func newOpenClawRouteSession(ctx context.Context, agentID, deviceID string) *ChatSession {
	clientState := &client.ClientState{
		Ctx:       ctx,
		AgentID:   agentID,
		DeviceID:  deviceID,
		SessionID: "session-italian",
		DeviceConfig: types.UConfig{
			OpenClaw: types.OpenClawConfig{
				Allowed:       true,
				EnterKeywords: []string{"apri openclaw", "entra in openclaw"},
				ExitKeywords:  []string{"chiudi openclaw", "esci da openclaw"},
			},
		},
	}
	ttsManager := NewTTSManager(clientState, nil, nil)
	return &ChatSession{
		clientState: clientState,
		ttsManager:  ttsManager,
		llmManager:  NewLLMManager(clientState, nil, ttsManager, nil, nil),
	}
}

func connectOpenClawAgent(t *testing.T, agentID string) (<-chan openclaw.WSMessage, func()) {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	accepted := make(chan *websocket.Conn, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		accepted <- conn
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	clientConn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		srv.Close()
		t.Fatalf("dial openclaw: %v", err)
	}
	var serverConn *websocket.Conn
	select {
	case serverConn = <-accepted:
	case <-time.After(2 * time.Second):
		clientConn.Close()
		srv.Close()
		t.Fatal("openclaw agent socket was not accepted")
	}
	openclaw.GetManager().RegisterAgentConnection(agentID, serverConn)

	frames := make(chan openclaw.WSMessage, 8)
	go func() {
		defer close(frames)
		for {
			_, data, err := clientConn.ReadMessage()
			if err != nil {
				return
			}
			var msg openclaw.WSMessage
			if json.Unmarshal(data, &msg) != nil {
				continue
			}
			frames <- msg
		}
	}()
	return frames, func() {
		clientConn.Close()
		serverConn.Close()
		srv.Close()
	}
}

func takeOpenClawFrame(frames <-chan openclaw.WSMessage, wait time.Duration) *openclaw.WSMessage {
	select {
	case msg, ok := <-frames:
		if !ok {
			return nil
		}
		return &msg
	case <-time.After(wait):
		return nil
	}
}
