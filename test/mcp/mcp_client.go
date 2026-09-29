package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"

	. "xiaozhi-esp32-server-golang/internal/domain/mcp"
)

// ExampleMCPInteractive 交互式演示如何使用MCP Host
func main() {
	fmt.Println("=== MCP Host 交互式使用示例 ===")

	// 1. 配置MCP
	setupMCPConfig()

	// 2. 启动全局MCP管理器
	globalManager := GetGlobalMCPManager()
	if err := globalManager.Start(); err != nil {
		log.Printf("启动全局MCP管理器失败: %v", err)
		return
	}
	defer globalManager.Stop()

	// 3. 展示全局工具
	showGlobalTools(globalManager)

	// 4. 交互式等待用户输入
	reader := bufio.NewReader(os.Stdin)
	missCount := 0
	for {
		fmt.Print("\n请输入要调用的工具名称（或 exit 退出，? 查看工具列表）：")
		toolName, _ := reader.ReadString('\n')
		toolName = strings.TrimSpace(toolName)
		if toolName == "exit" {
			fmt.Println("已退出交互模式。")
			break
		}
		if toolName == "?" {
			showGlobalTools(globalManager)
			continue
		}
		tool, exists := globalManager.GetToolByName(toolName)
		if !exists {
			fmt.Printf("未找到工具：%s\n", toolName)
			missCount++
			if missCount >= 3 {
				fmt.Println("连续3次未找到工具，自动退出交互模式。")
				break
			}
			continue
		}
		missCount = 0 // 找到工具则重置
		// 获取参数示例并打印
		info, err := tool.Info(context.Background())
		if err != nil {
			fmt.Printf("获取工具信息失败: %v\n", err)
			continue
		}
		fmt.Println("参数示例：")
		if info.ParamsOneOf != nil {
			// 尝试序列化为 JSON 美观输出
			if b, err := json.MarshalIndent(info.ParamsOneOf, "", "  "); err == nil {
				fmt.Println(string(b))
			} else {
				fmt.Printf("%+v\n", info.ParamsOneOf)
			}
		} else {
			fmt.Println("  (无参数或未定义)")
		}
		fmt.Print("请输入参数（JSON格式）：")
		argsInJSON, _ := reader.ReadString('\n')
		argsInJSON = strings.TrimSpace(argsInJSON)
		fmt.Println("   正在调用工具...")
		result, err := tool.InvokableRun(context.Background(), argsInJSON)
		if err != nil {
			fmt.Printf("   ❌ 工具调用失败: %v\n", err)
			continue
		}
		fmt.Printf("   ✓ 工具调用成功: %s\n", result)
	}
}

// ExampleMCPUsage 演示如何使用MCP Host
func ExampleMCPUsage(t *testing.T) {
	fmt.Println("=== MCP Host 使用示例 ===")

	// 1. 配置MCP
	setupMCPConfig()

	// 2. 启动全局MCP管理器
	globalManager := GetGlobalMCPManager()
	if err := globalManager.Start(); err != nil {
		log.Printf("启动全局MCP管理器失败: %v", err)
		return
	}
	defer globalManager.Stop()

	// 3. 获取设备MCP管理器
	deviceManager := GetDeviceMCPManager()

	// 4. 模拟等待工具注册
	time.Sleep(30 * time.Second)

	// 5. 展示全局工具
	showGlobalTools(globalManager)

	// 6. 展示设备工具
	showDeviceTools(deviceManager, "example_device")

	// 7. 演示工具调用
	demonstrateToolCalling(globalManager)
}

// setupMCPConfig 设置MCP配置
func setupMCPConfig() {
	fmt.Println("1. 设置MCP配置...")

	// 设置全局MCP配置
	viper.Set("mcp.global.enabled", true)
	viper.Set("mcp.global.reconnect_interval", 5)
	viper.Set("mcp.global.max_reconnect_attempts", 3)

	// 设置MCP服务器列表
	servers := []map[string]interface{}{
		{
			"name":    "global_mcp",
			"sse_url": "http://192.168.208.214:3001/sse",
			"enabled": true,
		},
	}
	viper.Set("mcp.global.servers", servers)

	// 设置设备MCP配置
	viper.Set("mcp.device.enabled", true)
	viper.Set("mcp.device.websocket_path", "/xiaozhi/mcp/")
	viper.Set("mcp.device.max_connections_per_device", 5)

	fmt.Println("   ✓ MCP配置已设置")
}

