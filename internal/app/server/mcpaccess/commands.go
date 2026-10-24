package mcpaccess

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"xiaozhi-esp32-server-golang/internal/domain/mcp"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// Endpoint builds the agent MCP access point. The token stays on the server.
// tools_count is how many tools the connected external client has reported.
func Endpoint(ctx context.Context, payload map[string]any) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	agentID, _ := payload["agent"].(string)
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return nil, fmt.Errorf("agent is required")
	}
	rawURL, err := EndpointURL(agentID)
	if err != nil {
		return nil, err
	}
	connected, _ := mcp.GetWsEndpointConnectionStatus(agentID)
	toolsCount := 0
	if connected {
		tools, err := mcp.RefreshReportedToolsByAgentID(agentID)
		if err != nil {
			return nil, err
		}
		toolsCount = len(tools)
	}
	return map[string]any{
		"url":         rawURL,
		"connected":   connected,
		"tools_count": toolsCount,
	}, nil
}

// Tools lists the tools a device has reported, including each input schema.
func Tools(ctx context.Context, payload map[string]any) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	deviceID, _ := payload["device"].(string)
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return nil, fmt.Errorf("device is required")
	}
	if mcp.GetDeviceMcpClient(deviceID) == nil {
		return nil, fmt.Errorf("device %s is offline", deviceID)
	}
	reported, err := mcp.RefreshReportedToolsByDeviceID(deviceID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"tools": toolSchemas(reported)}, nil
}

// Call runs one device tool with the arguments from the console.
func Call(ctx context.Context, payload map[string]any) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	deviceID, _ := payload["device"].(string)
	deviceID = strings.TrimSpace(deviceID)
	toolName, _ := payload["tool"].(string)
	toolName = strings.TrimSpace(toolName)
	if deviceID == "" || toolName == "" {
		return nil, fmt.Errorf("device and tool are required")
	}
	arguments, err := callArguments(payload["arguments"])
	if err != nil {
		return nil, err
	}
	if mcp.GetDeviceMcpClient(deviceID) == nil {
		return nil, fmt.Errorf("device %s is offline", deviceID)
	}
	if invokable, ok := mcp.GetReportedToolByDeviceIDAndName(deviceID, toolName); ok {
		raw, err := json.Marshal(arguments)
		if err != nil {
			return nil, err
		}
		result, err := invokable.InvokableRun(ctx, string(raw))
		if err != nil {
			return nil, err
		}
		return map[string]any{"result": result}, nil
	}
	result, called, err := mcp.RawCallReportedToolByDeviceID(deviceID, toolName, arguments)
	if err != nil {
		return nil, err
	}
	if !called {
		return nil, fmt.Errorf("tool not found: %s", toolName)
	}
	return map[string]any{"result": result}, nil
}

func callArguments(value any) (map[string]any, error) {
	if value == nil {
		return map[string]any{}, nil
	}
	arguments, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("arguments must be an object")
	}
	return arguments, nil
}

func toolSchemas(reported map[string]tool.InvokableTool) []map[string]any {
	names := make([]string, 0, len(reported))
	for name := range reported {
		names = append(names, name)
	}
	sort.Strings(names)
	list := make([]map[string]any, 0, len(names))
	for _, name := range names {
		item := map[string]any{
			"name":        name,
			"description": name,
		}
		info, err := reported[name].Info(context.Background())
		if err == nil && info != nil {
			if info.Desc != "" {
				item["description"] = info.Desc
			}
			if input := schemaMap(info.ParamsOneOf); input != nil {
				item["input_schema"] = input
			}
		}
		list = append(list, item)
	}
	return list
}

func schemaMap(params *schema.ParamsOneOf) map[string]any {
	if params == nil {
		return nil
	}
	openAPI, err := params.ToOpenAPIV3()
	if err != nil || openAPI == nil {
		return nil
	}
	raw, err := json.Marshal(openAPI)
	if err != nil {
		return nil
	}
	decoded := map[string]any{}
	if err := json.Unmarshal(raw, &decoded); err != nil || len(decoded) == 0 {
		return nil
	}
	return decoded
}
