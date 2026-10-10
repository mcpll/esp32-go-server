package websocket

import (
	"net/http"
	"strings"

	"xiaozhi-esp32-server-golang/internal/app/server/mcpaccess"
	"xiaozhi-esp32-server-golang/internal/domain/mcp"
	log "xiaozhi-esp32-server-golang/logger"
)

// handleMCPWebSocket handles MCP WebSocket connections
func (s *WebSocketServer) handleMCPWebSocket(w http.ResponseWriter, r *http.Request) {
	var agentId string

	// First try to get token from URL params
	token := r.URL.Query().Get("token")
	if token == "" {
		log.Warn("missing token")
		http.Error(w, "missing token", http.StatusUnauthorized)
		return
	}
	claims, err := s.parseMCPToken(token)
	if err != nil {
		log.Warnf("failed to parse token: %v", err)
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}
	log.Infof("token parsed successfully, agent %s", claims.AgentID)
	agentId = claims.AgentID

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

// parseMCPToken parses the MCP access-point JWT. Agent ids are PocketBase strings.
func (s *WebSocketServer) parseMCPToken(tokenString string) (*mcpaccess.Claims, error) {
	return mcpaccess.ParseAgentToken(tokenString)
}

// handleMCPAPI handles MCP REST API requests
func (s *WebSocketServer) handleMCPAPI(w http.ResponseWriter, r *http.Request) {
	// Extract deviceId from URL path
	// URL format: /xiaozhi/api/mcp/tools/{deviceId}
	path := strings.TrimPrefix(r.URL.Path, "/xiaozhi/api/mcp/tools/")
	if path == "" || path == r.URL.Path {
		http.Error(w, "missing device id", http.StatusBadRequest)
		return
	}

	deviceID := strings.TrimSuffix(path, "/")
	if deviceID == "" {
		http.Error(w, "device id is required", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case "GET":
		s.handleGetDeviceTools(w, r, deviceID)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
