package llm

import (
	"context"

	log "xiaozhi-esp32-server-golang/logger"

	"github.com/cloudwego/eino/schema"
)

// ConvertMCPToolsToEinoTools converts MCP tools to Eino ToolInfo format
func ConvertMCPToolsToEinoTools(ctx context.Context, mcpTools map[string]interface{}) ([]*schema.ToolInfo, error) {
	var einoTools []*schema.ToolInfo

	for toolName, mcpTool := range mcpTools {
		// Try to get tool info
		if invokableTool, ok := mcpTool.(interface {
			Info(context.Context) (*schema.ToolInfo, error)
		}); ok {
			toolInfo, err := invokableTool.Info(ctx)
			if err != nil {
				log.Errorf("failed to get info for tool %s: %v", toolName, err)
				continue
			}
			einoTools = append(einoTools, toolInfo)
		} else {
			log.Warnf("tool %s does not support Info interface, skipping conversion", toolName)
		}
	}

	log.Infof("converted %d MCP tools to Eino tools", len(einoTools))
	return einoTools, nil
}
