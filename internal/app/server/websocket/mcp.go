package websocket

import (
	"net/http"
	"strings"
	"xiaozhi-esp32-server-golang/internal/domain/mcp"
	"xiaozhi-esp32-server-golang/internal/util"
	log "xiaozhi-esp32-server-golang/logger"

	"github.com/golang-jwt/jwt/v4"
)

// MCPClaims JWT claims
type MCPClaims struct {
	UserID     uint   `json:"userId"`
	AgentID    string `json:"agentId"`
	EndpointID string `json:"endpointId"`
	Purpose    string `json:"purpose"`
	jwt.RegisteredClaims
}

// handleMCPWebSocket handles MCP WebSocket connections
func (s *WebSocketServer) handleMCPWebSocket(w http.ResponseWriter, r *http.Request) {
	var agentId string

	// First try to get token from URL params
	token := r.URL.Query().Get("token")
	if token != "" {
		// Parse device ID from token
		claims, err := s.parseMCPToken(token)
		if err != nil {
			log.Warnf("failed to parse token: %v", err)
			http.Error(w, "无效的token", http.StatusUnauthorized)
			return
		}
		log.Infof("token parsed successfully: %v", claims)

		agentId = claims.AgentID
	} else {
		log.Errorf("missing token")
		return
	}

	log.Infof("received MCP server WebSocket connection request, Agent ID: %s", agentId)

	// Upgrade to WebSocket
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Errorf("failed to upgrade WebSocket connection: %v", err)
		return
	}

	mcpClientSession := mcp.GetDeviceMcpClient(agentId)
	if mcpClientSession == nil {
		mcpClientSession = mcp.NewDeviceMCPSession(agentId)
		mcp.AddDeviceMcpClient(agentId, mcpClientSession)
	}

	// Create MCP client
	mcpClient := mcp.NewWsEndPointMcpClient(mcpClientSession.Ctx, agentId, conn)
	if mcpClient == nil {
		log.Errorf("failed to create MCP client")
		conn.Close()
		return
	}
	mcpClientSession.AddWsEndPointMcp(mcpClient)

	// When mcp server disconnects, clean up ws endpoint mcp client
	go func() {
		<-mcpClient.Ctx.Done()
		log.Infof("MCP connection for server %s disconnected", mcpClient.GetServerName())
	}()

	log.Infof("MCP connection for server %s established", mcpClient.GetServerName()) // todo
}

// parseMCPToken parses MCP JWT token
func (s *WebSocketServer) parseMCPToken(tokenString string) (*MCPClaims, error) {
	// Strip "Bearer " prefix
	if len(tokenString) > 7 && tokenString[:7] == "Bearer " {
		tokenString = tokenString[7:]
	}

	// Use the same key used to sign the token
	jwtSecret := []byte(util.GetManagerEndpointAuthToken())

	token, err := jwt.ParseWithClaims(tokenString, &MCPClaims{}, func(token *jwt.Token) (interface{}, error) {
		return jwtSecret, nil
	})

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*MCPClaims); ok && token.Valid {
		return claims, nil
	}

	return nil, jwt.ErrInvalidKey
}

// handleMCPAPI handles MCP REST API requests
func (s *WebSocketServer) handleMCPAPI(w http.ResponseWriter, r *http.Request) {
	// Extract deviceId from URL path
	// URL format: /xiaozhi/api/mcp/tools/{deviceId}
	path := strings.TrimPrefix(r.URL.Path, "/xiaozhi/api/mcp/tools/")
	if path == "" || path == r.URL.Path {
		http.Error(w, "缺少设备ID参数", http.StatusBadRequest)
		return
	}

	deviceID := strings.TrimSuffix(path, "/")
	if deviceID == "" {
		http.Error(w, "设备ID不能为空", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case "GET":
		s.handleGetDeviceTools(w, r, deviceID)
	default:
		http.Error(w, "不支持的HTTP方法", http.StatusMethodNotAllowed)
	}
}
