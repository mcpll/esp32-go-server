package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	neturl "net/url"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/gorilla/websocket"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const defaultAgentEndpointAuthToken = "xiaozhi_mcp_openclaw_secret_key"

var agentEndpointAuthToken = defaultAgentEndpointAuthToken

type agentWsEndpointRuntime struct {
	conn   *websocket.Conn
	server *server.MCPServer

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	writeMu sync.Mutex
	stateMu sync.Mutex
	methods map[string]int
	results map[string]int
	lastErr error
}

func runAgentWsEndpointMCPCase(serverAddr, deviceID string, testCase *protocolTestCase) error {
	agentID := buildAgentWsEndpointAgentID(deviceID, testCase.Name)
	endpoint, err := buildAgentWsEndpointURL(serverAddr, agentID)
	if err != nil {
		return err
	}
	fmt.Printf("智能体 MCP endpoint: %s\n", endpoint)

	runtime, err := startAgentWsEndpointRuntime(endpoint)
	if err != nil {
		return err
	}
	defer runtime.Close()

	timeout := testCase.Timeout
	if timeout <= 0 {
		timeout = autoCaseTimeout
	}
	if err := runtime.waitForMethodCount("initialize", 1, timeout); err != nil {
		return err
	}
	if err := runtime.waitForMethodCount("tools/list", 1, timeout); err != nil {
		return err
	}
	if err := runtime.waitForResultCount("initialize", 1, timeout); err != nil {
		return err
	}
	if err := runtime.waitForResultCount("tools/list", 1, timeout); err != nil {
		return err
	}

	switch testCase.Kind {
	case protocolCaseAgentWsEndpointKeepalive:
		if err := runtime.waitForMethodCount("ping", 1, timeout); err != nil {
			return err
		}
		if err := runtime.waitForMethodCount("tools/list", 2, timeout); err != nil {
			return err
		}
		if err := runtime.waitForResultCount("tools/list", 2, timeout); err != nil {
			return err
		}
	default:
		if err := runtime.waitForStableConnection(2 * time.Second); err != nil {
			return err
		}
	}

	return runtime.err()
}

func startAgentWsEndpointRuntime(endpoint string) (*agentWsEndpointRuntime, error) {
	conn, _, err := websocket.DefaultDialer.Dial(endpoint, http.Header{})
	if err != nil {
		return nil, fmt.Errorf("连接智能体 MCP endpoint 失败: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	runtime := &agentWsEndpointRuntime{
		conn:    conn,
		server:  newAgentWsEndpointMCPServer(),
		ctx:     ctx,
		cancel:  cancel,
		methods: make(map[string]int),
		results: make(map[string]int),
	}
	runtime.start()
	return runtime, nil
}

func newAgentWsEndpointMCPServer() *server.MCPServer {
	mcpServer := server.NewMCPServer("auto_test_agent_ws_endpoint", "1.0.0")

	helloTool := mcp.NewTool("hello_world",
		mcp.WithDescription("Say hello to someone"),
		mcp.WithString("name",
			mcp.Required(),
			mcp.Description("Name of the person to greet"),
		),
	)
	weatherTool := mcp.NewTool("query_weather",
		mcp.WithDescription("查询天气"),
	)

	mcpServer.AddTool(helloTool, agentWsEndpointHelloHandler)
	mcpServer.AddTool(weatherTool, agentWsEndpointWeatherHandler)
	return mcpServer
}

func (r *agentWsEndpointRuntime) start() {
	r.wg.Add(2)
	go r.readLoop()
	go r.websocketPingLoop()
}

func (r *agentWsEndpointRuntime) readLoop() {
	defer r.wg.Done()
	for {
		select {
		case <-r.ctx.Done():
			return
		default:
		}

		_, message, err := r.conn.ReadMessage()
		if err != nil {
			if r.ctx.Err() == nil {
				r.setErr(fmt.Errorf("读取智能体 MCP endpoint 消息失败: %w", err))
				r.cancel()
			}
			return
		}

		method := r.recordJSONRPCMethod(message)
		response := r.server.HandleMessage(r.ctx, message)
		if response == nil {
			continue
		}

		responseBytes, err := json.Marshal(response)
		if err != nil {
			r.setErr(fmt.Errorf("序列化 MCP 响应失败: %w", err))
			r.cancel()
			return
		}
		if err := r.writeMessage(websocket.TextMessage, responseBytes); err != nil {
			r.setErr(fmt.Errorf("发送 MCP 响应失败: %w", err))
			r.cancel()
			return
		}
		r.recordResult(method)
	}
}

func (r *agentWsEndpointRuntime) websocketPingLoop() {
	defer r.wg.Done()
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.ctx.Done():
			return
		case <-ticker.C:
			if err := r.writeMessage(websocket.PingMessage, []byte{}); err != nil {
				r.setErr(fmt.Errorf("发送 websocket ping 失败: %w", err))
				r.cancel()
				return
			}
		}
	}
}

