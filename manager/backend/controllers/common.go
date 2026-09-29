package controllers

import (
	"context"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// WebSocketControllerInterface 定义WebSocket控制器的接口
type WebSocketControllerInterface interface {
	RequestMcpToolDetailsFromClient(ctx context.Context, agentID string) ([]MCPTool, error)
}

// GetAgentMcpToolsCommon 获取智能体MCP工具列表的公共函数
// 这个函数可以被管理员和普通用户控制器共同使用
func GetAgentMcpToolsCommon(
	c *gin.Context,
	agentID string,
	webSocketController WebSocketControllerInterface,
	agentValidator func(agentID string) error, // 验证智能体权限的函数
) {
	log.Printf("GetAgentMcpToolsCommon 开始执行，agentID: %s", agentID)

	if agentID == "" {
		log.Printf("错误: agent_id参数为空")
		c.JSON(http.StatusBadRequest, gin.H{"error": "agent_id parameter is required"})
		return
	}

	// 验证智能体权限（由调用方提供验证逻辑）
	if err := agentValidator(agentID); err != nil {
		log.Printf("智能体验证失败: %v", err)
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	log.Printf("智能体验证成功，开始检查WebSocket控制器")

	// 检查WebSocket控制器是否存在
	if webSocketController == nil {
		// 当WebSocket控制器不存在时，返回空列表而不是错误
		log.Printf("WebSocket控制器未初始化，返回空工具列表")
		c.JSON(http.StatusOK, gin.H{"data": gin.H{"tools": []interface{}{}}})
		return
	}

	log.Printf("WebSocket控制器存在，开始请求MCP工具列表")

	// 创建上下文
	ctx := context.Background()

	// 获取工具详情（包含schema与样例）
	tools, err := webSocketController.RequestMcpToolDetailsFromClient(ctx, agentID)
	if err != nil {
		log.Printf("获取MCP工具列表失败: %v", err)
		// 如果获取失败，返回空列表而不是错误
		c.JSON(http.StatusOK, gin.H{"data": gin.H{"tools": []interface{}{}}})
		return
	}

	log.Printf("成功获取MCP工具列表: count=%d", len(tools))
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"tools": tools}})
}

func newMcpEndpointData(endpoint string) gin.H {
	return gin.H{
		"endpoint":     endpoint,
		"status":       "unknown",
		"connected":    false,
		"tools_count":  0,
		"client_count": 0,
	}
}

func applyMcpEndpointStatus(data gin.H, statusResult map[string]interface{}) {
	if data == nil || statusResult == nil {
		return
	}

	connected, _ := statusResult["connected"].(bool)
	status, _ := statusResult["status"].(string)
	status = strings.ToLower(strings.TrimSpace(status))
	if status == "" {
		if connected {
			status = "online"
		} else {
			status = "offline"
		}
	}

	data["connected"] = connected
	data["status"] = status
	if clientCount, ok := statusResult["client_count"]; ok {
		data["client_count"] = clientCount
	}
	if toolsCount, ok := statusResult["tools_count"]; ok {
		data["tools_count"] = toolsCount
	}
	if statusMessage, ok := statusResult["status_message"].(string); ok && strings.TrimSpace(statusMessage) != "" {
		data["status_message"] = statusMessage
	}
}
