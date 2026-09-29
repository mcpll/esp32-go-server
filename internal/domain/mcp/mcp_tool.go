package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	log "xiaozhi-esp32-server-golang/logger"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

var callRemoteMCPTool = func(ctx context.Context, cli *client.Client, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return cli.CallTool(ctx, request)
}

var reconnectGlobalMCPServer = func(serverName string) (*client.Client, error) {
	return GetGlobalMCPManager().reconnectServer(serverName)
}

// LocalToolHandler is the local tool handler function type
type LocalToolHandler func(ctx context.Context, argumentsInJSON string) (string, error)

// mcpTool is an MCP tool implementation supporting remote and local tools
type McpTool struct {
	info       *schema.ToolInfo
	originName string
	serverName string
	client     *client.Client

	// Local tool support
	isLocal      bool
	localHandler LocalToolHandler
}

// Info returns tool info; implements BaseTool
func (t *McpTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return t.info, nil
}

func (t *McpTool) callName() string {
	if t.originName != "" {
		return t.originName
	}
	if t.info != nil {
		return t.info.Name
	}
	return ""
}

func mcpToolMatchesName(invokable tool.InvokableTool, name string) bool {
	mcpTool, ok := invokable.(*McpTool)
	if !ok || mcpTool == nil {
		return false
	}
	if mcpTool.info != nil && mcpTool.info.Name == name {
		return true
	}
	return mcpTool.originName != "" && mcpTool.originName == name
}

func findInvokableToolByName(tools map[string]tool.InvokableTool, name string) (tool.InvokableTool, bool) {
	if invokable, ok := tools[name]; ok {
		return invokable, true
	}
	for _, invokable := range tools {
		if mcpToolMatchesName(invokable, name) {
			return invokable, true
		}
	}
	return nil, false
}

func remoteCallNameForTool(invokable tool.InvokableTool, fallback string) string {
	if mcpTool, ok := invokable.(*McpTool); ok && mcpTool != nil {
		if name := mcpTool.callName(); name != "" {
			return name
		}
	}
	return fallback
}

func (t *McpTool) InvokeableLocalRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	toolInfo := t.info
	if t.localHandler == nil {
		return "", fmt.Errorf("本地工具 %s 的处理函数未定义", toolInfo.Name)
	}

	log.Infof("running local tool: %s, args: %s", toolInfo.Name, argumentsInJSON)

	resultStr, err := t.localHandler(ctx, argumentsInJSON)
	if err != nil {
		log.Errorf("local tool %s failed: %v", toolInfo.Name, err)
		return "", fmt.Errorf("本地工具执行失败: %v", err)
	}
	if len(resultStr) > 2048 {
		log.Infof("local tool %s succeeded, result length: %d", toolInfo.Name, len(resultStr))
	} else {
		log.Infof("local tool %s succeeded, result: %+s", toolInfo.Name, resultStr)
	}

	return resultStr, nil
}

// InvokableRun invokes the tool; implements InvokableTool
func (t *McpTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	// For local tools, call the local handler directly
	if t.isLocal {
		return t.InvokeableLocalRun(ctx, argumentsInJSON, opts...)
	}

	retContent := ""

	// Remote MCP tool invocation
	// Check whether the client is available
	if t.client == nil {
		return retContent, fmt.Errorf("调用MCP工具失败: MCP客户端未初始化")
	}

	// Parse arguments
	var arguments map[string]interface{}
	if argumentsInJSON != "" {
		if err := json.Unmarshal([]byte(argumentsInJSON), &arguments); err != nil {
			return retContent, fmt.Errorf("解析工具参数失败: %v", err)
		}
	}

	// Prepare the call request
	toolName := t.callName()
	callRequest := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      toolName,
			Arguments: arguments,
		},
	}

	result, err := callRemoteMCPTool(ctx, t.client, callRequest)
	if err != nil {
		if !isRetryableRemoteCallError(err) {
			return retContent, fmt.Errorf("调用工具失败: %v", err)
		}

		log.Warnf("tool %s call failed, will reconnect server %s and retry: %v", t.info.Name, t.serverName, err)

		newClient, reconnectErr := reconnectGlobalMCPServer(t.serverName)
		if reconnectErr != nil {
			return retContent, fmt.Errorf("调用工具失败: %v，且重连服务器失败: %v", err, reconnectErr)
		}

		t.client = newClient
		result, err = callRemoteMCPTool(ctx, t.client, callRequest)
		if err != nil {
			return retContent, fmt.Errorf("重连后调用仍然失败: %v", err)
		}
	}

	if err != nil {
		return retContent, fmt.Errorf("调用工具失败: %v", err)
	}

	resultStr, err := result.MarshalJSON()
	if err != nil {
		return retContent, fmt.Errorf("工具调用返回内容转换失败: %v", err)
	}

	return string(resultStr), nil
}

func (t *McpTool) GetClient() *client.Client {
	return t.client
}

func (t *McpTool) GetServerName() string {
	return t.serverName
}
