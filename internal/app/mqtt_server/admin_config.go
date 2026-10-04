package mqtt_server

import (
	"strings"

	"github.com/spf13/viper"
)

const (
	defaultAdminUsername = "admin"
)

func configuredAdminUsername() string {
	if username := strings.TrimSpace(viper.GetString("mqtt_server.username")); username != "" {
		return username
	}
	return defaultAdminUsername
}

func configuredAdminPassword() string {
	return strings.TrimSpace(viper.GetString("mqtt_server.password"))
}
