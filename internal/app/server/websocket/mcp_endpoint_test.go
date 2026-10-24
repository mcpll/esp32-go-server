package websocket

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"xiaozhi-esp32-server-golang/internal/app/server/mcpaccess"
	"xiaozhi-esp32-server-golang/internal/domain/mcp"

	"github.com/gorilla/websocket"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/spf13/viper"
)

func TestExternalClientConnectsWithCopiedEndpointURL(t *testing.T) {
	t.Setenv("ENDPOINT_AUTH_TOKEN", "endpoint-secret-not-the-default")
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("ota.external.websocket.url", "wss://eye.example/xiaozhi/v1/")

	const agentID = "k7m2n9p4q1r8s3t"
	copied, err := mcpaccess.EndpointURL(agentID)
	if err != nil {
		t.Fatalf("endpoint url: %v", err)
	}
	copiedURL, err := url.Parse(copied)
	if err != nil {
		t.Fatalf("parse copied url: %v", err)
	}
	token := copiedURL.Query().Get("token")
	if token == "" {
		t.Fatalf("copied url has no token: %s", copied)
	}

	s := NewWebSocketServer(0)
	ts := httptest.NewServer(http.HandlerFunc(s.handleMCPWebSocket))
	t.Cleanup(func() {
		ts.Close()
		_ = mcp.RemoveDeviceMcpClient(agentID)
	})

	conn := dialMCP(t, ts.URL, token)
	defer conn.Close()
	serveExternalMCP(t, conn)

	deadline := time.Now().Add(2 * time.Second)
	for {
		connected, _ := mcp.GetWsEndpointConnectionStatus(agentID)
		if connected {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("external client was not connected for the string agent id")
		}
		time.Sleep(20 * time.Millisecond)
	}

	result, err := mcpaccess.Endpoint(context.Background(), map[string]any{"agent": agentID})
	if err != nil {
		t.Fatalf("mcp_endpoint: %v", err)
	}
	body, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("result = %#v", result)
	}
	if body["url"] != copied {
		t.Fatalf("url = %v, want the copied URL", body["url"])
	}
	if body["connected"] != true || body["tools_count"] != 1 {
		t.Fatalf("connected = %#v tools_count = %#v, want true and 1", body["connected"], body["tools_count"])
	}

	bad, resp, err := websocket.DefaultDialer.Dial(localMCPURL(ts.URL, "not-a-token"), nil)
	if err == nil {
		bad.Close()
		t.Fatal("a bad token was accepted")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		t.Fatalf("bad token status = %d, want 401", status)
	}
}

func dialMCP(t *testing.T, serverURL, token string) *websocket.Conn {
	t.Helper()
	conn, resp, err := websocket.DefaultDialer.Dial(localMCPURL(serverURL, token), nil)
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		t.Fatalf("dial copied token: %v status=%d", err, status)
	}
	return conn
}

func localMCPURL(serverURL, token string) string {
	return "ws" + strings.TrimPrefix(serverURL, "http") + "/mcp?token=" + url.QueryEscape(token)
}

func serveExternalMCP(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	go func() {
		for {
			_, payload, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var request struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
			}
			if err := json.Unmarshal(payload, &request); err != nil || len(request.ID) == 0 {
				continue
			}
			var result any
			switch request.Method {
			case string(mcpgo.MethodInitialize):
				result = mcpgo.InitializeResult{
					ProtocolVersion: mcpgo.LATEST_PROTOCOL_VERSION,
					Capabilities:    mcpgo.ServerCapabilities{},
					ServerInfo:      mcpgo.Implementation{Name: "external", Version: "1.0.0"},
				}
			case string(mcpgo.MethodToolsList):
				result = mcpgo.ListToolsResult{
					Tools: []mcpgo.Tool{
						mcpgo.NewTool("set_volume", mcpgo.WithDescription("Set the speaker volume"), mcpgo.WithNumber("volume")),
					},
				}
			default:
				continue
			}
			raw, err := json.Marshal(result)
			if err != nil {
				t.Errorf("marshal %s: %v", request.Method, err)
				return
			}
			response, err := json.Marshal(struct {
				JSONRPC string          `json:"jsonrpc"`
				ID      json.RawMessage `json:"id"`
				Result  json.RawMessage `json:"result"`
			}{JSONRPC: "2.0", ID: request.ID, Result: raw})
			if err != nil {
				t.Errorf("marshal response: %v", err)
				return
			}
			if err := conn.WriteMessage(websocket.TextMessage, response); err != nil {
				return
			}
		}
	}()
}