func (r *agentWsEndpointRuntime) writeMessage(messageType int, payload []byte) error {
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	return r.conn.WriteMessage(messageType, payload)
}

func (r *agentWsEndpointRuntime) recordJSONRPCMethod(message []byte) string {
	var request struct {
		Method string `json:"method"`
	}
	if err := json.Unmarshal(message, &request); err != nil || strings.TrimSpace(request.Method) == "" {
		return ""
	}

	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	r.methods[request.Method]++
	return request.Method
}

func (r *agentWsEndpointRuntime) recordResult(method string) {
	method = strings.TrimSpace(method)
	if method == "" {
		return
	}

	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	r.results[method]++
}

func (r *agentWsEndpointRuntime) waitForMethodCount(method string, expected int, timeout time.Duration) error {
	return r.waitForCount("MCP endpoint 方法", method, expected, timeout, r.methodCount)
}

func (r *agentWsEndpointRuntime) waitForResultCount(method string, expected int, timeout time.Duration) error {
	return r.waitForCount("MCP endpoint result", method, expected, timeout, r.resultCount)
}

func (r *agentWsEndpointRuntime) waitForCount(label string, method string, expected int, timeout time.Duration, countFunc func(string) int) error {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		if countFunc(method) >= expected {
			return nil
		}
		if err := r.err(); err != nil {
			return err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("等待 %s %s 数量达到 %d 超时，当前=%d", label, method, expected, countFunc(method))
		}

		<-ticker.C
	}
}

func (r *agentWsEndpointRuntime) waitForStableConnection(duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timer.C:
			return r.err()
		case <-ticker.C:
			if err := r.err(); err != nil {
				return err
			}
		}
	}
}

func (r *agentWsEndpointRuntime) methodCount(method string) int {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	return r.methods[method]
}

func (r *agentWsEndpointRuntime) resultCount(method string) int {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	return r.results[method]
}

func (r *agentWsEndpointRuntime) setErr(err error) {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	if r.lastErr == nil {
		r.lastErr = err
	}
}

func (r *agentWsEndpointRuntime) err() error {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	return r.lastErr
}

func (r *agentWsEndpointRuntime) Close() {
	r.cancel()
	_ = r.conn.Close()
	r.wg.Wait()
}

func buildAgentWsEndpointURL(serverAddr, agentID string) (string, error) {
	parsed, err := neturl.Parse(strings.TrimSpace(serverAddr))
	if err != nil {
		return "", fmt.Errorf("解析 server 地址失败: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("server 地址必须包含 scheme 和 host: %s", serverAddr)
	}

	switch parsed.Scheme {
	case "http":
		parsed.Scheme = "ws"
	case "https":
		parsed.Scheme = "wss"
	case "ws", "wss":
	default:
		return "", fmt.Errorf("不支持的 server scheme: %s", parsed.Scheme)
	}

	token, err := signAgentWsEndpointToken(agentID)
	if err != nil {
		return "", err
	}
	parsed.Path = "/mcp"
	parsed.RawQuery = neturl.Values{"token": []string{token}}.Encode()
	return parsed.String(), nil
}

func signAgentWsEndpointToken(agentID string) (string, error) {
	now := time.Now()
	claims := jwt.MapClaims{
		"userId":     1,
		"agentId":    agentID,
		"endpointId": "agent_" + agentID,
		"purpose":    "mcp-endpoint",
		"iat":        now.Unix(),
		"exp":        now.Add(2 * time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	secret := strings.TrimSpace(agentEndpointAuthToken)
	if secret == "" {
		secret = defaultAgentEndpointAuthToken
	}
	return token.SignedString([]byte(secret))
}

func buildAgentWsEndpointAgentID(deviceID, caseName string) string {
	base := strings.TrimSpace(deviceID)
	if base == "" {
		base = "auto-test"
	}
	return sanitizeCaseDeviceID(base+"-"+caseName, 72)
}

func agentWsEndpointHelloHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name, err := request.RequireString("name")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("Hello, %s!", name)), nil
}

func agentWsEndpointWeatherHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultText("天气晴朗 20度 北风3级"), nil
}
