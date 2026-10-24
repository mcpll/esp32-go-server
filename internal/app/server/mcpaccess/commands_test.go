package mcpaccess

import (
	"context"
	"net/url"
	"testing"

	"github.com/spf13/viper"
)

func TestEndpointCommandReportsURLWhenNobodyIsConnected(t *testing.T) {
	t.Setenv("ENDPOINT_AUTH_TOKEN", "endpoint-secret-not-the-default")
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("ota.external.websocket.url", "wss://eye.example/xiaozhi/v1/")

	const agentID = "k7m2n9p4q1r8s3t"
	result, err := Endpoint(context.Background(), map[string]any{"agent": agentID})
	if err != nil {
		t.Fatalf("endpoint: %v", err)
	}
	body, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("result = %#v", result)
	}
	if body["connected"] != false || body["tools_count"] != 0 {
		t.Fatalf("status = connected %#v tools_count %#v", body["connected"], body["tools_count"])
	}
	rawURL, _ := body["url"].(string)
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("url: %v", err)
	}
	if parsed.Scheme != "wss" || parsed.Host != "eye.example" || parsed.Path != "/mcp" {
		t.Fatalf("url = %s", rawURL)
	}
	claims, err := ParseAgentToken(parsed.Query().Get("token"))
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	if claims.AgentID != agentID {
		t.Fatalf("agent id = %q", claims.AgentID)
	}
}
