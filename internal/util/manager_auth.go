package util

import (
	"strings"

	"github.com/spf13/viper"
)

const DefaultManagerEndpointAuthToken = "xiaozhi_mcp_openclaw_secret_key"

// GetManagerEndpointAuthToken returns the token used to sign/verify MCP/OpenClaw endpoint JWTs.
// priority:
// 1. manager.endpoint_auth_token
// 2. default value (must match the console)
func GetManagerEndpointAuthToken() string {
	if token := strings.TrimSpace(viper.GetString("manager.endpoint_auth_token")); token != "" {
		return token
	}
	return DefaultManagerEndpointAuthToken
}
