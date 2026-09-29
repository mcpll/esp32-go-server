package main

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/gorilla/websocket"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

func main() {
	websocketServer()
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // 允许所有来源的连接
	},
}

func websocketServer() {
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Fatal(err)
			return
		}

		transport, err := NewWebsocketTransport(conn)
		if err != nil {
			fmt.Println("Failed to create transport: %v", err)
			return
		}

		client := client.NewClient(transport)

		ctx := context.Background()

		err = client.Start(ctx)
		if err != nil {
			fmt.Println("Failed to start client: %v", err)
			return
		}

		initRequest := mcp.InitializeRequest{
			Params: mcp.InitializeParams{
				ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
				ClientInfo: mcp.Implementation{
					Name:    "mcp-go",
					Version: "0.1.0",
				},
				Capabilities: mcp.ClientCapabilities{},
			},
		}

		serverInfo, err := client.Initialize(ctx, initRequest)
		if err != nil {
			fmt.Println("Failed to initialize: %v", err)
			return
		}

		fmt.Println("Server info:")
		fmt.Println(serverInfo)

		if serverInfo.Capabilities.Tools != nil {
			tools, err := client.ListTools(ctx, mcp.ListToolsRequest{})
			if err != nil {
				fmt.Println("Failed to list tools: %v", err)
				return
			}

			fmt.Println("Available tools:")
			for _, tool := range tools.Tools {
				fmt.Println(tool.Name)
			}
		}
		defer conn.Close()
	})

	log.Fatal(http.ListenAndServe(":6666", nil))
}
