package mcpaccess

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"xiaozhi-esp32-server-golang/internal/domain/mcp"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

func TestDeviceToolsCommandListsTheSchema(t *testing.T) {
	const deviceID = "AA:BB:CC:DD:EE:01"
	conn := newEyeConn()
	mcp.RegisterCurrentDeviceTransportResolver(func(id string) string {
		if id == deviceID {
			return "websocket"
		}
		return ""
	})
	t.Cleanup(func() {
		mcp.RegisterCurrentDeviceTransportResolver(nil)
		mcp.CloseDeviceIotOverMcp(deviceID, conn)
		_ = mcp.RemoveDeviceMcpClient(deviceID)
	})
	if err := mcp.EnsureDeviceIotOverMcp(deviceID, conn); err != nil {
		t.Fatalf("device mcp: %v", err)
	}

	result, err := Tools(context.Background(), map[string]any{"device": deviceID})
	if err != nil {
		t.Fatalf("mcp_tools: %v", err)
	}
	tools := toolList(t, result)
	if len(tools) != 1 {
		t.Fatalf("tools = %#v", tools)
	}
	if tools[0]["name"] != "set_volume" {
		t.Fatalf("name = %#v", tools[0]["name"])
	}
	if tools[0]["description"] != "Set the speaker volume" {
		t.Fatalf("description = %#v", tools[0]["description"])
	}
	schema, _ := tools[0]["input_schema"].(map[string]any)
	properties, _ := schema["properties"].(map[string]any)
	volume, _ := properties["volume"].(map[string]any)
	if volume["type"] != "number" {
		t.Fatalf("volume schema = %#v", schema)
	}
}

func TestDeviceCallCommandSendsArguments(t *testing.T) {
	const deviceID = "AA:BB:CC:DD:EE:02"
	conn := newEyeConn()
	mcp.RegisterCurrentDeviceTransportResolver(func(id string) string {
		if id == deviceID {
			return "websocket"
		}
		return ""
	})
	t.Cleanup(func() {
		mcp.RegisterCurrentDeviceTransportResolver(nil)
		mcp.CloseDeviceIotOverMcp(deviceID, conn)
		_ = mcp.RemoveDeviceMcpClient(deviceID)
	})
	if err := mcp.EnsureDeviceIotOverMcp(deviceID, conn); err != nil {
		t.Fatalf("device mcp: %v", err)
	}

	result, err := Call(context.Background(), map[string]any{
		"device":    deviceID,
		"tool":      "set_volume",
		"arguments": map[string]any{"volume": 40},
	})
	if err != nil {
		t.Fatalf("mcp_call: %v", err)
	}
	body, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("result = %#v", result)
	}
	text, _ := body["result"].(string)
	if !strings.Contains(text, "volume set to 40") {
		t.Fatalf("result = %q", text)
	}
}

type eyeConn struct {
	recv chan []byte
}

func newEyeConn() *eyeConn {
	return &eyeConn{recv: make(chan []byte, 8)}
}

func (c *eyeConn) GetMcpTransportType() string { return "websocket" }

func (c *eyeConn) HandleMcpMessage(payload []byte) error {
	c.recv <- append([]byte(nil), payload...)
	return nil
}

func (c *eyeConn) RecvMcpMsg(ctx context.Context, timeout int) ([]byte, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case payload := <-c.recv:
		return payload, nil
	case <-time.After(time.Duration(timeout) * time.Millisecond):
		return nil, fmt.Errorf("timeout")
	}
}

func (c *eyeConn) SendMcpMsg(payload []byte) error {
	var request struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params map[string]any  `json:"params"`
	}
	if err := json.Unmarshal(payload, &request); err != nil || len(request.ID) == 0 {
		return nil
	}
	switch request.Method {
	case string(mcpgo.MethodInitialize):
		c.reply(request.ID, mcpgo.InitializeResult{
			ProtocolVersion: mcpgo.LATEST_PROTOCOL_VERSION,
			Capabilities:    mcpgo.ServerCapabilities{},
			ServerInfo:      mcpgo.Implementation{Name: "eye", Version: "1.0.0"},
		})
	case string(mcpgo.MethodToolsList):
		c.reply(request.ID, mcpgo.ListToolsResult{
			Tools: []mcpgo.Tool{
				mcpgo.NewTool("set_volume",
					mcpgo.WithDescription("Set the speaker volume"),
					mcpgo.WithNumber("volume"),
				),
			},
		})
	case string(mcpgo.MethodToolsCall):
		arguments, _ := request.Params["arguments"].(map[string]any)
		c.reply(request.ID, *mcpgo.NewToolResultText(fmt.Sprintf("volume set to %v", arguments["volume"])))
	}
	return nil
}

func (c *eyeConn) reply(id json.RawMessage, result any) {
	raw, err := json.Marshal(result)
	if err != nil {
		return
	}
	payload, err := json.Marshal(struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Result  json.RawMessage `json:"result"`
	}{JSONRPC: "2.0", ID: id, Result: raw})
	if err != nil {
		return
	}
	c.recv <- payload
}

func toolList(t *testing.T, result any) []map[string]any {
	t.Helper()
	body, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("result = %#v", result)
	}
	raw, ok := body["tools"].([]map[string]any)
	if !ok {
		t.Fatalf("tools = %#v", body["tools"])
	}
	return raw
}
