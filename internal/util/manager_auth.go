package util

import (
	"strings"

	"github.com/spf13/viper"
)

const DefaultManagerAuthToken = "xiaozhi_admin_secret_key"
const DefaultManagerEndpointAuthToken = "xiaozhi_mcp_openclaw_secret_key"

// GetManagerAuthToken returns the shared internal auth token between the main process and the console.
// priority:
// 1. manager.auth_token
// 2. default value (kept in sync on both ends)
func GetManagerAuthToken() string {
	if token := strings.TrimSpace(viper.GetString("manager.auth_token")); token != "" {
		return token
	}
	return DefaultManagerAuthToken
}

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
