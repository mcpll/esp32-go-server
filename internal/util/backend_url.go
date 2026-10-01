package util

import (
	"os"

	"github.com/spf13/viper"
)

// GetBackendURL returns backend URL from env first, else from config
func GetBackendURL() string {
	// Prefer environment variable
	if backendURL := os.Getenv("BACKEND_URL"); backendURL != "" {
		return backendURL
	}
	// Fall back to config file
	return viper.GetString("manager.backend_url")
}
