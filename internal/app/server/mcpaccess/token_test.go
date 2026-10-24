package mcpaccess

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestEndpointTokenClaimsKeepStringAgentIDs(t *testing.T) {
	t.Setenv("ENDPOINT_AUTH_TOKEN", "endpoint-secret-not-the-default")
	const agentID = "k7m2n9p4q1r8s3t"

	first, err := SignAgentToken(agentID)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	second, err := SignAgentToken(agentID)
	if err != nil {
		t.Fatalf("sign again: %v", err)
	}
	if first != second {
		t.Fatal("the same agent must get the same token, so a copied URL keeps working")
	}

	claims := jwtPayload(t, first)
	agentClaim, ok := claims["agentId"]
	if !ok {
		t.Fatalf("claims = %#v, missing agentId", claims)
	}
	if agentClaim != agentID {
		t.Fatalf("agentId = %#v, want the PocketBase id %q", agentClaim, agentID)
	}

	parsed, err := ParseAgentToken(first)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed.AgentID != agentID {
		t.Fatalf("parsed agent id = %q", parsed.AgentID)
	}
}

func TestEndpointURLUsesExternalWebsocketBase(t *testing.T) {
	t.Setenv("ENDPOINT_AUTH_TOKEN", "endpoint-secret-not-the-default")
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("ota.external.websocket.url", "wss://eye.example/xiaozhi/v1/")

	const agentID = "k7m2n9p4q1r8s3t"
	got, err := EndpointURL(agentID)
	if err != nil {
		t.Fatalf("url: %v", err)
	}
	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	if parsed.Scheme != "wss" || parsed.Host != "eye.example" || parsed.Path != "/mcp" {
		t.Fatalf("url = %s, want wss://eye.example/mcp", got)
	}
	claims, err := ParseAgentToken(parsed.Query().Get("token"))
	if err != nil {
		t.Fatalf("token in url: %v", err)
	}
	if claims.AgentID != agentID {
		t.Fatalf("url agent id = %q", claims.AgentID)
	}
}

func jwtPayload(t *testing.T, token string) map[string]any {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("token parts = %d", len(parts))
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	var claims map[string]any
	if err := json.Unmarshal(raw, &claims); err != nil {
		t.Fatalf("payload json: %v", err)
	}
	return claims
}