// showGlobalTools 展示全局工具
func showGlobalTools(manager *GlobalMCPManager) {
	fmt.Println("\n2. 全局工具列表:")

	tools := manager.GetAllTools()
	if len(tools) == 0 {
		fmt.Println("   暂无全局工具（需要连接到真实的MCP服务器）")
		return
	}

	for name, tool := range tools {
		info, err := tool.Info(context.Background())
		if err != nil {
			fmt.Printf("   ❌ %s: 获取信息失败 - %v\n", name, err)
			continue
		}
		fmt.Printf("   ✓ %s: %s,%+v\n", info.Name, info.Desc, info.ParamsOneOf)
	}
}

// showDeviceTools 展示设备工具
func showDeviceTools(manager *DeviceMCPManager, deviceID string) {
	fmt.Printf("\n3. 设备 %s 的工具列表:\n", deviceID)

	tools := manager.GetDeviceTools(deviceID)
	if len(tools) == 0 {
		fmt.Println("   暂无设备工具（需要设备连接到MCP WebSocket端点）")
		return
	}

	for name, tool := range tools {
		info, err := tool.Info(context.Background())
		if err != nil {
			fmt.Printf("   ❌ %s: 获取信息失败 - %v\n", name, err)
			continue
		}
		fmt.Printf("   ✓ %s: %s\n", info.Name, info.Desc)
	}
}

// demonstrateToolCalling 演示工具调用
func demonstrateToolCalling(manager *GlobalMCPManager) {
	fmt.Println("\n4. 工具调用演示:")

	// 尝试获取一个工具
	tool, exists := manager.GetToolByName("random")
	if !exists {
		fmt.Println("   暂无可用工具进行演示")
		return
	}

	argsInJSON := `{"min":1,"max":100}`
	fmt.Printf("argsInJSON: %s", argsInJSON)
	// 调用工具
	fmt.Println("   正在调用工具...")
	result, err := tool.InvokableRun(
		context.Background(),
		argsInJSON,
	)

	if err != nil {
		fmt.Printf("   ❌ 工具调用失败: %v\n", err)
		return
	}

	fmt.Printf("   ✓ 工具调用成功: %s\n", result)
}

/*
// ExampleMCPTool 演示自定义MCP工具
func ExampleMCPTool() {
	fmt.Println("=== 自定义MCP工具示例 ===")

	// 创建示例工具
	tool := &mcpTool{
		name:        "example_tool",
		description: "这是一个示例工具",
		inputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"message": map[string]interface{}{
					"type":        "string",
					"description": "要处理的消息",
				},
			},
			"required": []string{"message"},
		},
		serverName: "example_server",
		client:     nil, // 在实际使用中需要提供真实的客户端
	}

	// 获取工具信息
	info, err := tool.Info(context.Background())
	if err != nil {
		fmt.Printf("获取工具信息失败: %v\n", err)
		return
	}

	fmt.Printf("工具名称: %s\n", info.Name)
	fmt.Printf("工具描述: %s\n", info.Desc)

	// 注意：由于没有真实的客户端连接，工具调用会失败
	fmt.Println("注意: 由于没有真实的MCP客户端连接，工具调用功能无法演示")
}*/

// ExampleWebSocketClient 演示WebSocket客户端连接
func ExampleWebSocketClient() {
	fmt.Println("=== WebSocket客户端连接示例 ===")

	fmt.Print(`
JavaScript客户端示例:

const ws = new WebSocket('ws://localhost:8989/xiaozhi/mcp/device123');

ws.onopen = function() {
    console.log('MCP连接已建立');
};

ws.onmessage = function(event) {
    const message = JSON.parse(event.data);
    console.log('收到消息:', message);
    
    if (message.method === 'initialize') {
        // 响应初始化
        ws.send(JSON.stringify({
            jsonrpc: "2.0",
            id: message.id,
            result: {
                protocolVersion: "2024-11-05",
                serverInfo: {
                    name: "device-mcp-server",
                    version: "1.0.0"
                }
            }
        }));
    }
};

ws.onerror = function(error) {
    console.error('WebSocket错误:', error);
};

ws.onclose = function() {
    console.log('MCP连接已关闭');
};
`)
}
