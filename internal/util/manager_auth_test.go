package util

import (
	"testing"

	"github.com/spf13/viper"
)

func TestEndpointAuthTokenIsReadFromEnvOnly(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("manager.endpoint_auth_token", "from-yaml")

	t.Setenv("ENDPOINT_AUTH_TOKEN", "from-env")
	if got := GetManagerEndpointAuthToken(); got != "from-env" {
		t.Fatalf("token = %q, want the environment value", got)
	}

	t.Setenv("ENDPOINT_AUTH_TOKEN", "")
	if got := GetManagerEndpointAuthToken(); got != "" {
		t.Fatalf("token = %q, want empty when ENDPOINT_AUTH_TOKEN is unset", got)
	}
}
