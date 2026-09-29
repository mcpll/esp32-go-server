package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/mark3labs/mcp-go/server"
)

type WebSocketServerTransportOption func(*websocketServerTransport)

type websocketServerTransport struct {
	server    *server.MCPServer
	mcpHandle McpInterface

	ctx    context.Context
	cancel context.CancelFunc

	wg sync.WaitGroup
}

func WithWebSocketServerOptionMcpServer(server *server.MCPServer) WebSocketServerTransportOption {
	return func(t *websocketServerTransport) {
		t.server = server
	}
}

func NewWebSocketServerTransport(mcpHandle McpInterface, opts ...WebSocketServerTransportOption) (*websocketServerTransport, error) {
	t := &websocketServerTransport{
		mcpHandle: mcpHandle,
	}

	for _, opt := range opts {
		opt(t)
	}

	return t, nil
}

func (t *websocketServerTransport) Run() error {
	ctx, cancel := context.WithCancel(context.Background())
	t.ctx = ctx
	t.cancel = cancel

	// 主消息处理循环
	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		for {
			select {
			case <-t.ctx.Done():
				return
			default:
				message, err := t.mcpHandle.RecvMcpMsg(60000)
				if err != nil {
					fmt.Println("read message error:", err)
					t.cancel()
					return
				}
				fmt.Println("recv message:", string(message))

				response := t.server.HandleMessage(t.ctx, message)
				if response != nil {
					responseBytes, err := json.Marshal(response)
					if err != nil {
						fmt.Println("marshal message error:", err)
						t.cancel()
						return
					}
					fmt.Println("send message:", string(responseBytes))
					err = t.mcpHandle.SendMcpMsg(responseBytes)
					if err != nil {
						fmt.Println("write message error:", err)
						t.cancel()
						return
					}
				}
			}
		}
	}()

	// 等待所有goroutine完成
	t.wg.Wait()
	return nil
}
