package util

import (
	"strings"

	"github.com/spf13/viper"
)

// GetManagerEndpointAuthToken returns the token used to sign/verify MCP/OpenClaw endpoint JWTs.
func GetManagerEndpointAuthToken() string {
	return strings.TrimSpace(viper.GetString("manager.endpoint_auth_token"))
}
