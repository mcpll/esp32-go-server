package mcpaccess

import (
	"fmt"
	"net/url"
	"strings"

	"xiaozhi-esp32-server-golang/internal/domain/config/store"
	"xiaozhi-esp32-server-golang/internal/util"

	"github.com/golang-jwt/jwt/v4"
)

// Claims are what the MCP WebSocket verifier reads. AgentID stays a string:
// PocketBase ids are not numbers. UserID is unused for routing and stays the
// numeric field the verifier already unmarshals.
type Claims struct {
	UserID     uint   `json:"userId"`
	AgentID    string `json:"agentId"`
	EndpointID string `json:"endpointId"`
	Purpose    string `json:"purpose"`
	jwt.RegisteredClaims
}

// SignAgentToken returns a stable HS256 token for one agent. No issued-at or
// expiry, so the same agent always produces the same URL.
func SignAgentToken(agentID string) (string, error) {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return "", fmt.Errorf("agent id is required")
	}
	secret := util.GetManagerEndpointAuthToken()
	if secret == "" {
		return "", fmt.Errorf("endpoint auth token is not set")
	}
	claims := Claims{
		AgentID:    agentID,
		EndpointID: "agent_" + agentID,
		Purpose:    "mcp-endpoint",
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		return "", err
	}
	return signed, nil
}

// ParseAgentToken checks the signature and returns the claims.
func ParseAgentToken(tokenString string) (*Claims, error) {
	tokenString = strings.TrimSpace(tokenString)
	tokenString = strings.TrimPrefix(tokenString, "Bearer ")
	if tokenString == "" {
		return nil, fmt.Errorf("missing token")
	}
	secret := util.GetManagerEndpointAuthToken()
	if secret == "" {
		return nil, fmt.Errorf("endpoint auth token is not set")
	}
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return []byte(secret), nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}
	if strings.TrimSpace(claims.AgentID) == "" {
		return nil, fmt.Errorf("invalid token: missing agentId")
	}
	return claims, nil
}

// EndpointURL is the agent MCP access point: the external WebSocket origin from
// settings.ota, the /mcp path, and a stable token. The JWT stays on the server.
func EndpointURL(agentID string) (string, error) {
	raw := strings.TrimSpace(store.GetString("ota.external.websocket.url"))
	if raw == "" {
		return "", fmt.Errorf("external websocket URL is not set")
	}
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "ws" && parsed.Scheme != "wss") || parsed.Host == "" {
		return "", fmt.Errorf("external websocket URL is invalid")
	}
	token, err := SignAgentToken(agentID)
	if err != nil {
		return "", err
	}
	return (&url.URL{
		Scheme:   parsed.Scheme,
		Host:     parsed.Host,
		Path:     "/mcp",
		RawQuery: url.Values{"token": {token}}.Encode(),
	}).String(), nil
}
