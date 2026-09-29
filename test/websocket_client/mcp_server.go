package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type McpInterface interface {
	SendMcpMsg(payload json.RawMessage) error
	RecvMcpMsg(timeOut int) ([]byte, error)
}

type McpTransport struct {
	SendMsgChan chan []byte
	RecvMsgChan chan []byte
}

func (c *McpTransport) SendMcpMsg(payload json.RawMessage) error {
	serverMsg := ServerMessage{
		Type:    MessageTypeMcp,
		PayLoad: payload,
	}
	serverBytes, err := json.Marshal(serverMsg)
	if err != nil {
		return err
	}
	select {
	case c.SendMsgChan <- serverBytes:
		return nil
	case <-time.After(time.Duration(2000) * time.Millisecond):
		return fmt.Errorf("mcp 发送消息超时")
	}
}

func (c *McpTransport) RecvMcpMsg(timeOut int) ([]byte, error) {
	select {
	case msg := <-c.RecvMsgChan:
		return msg, nil
	case <-time.After(time.Duration(timeOut) * time.Millisecond):
		return nil, fmt.Errorf("mcp 接收消息超时")
	}
}

func NewMcpServer(sendMsgChan chan []byte, recvMsgChan chan []byte) {
	/*
		hooks := &server.Hooks{}

		hooks.AddAfterInitialize(func(ctx context.Context, id any, message *mcp.InitializeRequest, result *mcp.InitializeResult) {
			result.ServerInfo.Name = "taiji-pi-s3"
			result.ServerInfo.Version = "1.0.0"
			fmt.Printf("afterInitialize: %v, %v, %v\n", id, message, result)
		})*/

	s := server.NewMCPServer("taiji-pi-s3", "1.0.0")

	// Add tool
	/*tool := mcp.NewTool("hello_world",
		mcp.WithDescription("Say hello to someone"),
		mcp.WithString("name",
			mcp.Required(),
			mcp.Description("Name of the person to greet"),
		),
	)

	// Add weather query tool
	weatherTool := mcp.NewTool("query_weather",
		mcp.WithDescription("Query Dalian weather"),
	)

	// Add random-number tool (string params; convert in handler)
	randomNumberTool := mcp.NewTool("random_number",
		mcp.WithDescription("Generate a random int in a range"),
		mcp.WithNumber("min",
			mcp.Required(),
			mcp.Description("Minimum"),
		),
		mcp.WithNumber("max",
			mcp.Required(),
			mcp.Description("Maximum"),
		),
	)



	// Register all tools and handlers
	s.AddTool(tool, helloHandler)
	s.AddTool(weatherTool, queryWeatherHandler)
	s.AddTool(randomNumberTool, randomNumberHandler)*/

	// Add joke tool
	jokeTool := mcp.NewTool("tell_joke",
		mcp.WithDescription("讲一个笑话"),
	)
	s.AddTool(jokeTool, jokeHandler)

	// Add joke tool
	visionTool := mcp.NewTool("vision_tool",
		mcp.WithDescription("拍照分析图片"),
	)
	s.AddTool(visionTool, visionHandler)

	mcpHandle := &McpTransport{
		SendMsgChan: sendMsgChan,
		RecvMsgChan: recvMsgChan,
	}

	transport, err := NewWebSocketServerTransport(mcpHandle, WithWebSocketServerOptionMcpServer(s))
	if err != nil {
		log.Fatalf("Failed to create websocket server transport: %v", err)
	}
	transport.Run()
}

func helloHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name, err := request.RequireString("name")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf("Hello, %s!", name)), nil
}

// Weather query handler
func queryWeatherHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultText("天气晴朗 20度 北风3级"), nil
}

// Random number handler
func randomNumberHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	//Reimplemented
	min := request.GetInt("min", 0)
	max := request.GetInt("max", 100)

	if min > max {
		return mcp.NewToolResultError("min 不能大于 max"), nil
	}
	rnd := min
	if max > min {
		rnd = min + int(time.Now().UnixNano()%int64(max-min+1))
	}
	return mcp.NewToolResultText(fmt.Sprintf("随机数：%d", rnd)), nil
}

// Joke handler
func jokeHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	joke := "有一天小明去上学，老师问他为什么迟到，小明说：因为作业太难，梦里都在写作业，结果一觉醒来就迟到了。"
	return mcp.NewToolResultText(joke), nil
}

func visionHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	image := "1.jpg"
	question := "图片中有什么？"
	url := GetServerVisionURL()
	if url == "" {
		url = "http://192.168.208.214:8989/xiaozhi/api/vision" // Default when server did not push a URL
	}
	deviceId := "shijingbo"
	responseText, err := requestVllm(image, question, url, deviceId)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(responseText), nil
}
