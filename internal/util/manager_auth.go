package util

import (
	"os"
	"strings"
)

// GetManagerEndpointAuthToken returns the token used to sign and verify MCP/OpenClaw endpoint JWTs.
// The value comes from ENDPOINT_AUTH_TOKEN. A config file or settings record cannot supply it.
func GetManagerEndpointAuthToken() string {
	return strings.TrimSpace(os.Getenv("ENDPOINT_AUTH_TOKEN"))
}
