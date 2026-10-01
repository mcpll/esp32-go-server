package mcp

import (
	"fmt"
	"sync"

	log "xiaozhi-esp32-server-golang/logger"
)

// MCPManager unified MCP manager coordinating all sub-managers
type MCPManager struct {
	localManager  *LocalMCPManager
	globalManager *GlobalMCPManager
	// deviceManager may manage a device-manager pool here later

	mu      sync.RWMutex
	started bool
}

var (
	mcpManager *MCPManager
	mcpOnce    sync.Once
)

// GetMCPManager returns the unified MCP manager singleton
func GetMCPManager() *MCPManager {
	mcpOnce.Do(func() {
		mcpManager = &MCPManager{
			localManager:  GetLocalMCPManager(),
			globalManager: GetGlobalMCPManager(),
			started:       false,
		}
	})
	return mcpManager
}

// Start starts all MCP managers
func (m *MCPManager) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		log.Warn("MCP manager already started")
		return nil
	}

	log.Info("=== starting MCP manager cluster ===")

	// 1. Start local manager first
	log.Info("starting local MCP manager...")
	if err := m.localManager.Start(); err != nil {
		log.Errorf("failed to start local MCP manager: %v", err)
		return fmt.Errorf("启动本地MCP管理器失败: %v", err)
	}

	// 2. Then start global manager
	log.Info("starting global MCP manager...")
	if err := m.globalManager.Start(); err != nil {
		log.Errorf("failed to start global MCP manager: %v", err)
		return fmt.Errorf("启动全局MCP管理器失败: %v", err)
	}

	// 3. Device managers are created on connect; nothing to start here
	log.Info("device MCP managers will be created dynamically on connect")

	m.started = true
	log.Info("=== MCP manager cluster started ===")

	// Print startup stats
	m.printStartupStats()

	return nil
}

// Stop stops all MCP managers
func (m *MCPManager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.started {
		log.Info("MCP manager not started, nothing to stop")
		return nil
	}

	log.Info("=== stopping MCP manager cluster ===")

	// Stop managers in reverse order
	// 1. Stop global manager
	log.Info("stopping global MCP manager...")
	if err := m.globalManager.Stop(); err != nil {
		log.Errorf("failed to stop global MCP manager: %v", err)
	}

	// 2. Stop local manager
	log.Info("stopping local MCP manager...")
	if err := m.localManager.Stop(); err != nil {
		log.Errorf("failed to stop local MCP manager: %v", err)
	}

	// 3. Device managers clean up on disconnect
	log.Info("device MCP connections will be cleaned up automatically")

	m.started = false
	log.Info("=== MCP manager cluster stopped ===")
	return nil
}

// IsStarted reports whether managers are started
func (m *MCPManager) IsStarted() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.started
}

// GetLocalManager returns the local manager
func (m *MCPManager) GetLocalManager() *LocalMCPManager {
	return m.localManager
}

// GetGlobalManager returns the global manager
func (m *MCPManager) GetGlobalManager() *GlobalMCPManager {
	return m.globalManager
}

// printStartupStats prints startup stats
func (m *MCPManager) printStartupStats() {
	localToolCount := m.localManager.GetToolCount()
	globalToolCount := len(m.globalManager.GetAllTools())

	log.Infof("MCP manager startup stats:")
	log.Infof("  - local tools: %d", localToolCount)
	log.Infof("  - global tools: %d", globalToolCount)
	log.Infof("  - device managers: dynamic")
	log.Infof("  - total tools: %d", localToolCount+globalToolCount)
}

// GetAllManagersStatus returns status for all managers
func (m *MCPManager) GetAllManagersStatus() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	status := map[string]interface{}{
		"mcp_manager": map[string]interface{}{
			"started": m.started,
		},
		"local_manager": map[string]interface{}{
			"tool_count": m.localManager.GetToolCount(),
			"tool_names": m.localManager.GetToolNames(),
		},
		"global_manager": map[string]interface{}{
			"tool_count": len(m.globalManager.GetAllTools()),
		},
		"device_manager": map[string]interface{}{
			"active_devices": mcpClientPool.device2McpClient.Count(),
		},
	}

	return status
}

// RestartManager restarts a named manager
func (m *MCPManager) RestartManager(managerType string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.started {
		return fmt.Errorf("MCP管理器集群未启动")
	}

	switch managerType {
	case "local":
		log.Info("restarting local MCP manager...")
		if err := m.localManager.Stop(); err != nil {
			log.Errorf("failed to stop local manager: %v", err)
		}
		if err := m.localManager.Start(); err != nil {
			return fmt.Errorf("重启本地管理器失败: %v", err)
		}
		log.Info("local MCP manager restarted")

	case "global":
		log.Info("restarting global MCP manager...")
		if err := m.globalManager.Stop(); err != nil {
			log.Errorf("failed to stop global manager: %v", err)
		}
		if err := m.globalManager.Start(); err != nil {
			return fmt.Errorf("重启全局管理器失败: %v", err)
		}
		log.Info("global MCP manager restarted")

	default:
		return fmt.Errorf("不支持的管理器类型: %s", managerType)
	}

	return nil
}

// Convenience helpers for backward compatibility

// StartMCPManagers start all MCP managers (convenience)
func StartMCPManagers() error {
	return GetMCPManager().Start()
}

// StopMCPManagers stop all MCP managers (convenience)
func StopMCPManagers() error {
	return GetMCPManager().Stop()
}

// GetMCPManagerStatus MCP manager status (convenience)
func GetMCPManagerStatus() map[string]interface{} {
	return GetMCPManager().GetAllManagersStatus()
}
