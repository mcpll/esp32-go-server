package mcp

import (
	"fmt"
	"sync"

	log "xiaozhi-esp32-server-golang/logger"

	"github.com/bytedance/sonic"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/getkin/kin-openapi/openapi3"

	mcp_protocol "github.com/ThinkInAIXYZ/go-mcp/protocol"
)

// LocalMCPManager local MCP tool manager
type LocalMCPManager struct {
	tools map[string]*McpTool // tool name -> tool definition
	mu    sync.RWMutex        // RW mutex guarding concurrent access
}

var (
	localManager *LocalMCPManager
	localOnce    sync.Once
)

// GetLocalMCPManager returns the local MCP manager singleton
func GetLocalMCPManager() *LocalMCPManager {
	localOnce.Do(func() {
		localManager = &LocalMCPManager{
			tools: make(map[string]*McpTool),
		}
		// Initialize default local tools
		localManager.initDefaultTools()
	})
	return localManager
}

// initDefaultTools initializes default local tools
func (l *LocalMCPManager) initDefaultTools() {

	log.Info("local MCP manager default tools initialized")
}

// RegisterTool registers a local tool
func (l *LocalMCPManager) RegisterTool(tool *McpTool) error {
	if tool == nil {
		return fmt.Errorf("工具不能为空")
	}

	if tool.info.Name == "" {
		return fmt.Errorf("工具名称不能为空")
	}

	if !tool.isLocal || tool.localHandler == nil {
		return fmt.Errorf("工具处理函数不能为空")
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	// Check whether the tool already exists
	if _, exists := l.tools[tool.info.Name]; exists {
		log.Warnf("local tool %s already exists, will be overwritten", tool.info.Name)
	}

	l.tools[tool.info.Name] = tool
	log.Infof("registered local tool: %s - %s", tool.info.Name, tool.info.Desc)
	return nil
}

func (l *LocalMCPManager) convertStructToOpenaipi3Schema(inputParams any) (*openapi3.Schema, error) {
	// Use github.com/ThinkInAIXYZ/go-mcp to generate a tool from a struct, then convert to openapi3.Schema
	toolInstance, err := mcp_protocol.NewTool("get_system_info", "获取系统基本信息", inputParams)
	if err != nil {
		return nil, err
	}

	marshaledInputSchema, err := sonic.Marshal(toolInstance.InputSchema)
	if err != nil {
		return nil, err
	}

	inputSchema := &openapi3.Schema{}
	err = sonic.Unmarshal(marshaledInputSchema, inputSchema)
	if err != nil {
		return nil, err
	}
	return inputSchema, nil
}

// RegisterToolFunc registers a tool function (simplified)
func (l *LocalMCPManager) RegisterToolFunc(name, description string, inputParams any, handler LocalToolHandler) error {
	inputSchema, err := l.convertStructToOpenaipi3Schema(inputParams)
	if err != nil {
		log.Errorf("Failed to convert struct to openapi3 schema: %v", err)
		return err
	}
	tool := &McpTool{
		info: &schema.ToolInfo{
			Name:        name,
			Desc:        description,
			ParamsOneOf: schema.NewParamsOneOfByOpenAPIV3(inputSchema),
		},
		isLocal:      true,
		localHandler: handler,
	}
	return l.RegisterTool(tool)
}

// UnregisterTool unregisters a tool
func (l *LocalMCPManager) UnregisterTool(name string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if _, exists := l.tools[name]; !exists {
		return fmt.Errorf("工具 %s 不存在", name)
	}

	delete(l.tools, name)
	log.Infof("unregistered local tool: %s", name)
	return nil
}

// GetAllTools returns all local tools in Eino tool interface form
func (l *LocalMCPManager) GetAllTools() map[string]tool.InvokableTool {
	l.mu.RLock()
	defer l.mu.RUnlock()

	result := make(map[string]tool.InvokableTool)
	for name, mcpTool := range l.tools {
		result[name] = mcpTool
	}
	return result
}

// GetToolByName returns a tool by name
func (l *LocalMCPManager) GetToolByName(name string) (tool.InvokableTool, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	mcpTool, exists := l.tools[name]
	if !exists {
		return nil, false
	}

	return mcpTool, true
}

// GetToolNames returns all tool names
func (l *LocalMCPManager) GetToolNames() []string {
	l.mu.RLock()
	defer l.mu.RUnlock()

	names := make([]string, 0, len(l.tools))
	for name := range l.tools {
		names = append(names, name)
	}
	return names
}

// GetToolCount returns the tool count
func (l *LocalMCPManager) GetToolCount() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return len(l.tools)
}

// Start starts the local manager (reserved)
func (l *LocalMCPManager) Start() error {
	log.Info("local MCP manager started")
	return nil
}

// Stop stops the local manager (reserved)
func (l *LocalMCPManager) Stop() error {
	// Note: we do not clear tools; local manager tools should stay available for the app lifetime
	// To clear tools, call UnregisterTool explicitly
	log.Info("local MCP manager stopped")
	return nil
}
