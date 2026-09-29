package mcp

import (
	"fmt"
	"net/url"
	"strings"

	log "xiaozhi-esp32-server-golang/logger"

	"github.com/spf13/viper"
)

// CheckMCPConfig checks MCP config and reports potential issues
func CheckMCPConfig() {
	log.Info("=== MCP config check ===")

	// Check global enable flag
	globalEnabled := viper.GetBool("mcp.global.enabled")
	log.Infof("global MCP enabled: %v", globalEnabled)

	if !globalEnabled {
		log.Info("global MCP disabled, config check done")
		return
	}

	// Check reconnect config
	reconnectInterval := viper.GetInt("mcp.global.reconnect_interval")
	maxAttempts := viper.GetInt("mcp.global.max_reconnect_attempts")
	log.Infof("reconnect config: interval=%ds, max_attempts=%d", reconnectInterval, maxAttempts)

	// Check server configs
	var serverConfigs []MCPServerConfig
	if err := viper.UnmarshalKey("mcp.global.servers", &serverConfigs); err != nil {
		log.Errorf("❌ failed to parse MCP server config: %v", err)
		return
	}

	if len(serverConfigs) == 0 {
		log.Warn("⚠️  no MCP servers configured")
		return
	}

	log.Infof("configured %d MCP servers:", len(serverConfigs))

	enabledCount := 0
	problemCount := 0

	for i, config := range serverConfigs {
		status := "✅"
		issues := []string{}

		// Check name
		if config.Name == "" {
			status = "❌"
			issues = append(issues, "name is empty")
			problemCount++
		}

		transportType, endpoint, err := endpointForConfig(config)
		if err != nil {
			status = "❌"
			issues = append(issues, err.Error())
			problemCount++
		} else {
			if _, parseErr := url.ParseRequestURI(endpoint); parseErr != nil {
				status = "❌"
				issues = append(issues, "invalid URL format")
				problemCount++
			}
			if transportType == "sse" && !strings.HasPrefix(endpoint, "http://") && !strings.HasPrefix(endpoint, "https://") {
				status = "⚠️"
				issues = append(issues, "SSE URL format may be incorrect")
			}
		}

		// Check enable flag
		if config.Enabled {
			enabledCount++
		}

		// Print check results
		issueStr := ""
		if len(issues) > 0 {
			issueStr = fmt.Sprintf(" - issues: %s", strings.Join(issues, ", "))
		}

		log.Infof("  [%d] %s %s (URL: %s, enabled: %v)%s",
			i+1, status, config.Name, endpointForLog(config), config.Enabled, issueStr)
	}

	// Summary
	log.Infof("config check done: %d servers enabled, %d with issues", enabledCount, problemCount)

	if problemCount > 0 {
		log.Warn("⚠️  config issues found, please review and fix the errors above")
	}

	log.Info("=== MCP config check complete ===")
}

func endpointForLog(config MCPServerConfig) string {
	_, endpoint, err := endpointForConfig(config)
	if err != nil {
		if strings.TrimSpace(config.Url) != "" {
			return config.Url
		}
		return config.SSEUrl
	}
	return endpoint
}
