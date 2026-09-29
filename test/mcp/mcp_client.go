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

// ExampleMCPInteractive interactive demo of using MCP Host
func main() {
	fmt.Println("=== MCP Host 交互式使用示例 ===")

	// 1. configure MCP
	setupMCPConfig()

	// 2. start the global MCP manager
	globalManager := GetGlobalMCPManager()
	if err := globalManager.Start(); err != nil {
		log.Printf("failed to start global MCP manager: %v", err)
		return
	}
	defer globalManager.Stop()

	// 3. show global tools
	showGlobalTools(globalManager)

	// 4. wait interactively for user input
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
		missCount = 0 // reset after a tool is found
		// fetch and print a parameter example
		info, err := tool.Info(context.Background())
		if err != nil {
			fmt.Printf("获取工具信息失败: %v\n", err)
			continue
		}
		fmt.Println("参数示例：")
		if info.ParamsOneOf != nil {
			// try serializing as pretty JSON
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

// ExampleMCPUsage demos using MCP Host
func ExampleMCPUsage(t *testing.T) {
	fmt.Println("=== MCP Host 使用示例 ===")

	// 1. configure MCP
	setupMCPConfig()

	// 2. start the global MCP manager
	globalManager := GetGlobalMCPManager()
	if err := globalManager.Start(); err != nil {
		log.Printf("failed to start global MCP manager: %v", err)
		return
	}
	defer globalManager.Stop()

	// 3. get the device MCP manager
	deviceManager := GetDeviceMCPManager()

	// 4. simulate waiting for tool registration
	time.Sleep(30 * time.Second)

	// 5. show global tools
	showGlobalTools(globalManager)

	// 6. show device tools
	showDeviceTools(deviceManager, "example_device")

	// 7. demo tool calling
	demonstrateToolCalling(globalManager)
}

// setupMCPConfig sets MCP config
func setupMCPConfig() {
	fmt.Println("1. 设置MCP配置...")

	// set global MCP config
	viper.Set("mcp.global.enabled", true)
	viper.Set("mcp.global.reconnect_interval", 5)
	viper.Set("mcp.global.max_reconnect_attempts", 3)

	// set MCP server list
	servers := []map[string]interface{}{
		{
			"name":    "global_mcp",
			"sse_url": "http://192.168.208.214:3001/sse",
			"enabled": true,
		},
	}
	viper.Set("mcp.global.servers", servers)

	// set device MCP config
	viper.Set("mcp.device.enabled", true)
	viper.Set("mcp.device.websocket_path", "/xiaozhi/mcp/")
	viper.Set("mcp.device.max_connections_per_device", 5)

	fmt.Println("   ✓ MCP配置已设置")
}

// showGlobalTools shows global tools
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

// showDeviceTools shows device tools
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

// demonstrateToolCalling demos tool calling
func demonstrateToolCalling(manager *GlobalMCPManager) {
	fmt.Println("\n4. 工具调用演示:")

	// try to get a tool
	tool, exists := manager.GetToolByName("random")
	if !exists {
		fmt.Println("   暂无可用工具进行演示")
		return
	}

	argsInJSON := `{"min":1,"max":100}`
	fmt.Printf("argsInJSON: %s", argsInJSON)
	// call tool
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
// ExampleMCPTool demos a custom MCP tool
func ExampleMCPTool() {
	fmt.Println("=== custom MCP tool example ===")

	// create example tool
	tool := &mcpTool{
		name:        "example_tool",
		description: "this is an example tool",
		inputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"message": map[string]interface{}{
					"type":        "string",
					"description": "message to process",
				},
			},
			"required": []string{"message"},
		},
		serverName: "example_server",
		client:     nil, // in real use a real client must be provided
	}

	// get tool info
	info, err := tool.Info(context.Background())
	if err != nil {
		fmt.Printf("failed to get tool info: %v\n", err)
		return
	}

	fmt.Printf("tool name: %s\n", info.Name)
	fmt.Printf("tool description: %s\n", info.Desc)

	// Note: without a real client connection, tool calls will fail
	fmt.Println("Note: without a real MCP client connection, tool calling cannot be demonstrated")
}*/

// ExampleWebSocketClient demos a WebSocket client connection
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
