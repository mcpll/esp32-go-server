package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type WSMessage struct {
	ID            string          `json:"id"`
	Timestamp     int64           `json:"timestamp"`
	Type          string          `json:"type"`
	CorrelationID string          `json:"correlation_id,omitempty"`
	Payload       json.RawMessage `json:"payload,omitempty"`
}

type MessagePayload struct {
	Content   string                 `json:"content"`
	SessionID string                 `json:"session_id,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

type PingPayload struct {
	Seq int `json:"seq"`
}

type OpenClawClaims struct {
	UserID     uint   `json:"user_id"`
	AgentID    string `json:"agent_id"`
	EndpointID string `json:"endpoint_id"`
	Purpose    string `json:"purpose"`
	jwt.RegisteredClaims
}

type AgentConnection struct {
	AgentID    string    `json:"agent_id"`
	UserID     uint      `json:"user_id"`
	EndpointID string    `json:"endpoint_id"`
	Connected  time.Time `json:"connected_at"`
	ConnID     string    `json:"conn_id"`
	RemoteAddr string    `json:"remote_addr"`
	closeMu    sync.Mutex
	closeNote  string
	conn       *websocket.Conn
	writeMu    sync.Mutex
}

func (c *AgentConnection) setCloseNote(note string) {
	if c == nil {
		return
	}
	normalized := strings.TrimSpace(note)
	if normalized == "" {
		return
	}
	c.closeMu.Lock()
	defer c.closeMu.Unlock()
	if c.closeNote == "" {
		c.closeNote = normalized
	}
}

func (c *AgentConnection) getCloseNote() string {
	if c == nil {
		return ""
	}
	c.closeMu.Lock()
	defer c.closeMu.Unlock()
	return c.closeNote
}

type ResponseEvent struct {
	At            time.Time              `json:"at"`
	AgentID       string                 `json:"agent_id"`
	UserID        uint                   `json:"user_id"`
	EndpointID    string                 `json:"endpoint_id"`
	MessageID     string                 `json:"message_id"`
	CorrelationID string                 `json:"correlation_id,omitempty"`
	DeviceID      string                 `json:"device_id"`
	Content       string                 `json:"content"`
	SessionID     string                 `json:"session_id,omitempty"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
}

type TestServer struct {
	jwtSecret []byte
	upgrader  websocket.Upgrader
	verbose   bool
	connSeq   uint64

	mu               sync.RWMutex
	connections      map[string]*AgentConnection            // conn_id -> active connection
	agentConnections map[string]map[string]*AgentConnection // agent_id -> conn_id -> connection
	responses        []ResponseEvent
}

func NewTestServer(secret string, verbose bool) *TestServer {
	return &TestServer{
		jwtSecret: []byte(secret),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
		verbose:          verbose,
		connections:      make(map[string]*AgentConnection),
		agentConnections: make(map[string]map[string]*AgentConnection),
		responses:        make([]ResponseEvent, 0, 256),
	}
}

func (s *TestServer) debugf(format string, args ...interface{}) {
	if !s.verbose {
		return
	}
	log.Printf("[debug] "+format, args...)
}

func (s *TestServer) nextConnID(agentID string) string {
	seq := atomic.AddUint64(&s.connSeq, 1)
	if strings.TrimSpace(agentID) == "" {
		return fmt.Sprintf("conn-%d", seq)
	}
	return fmt.Sprintf("%s-%d", agentID, seq)
}

func (s *TestServer) parseToken(tokenString string) (*OpenClawClaims, error) {
	tokenString = strings.TrimSpace(tokenString)
	if tokenString == "" {
		return nil, errors.New("missing token")
	}
	if strings.HasPrefix(tokenString, "Bearer ") {
		tokenString = strings.TrimSpace(strings.TrimPrefix(tokenString, "Bearer "))
	}

	token, err := jwt.ParseWithClaims(tokenString, &OpenClawClaims{}, func(token *jwt.Token) (interface{}, error) {
		return s.jwtSecret, nil
	})
	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*OpenClawClaims)
	if !ok || !token.Valid {
		return nil, jwt.ErrInvalidKey
	}
	claims.AgentID = strings.TrimSpace(claims.AgentID)
	claims.EndpointID = strings.TrimSpace(claims.EndpointID)
	claims.Purpose = strings.TrimSpace(claims.Purpose)
	if claims.AgentID == "" {
		return nil, errors.New("missing agent_id")
	}
	if claims.Purpose != "" && claims.Purpose != "openclaw-endpoint" {
		return nil, fmt.Errorf("invalid purpose: %s", claims.Purpose)
	}
	return claims, nil
}

func extractTokenFromRequest(r *http.Request) (token string, source string) {
	if r == nil {
		return "", ""
	}

	if value := strings.TrimSpace(r.URL.Query().Get("token")); value != "" {
		return value, "query.token"
	}
	if value := strings.TrimSpace(r.URL.Query().Get("access_token")); value != "" {
		return value, "query.access_token"
	}

	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if auth != "" {
		return auth, "header.Authorization"
	}

	// Some WebSocket clients pass credentials via subprotocol:
	//   Sec-WebSocket-Protocol: bearer,<token>
	//   Sec-WebSocket-Protocol: Bearer <token>
	if raw := strings.TrimSpace(r.Header.Get("Sec-WebSocket-Protocol")); raw != "" {
		parts := strings.Split(raw, ",")
		for _, part := range parts {
			p := strings.TrimSpace(part)
			if p == "" {
				continue
			}
			if strings.HasPrefix(strings.ToLower(p), "bearer ") {
				return p, "header.Sec-WebSocket-Protocol"
			}
		}
		if len(parts) >= 2 {
			candidate := strings.TrimSpace(parts[1])
			if candidate != "" {
				return candidate, "header.Sec-WebSocket-Protocol"
			}
		}
	}

	if c, err := r.Cookie("token"); err == nil {
		value := strings.TrimSpace(c.Value)
		if value != "" {
			return value, "cookie.token"
		}
	}

	return "", ""
}

func maskToken(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	if len(token) <= 12 {
		return "***"
	}
	return token[:6] + "..." + token[len(token)-4:]
}

func wsMessageTypeName(msgType int) string {
	switch msgType {
	case websocket.TextMessage:
		return "text"
	case websocket.BinaryMessage:
		return "binary"
	case websocket.CloseMessage:
		return "close"
	case websocket.PingMessage:
		return "ping"
	case websocket.PongMessage:
		return "pong"
	default:
		return fmt.Sprintf("unknown(%d)", msgType)
	}
}

func headerValuePreview(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if len(value) <= 48 {
		return value
	}
	return value[:48] + "..."
}

func truncateCloseReason(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return ""
	}
	// RFC 6455 close reason max payload is 123 bytes.
	const maxReasonBytes = 120
	if len(reason) <= maxReasonBytes {
		return reason
	}
	return reason[:maxReasonBytes]
}

func interruptModeByNote(note string) string {
	switch {
	case strings.HasPrefix(note, "server:"):
		return "active"
	case strings.HasPrefix(note, "peer:"):
		return "passive"
	case strings.TrimSpace(note) != "":
		return "active"
	default:
		return "passive"
	}
}

func (s *TestServer) closeConnection(conn *AgentConnection, code int, reason string, note string) {
	if conn == nil || conn.conn == nil {
		return
	}
	reason = strings.TrimSpace(reason)
	note = strings.TrimSpace(note)
	if note != "" {
		conn.setCloseNote(note)
	}
	if code == 0 {
		code = websocket.CloseNormalClosure
	}
	closeReason := truncateCloseReason(reason)

	log.Printf(
		"connection close triggered: agent_id=%s conn_id=%s remote=%s code=%d reason=%q note=%s",
		conn.AgentID,
		conn.ConnID,
		conn.RemoteAddr,
		code,
		closeReason,
		conn.getCloseNote(),
	)

	_ = conn.conn.WriteControl(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(code, closeReason),
		time.Now().Add(2*time.Second),
	)
	_ = conn.conn.Close()
}

func (s *TestServer) logConnectionInterrupted(conn *AgentConnection, err error) {
	if conn == nil {
		log.Printf("connection interrupted: err=%v", err)
		return
	}
	note := conn.getCloseNote()
	mode := interruptModeByNote(note)

	closeCode := 0
	closeText := ""
	var closeErr *websocket.CloseError
	if errors.As(err, &closeErr) {
		closeCode = closeErr.Code
		closeText = closeErr.Text
	}

	log.Printf(
		"connection interrupted: mode=%s agent_id=%s conn_id=%s remote=%s close_code=%d close_text=%q note=%s err=%v",
		mode,
		conn.AgentID,
		conn.ConnID,
		conn.RemoteAddr,
		closeCode,
		closeText,
		note,
		err,
	)
}

func (s *TestServer) handleWSOpenClaw(w http.ResponseWriter, r *http.Request) {
	s.debugf(
		"ws request: remote=%s method=%s path=%s ua=%q auth=%t sec_ws_proto=%q",
		r.RemoteAddr,
		r.Method,
		r.URL.RequestURI(),
		headerValuePreview(r.UserAgent()),
		strings.TrimSpace(r.Header.Get("Authorization")) != "",
		headerValuePreview(r.Header.Get("Sec-WebSocket-Protocol")),
	)

	rawToken, tokenSource := extractTokenFromRequest(r)
	claims, err := s.parseToken(rawToken)
	if err != nil {
		log.Printf(
			"ws auth failed: remote=%s path=%s source=%s token=%s err=%v",
			r.RemoteAddr,
			r.URL.RequestURI(),
			tokenSource,
			maskToken(rawToken),
			err,
		)
		http.Error(w, "invalid token: "+err.Error(), http.StatusUnauthorized)
		return
	}
	s.debugf(
		"ws auth ok: remote=%s source=%s token=%s claims={user_id:%d agent_id:%s endpoint_id:%s purpose:%s}",
		r.RemoteAddr,
		tokenSource,
		maskToken(rawToken),
		claims.UserID,
		claims.AgentID,
		claims.EndpointID,
		claims.Purpose,
	)

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("upgrade failed: %v", err)
		return
	}

	connID := s.nextConnID(claims.AgentID)
	client := &AgentConnection{
		AgentID:    claims.AgentID,
		UserID:     claims.UserID,
		EndpointID: claims.EndpointID,
		Connected:  time.Now(),
		ConnID:     connID,
		RemoteAddr: r.RemoteAddr,
		conn:       conn,
	}

	defaultCloseHandler := conn.CloseHandler()
	conn.SetCloseHandler(func(code int, text string) error {
		client.setCloseNote(fmt.Sprintf("peer:close_frame code=%d reason=%q", code, text))
		log.Printf(
			"close frame received: agent_id=%s conn_id=%s remote=%s code=%d reason=%q",
			client.AgentID,
			client.ConnID,
			client.RemoteAddr,
			code,
			text,
		)
		if defaultCloseHandler != nil {
			return defaultCloseHandler(code, text)
		}
		return nil
	})

	totalConnections := s.addConnection(client)
	log.Printf(
		"agent connection accepted: agent_id=%s conn_id=%s remote=%s total_connections=%d",
		client.AgentID,
		client.ConnID,
		client.RemoteAddr,
		totalConnections,
	)
	defer func() {
		s.debugf("ws cleanup: agent_id=%s conn_id=%s", client.AgentID, client.ConnID)
		s.removeConnection(client)
		_ = conn.Close()
	}()

	if err := s.sendHandshakeAck(client); err != nil {
		log.Printf("send handshake_ack failed: agent=%s conn_id=%s err=%v", client.AgentID, client.ConnID, err)
		return
	}

	log.Printf(
		"agent connected: agent_id=%s user_id=%d endpoint_id=%s conn_id=%s remote=%s",
		client.AgentID,
		client.UserID,
		client.EndpointID,
		client.ConnID,
		client.RemoteAddr,
	)

	for {
		msgType, data, err := conn.ReadMessage()
		if err != nil {
			s.logConnectionInterrupted(client, err)
			return
		}
		s.debugf(
			"ws frame received: agent_id=%s conn_id=%s frame_type=%s bytes=%d",
			client.AgentID,
			client.ConnID,
			wsMessageTypeName(msgType),
			len(data),
		)
		if msgType != websocket.TextMessage {
			continue
		}

		var msg WSMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			log.Printf("decode ws message failed: agent=%s conn_id=%s err=%v", client.AgentID, client.ConnID, err)
			continue
		}
		s.debugf(
			"ws message decoded: agent_id=%s conn_id=%s type=%s id=%s corr=%s ts=%d payload_bytes=%d",
			client.AgentID,
			client.ConnID,
			msg.Type,
			msg.ID,
			msg.CorrelationID,
			msg.Timestamp,
			len(msg.Payload),
		)
		s.handleInboundWSMessage(client, msg)
	}
}

func (s *TestServer) handleWSDebug(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"error": "method not allowed"})
		return
	}

	token, source := extractTokenFromRequest(r)
	claims, err := s.parseToken(token)
	resp := map[string]interface{}{
		"ok":           err == nil,
		"token_source": source,
		"token_masked": maskToken(token),
		"path":         r.URL.RequestURI(),
	}
	if err != nil {
		resp["error"] = err.Error()
		writeJSON(w, http.StatusUnauthorized, resp)
		return
	}
	resp["claims"] = map[string]interface{}{
		"user_id":     claims.UserID,
		"agent_id":    claims.AgentID,
		"endpoint_id": claims.EndpointID,
		"purpose":     claims.Purpose,
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *TestServer) handleInboundWSMessage(client *AgentConnection, msg WSMessage) {
	switch msg.Type {
	case "handshake":
		log.Printf("handshake received: agent_id=%s conn_id=%s message_id=%s", client.AgentID, client.ConnID, msg.ID)
	case "ping":
		var ping PingPayload
		if len(msg.Payload) > 0 {
			_ = json.Unmarshal(msg.Payload, &ping)
		}
		s.debugf("ping received: agent_id=%s conn_id=%s seq=%d corr=%s", client.AgentID, client.ConnID, ping.Seq, msg.CorrelationID)
		if err := s.sendPong(client, msg.ID, ping.Seq); err != nil {
			log.Printf("send pong failed: agent=%s conn_id=%s err=%v", client.AgentID, client.ConnID, err)
		}
	case "response":
		var payload MessagePayload
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			log.Printf("decode response payload failed: agent=%s conn_id=%s err=%v", client.AgentID, client.ConnID, err)
			return
		}
		deviceID := extractString(payload.Metadata, "device_id")
		event := ResponseEvent{
			At:            time.Now(),
			AgentID:       client.AgentID,
			UserID:        client.UserID,
			EndpointID:    client.EndpointID,
			MessageID:     msg.ID,
			CorrelationID: msg.CorrelationID,
			DeviceID:      deviceID,
			Content:       payload.Content,
			SessionID:     payload.SessionID,
			Metadata:      payload.Metadata,
		}
		s.appendResponse(event)
		log.Printf(
			"response received: agent=%s conn_id=%s device=%s corr=%s session=%s content=%q",
			client.AgentID,
			client.ConnID,
			deviceID,
			msg.CorrelationID,
			payload.SessionID,
			payload.Content,
		)
	case "error":
		log.Printf("error message from agent=%s conn_id=%s payload=%s", client.AgentID, client.ConnID, string(msg.Payload))
	case "close":
		var payload struct {
			Code   int    `json:"code"`
			Reason string `json:"reason"`
		}
		if len(msg.Payload) > 0 {
			_ = json.Unmarshal(msg.Payload, &payload)
		}
		if payload.Code == 0 {
			payload.Code = websocket.CloseNormalClosure
		}
		if strings.TrimSpace(payload.Reason) == "" {
			payload.Reason = "peer requested close via protocol message"
		}
		s.closeConnection(
			client,
			payload.Code,
			payload.Reason,
			fmt.Sprintf("peer:close_message code=%d reason=%q message_id=%s", payload.Code, payload.Reason, msg.ID),
		)
	default:
		log.Printf("unknown message type from agent=%s conn_id=%s type=%s", client.AgentID, client.ConnID, msg.Type)
	}
}

func (s *TestServer) addConnection(next *AgentConnection) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if next == nil {
		return len(s.connections)
	}
	s.connections[next.ConnID] = next
	byAgent := s.agentConnections[next.AgentID]
	if byAgent == nil {
		byAgent = make(map[string]*AgentConnection)
		s.agentConnections[next.AgentID] = byAgent
	}
	byAgent[next.ConnID] = next
	s.debugf(
		"connection added: agent_id=%s conn_id=%s total_connections=%d agent_connections=%d",
		next.AgentID,
		next.ConnID,
		len(s.connections),
		len(byAgent),
	)
	return len(s.connections)
}

func (s *TestServer) removeConnection(conn *AgentConnection) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if conn == nil {
		return
	}
	current, ok := s.connections[conn.ConnID]
	if !ok {
		s.debugf("remove connection ignored: agent_id=%s conn_id=%s reason=not_found", conn.AgentID, conn.ConnID)
		return
	}
	if current == conn {
		delete(s.connections, conn.ConnID)
		if byAgent, ok := s.agentConnections[conn.AgentID]; ok {
			delete(byAgent, conn.ConnID)
			if len(byAgent) == 0 {
				delete(s.agentConnections, conn.AgentID)
			}
		}
		s.debugf("connection removed: agent_id=%s conn_id=%s remaining=%d", conn.AgentID, conn.ConnID, len(s.connections))
		return
	}
	s.debugf(
		"remove connection ignored: agent_id=%s conn_id=%s reason=stale_conn_mismatch=%s",
		conn.AgentID,
		conn.ConnID,
		current.ConnID,
	)
}

func (s *TestServer) getConnection(connID string) *AgentConnection {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.connections[connID]
}

func (s *TestServer) pickConnection(agentID string, connID string) (*AgentConnection, error) {
	agentID = strings.TrimSpace(agentID)
	connID = strings.TrimSpace(connID)

	s.mu.RLock()
	defer s.mu.RUnlock()

	if connID != "" {
		conn, ok := s.connections[connID]
		if !ok || conn == nil {
			return nil, fmt.Errorf("connection not found: %s", connID)
		}
		if agentID != "" && conn.AgentID != agentID {
			return nil, fmt.Errorf("connection %s does not belong to agent %s", connID, agentID)
		}
		return conn, nil
	}

	if agentID != "" {
		byAgent, ok := s.agentConnections[agentID]
		if !ok || len(byAgent) == 0 {
			return nil, fmt.Errorf("agent connection not found: %s", agentID)
		}
		var latest *AgentConnection
		for _, conn := range byAgent {
			if latest == nil || conn.Connected.After(latest.Connected) {
				latest = conn
			}
		}
		if latest == nil {
			return nil, fmt.Errorf("agent connection not found: %s", agentID)
		}
		return latest, nil
	}

	if len(s.connections) == 1 {
		for _, conn := range s.connections {
			return conn, nil
		}
	}
	if len(s.connections) == 0 {
		return nil, errors.New("no active agent connection")
	}
	return nil, errors.New("multiple active connections, provide agent_id or conn_id")
}

func (s *TestServer) appendResponse(event ResponseEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.responses = append(s.responses, event)
	if len(s.responses) > 1000 {
		s.responses = append([]ResponseEvent(nil), s.responses[len(s.responses)-1000:]...)
	}
}

func (s *TestServer) sendMessage(conn *AgentConnection, msg WSMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	s.debugf(
		"ws frame send: agent_id=%s conn_id=%s type=%s id=%s corr=%s bytes=%d",
		conn.AgentID,
		conn.ConnID,
		msg.Type,
		msg.ID,
		msg.CorrelationID,
		len(data),
	)
	conn.writeMu.Lock()
	defer conn.writeMu.Unlock()
	_ = conn.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return conn.conn.WriteMessage(websocket.TextMessage, data)
}

func (s *TestServer) sendHandshakeAck(conn *AgentConnection) error {
	payload, err := json.Marshal(map[string]interface{}{
		"version": "1.0.0",
		"server":  "test-openclaw-server",
	})
	if err != nil {
		return err
	}
	return s.sendMessage(conn, WSMessage{
		ID:        uuid.NewString(),
		Timestamp: time.Now().UnixMilli(),
		Type:      "handshake_ack",
		Payload:   payload,
	})
}

func (s *TestServer) sendPong(conn *AgentConnection, correlationID string, seq int) error {
	payload, err := json.Marshal(map[string]interface{}{"seq": seq})
	if err != nil {
		return err
	}
	return s.sendMessage(conn, WSMessage{
		ID:            uuid.NewString(),
		Timestamp:     time.Now().UnixMilli(),
		Type:          "pong",
		CorrelationID: correlationID,
		Payload:       payload,
	})
}

type SendRequest struct {
	AgentID   string                 `json:"agent_id"`
	ConnID    string                 `json:"conn_id"`
	DeviceID  string                 `json:"device_id"`
	Content   string                 `json:"content"`
	SessionID string                 `json:"session_id"`
	Metadata  map[string]interface{} `json:"metadata"`
}

func (s *TestServer) handleSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"error": "method not allowed"})
		return
	}

	var req SendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "invalid json: " + err.Error()})
		return
	}

	req.AgentID = strings.TrimSpace(req.AgentID)
	req.ConnID = strings.TrimSpace(req.ConnID)
	req.DeviceID = strings.TrimSpace(req.DeviceID)
	req.Content = strings.TrimSpace(req.Content)
	if req.Content == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "content is required"})
		return
	}
	if req.DeviceID == "" {
		req.DeviceID = extractString(req.Metadata, "device_id")
	}
	if req.DeviceID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "device_id is required"})
		return
	}

	conn, err := s.pickConnection(req.AgentID, req.ConnID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
		return
	}
	s.debugf(
		"http send request: agent_id=%s conn_id=%s target_conn=%s device_id=%s session_id=%s content_len=%d",
		conn.AgentID,
		req.ConnID,
		conn.ConnID,
		req.DeviceID,
		req.SessionID,
		len(req.Content),
	)

	metadata := cloneMap(req.Metadata)
	if metadata == nil {
		metadata = make(map[string]interface{})
	}
	metadata["device_id"] = req.DeviceID
	metadata["agent_id"] = conn.AgentID

	payload, err := json.Marshal(MessagePayload{
		Content:   req.Content,
		SessionID: strings.TrimSpace(req.SessionID),
		Metadata:  metadata,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "marshal payload failed: " + err.Error()})
		return
	}

	msgID := uuid.NewString()
	msg := WSMessage{
		ID:        msgID,
		Timestamp: time.Now().UnixMilli(),
		Type:      "message",
		Payload:   payload,
	}

	if err := s.sendMessage(conn, msg); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "send message failed: " + err.Error()})
		return
	}
	log.Printf(
		"message pushed to agent: agent_id=%s conn_id=%s message_id=%s device_id=%s session_id=%s",
		conn.AgentID,
		conn.ConnID,
		msgID,
		req.DeviceID,
		req.SessionID,
	)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":           true,
		"agent_id":     conn.AgentID,
		"conn_id":      conn.ConnID,
		"message_id":   msgID,
		"device_id":    req.DeviceID,
		"session_id":   req.SessionID,
		"sent_at_unix": time.Now().UnixMilli(),
	})
}

func (s *TestServer) handleConnections(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"error": "method not allowed"})
		return
	}

	s.mu.RLock()
	items := make([]map[string]interface{}, 0, len(s.connections))
	for _, conn := range s.connections {
		items = append(items, map[string]interface{}{
			"agent_id":     conn.AgentID,
			"user_id":      conn.UserID,
			"endpoint_id":  conn.EndpointID,
			"conn_id":      conn.ConnID,
			"remote_addr":  conn.RemoteAddr,
			"connected_at": conn.Connected.Format(time.RFC3339),
		})
	}
	s.mu.RUnlock()

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"count":       len(items),
		"connections": items,
	})
}

func (s *TestServer) handleResponses(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"error": "method not allowed"})
		return
	}

	limit := 20
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "limit must be positive integer"})
			return
		}
		if value > 200 {
			value = 200
		}
		limit = value
	}
	agentID := strings.TrimSpace(r.URL.Query().Get("agent_id"))

	s.mu.RLock()
	filtered := make([]ResponseEvent, 0, len(s.responses))
	for i := len(s.responses) - 1; i >= 0; i-- {
		event := s.responses[i]
		if agentID != "" && event.AgentID != agentID {
			continue
		}
		filtered = append(filtered, event)
		if len(filtered) >= limit {
			break
		}
	}
	s.mu.RUnlock()

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"count":     len(filtered),
		"responses": filtered,
	})
}

func (s *TestServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"error": "method not allowed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":   true,
		"name": "test_openclaw_server",
		"ws":   "/ws/openclaw",
	})
}

func writeJSON(w http.ResponseWriter, code int, data map[string]interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(data)
}

func extractString(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	value, ok := m[key]
	if !ok {
		return ""
	}
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case json.Number:
		return v.String()
	case float64:
		return strconv.FormatInt(int64(v), 10)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	default:
		return ""
	}
}

func cloneMap(src map[string]interface{}) map[string]interface{} {
	if src == nil {
		return nil
	}
	dst := make(map[string]interface{}, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func main() {
	addr := flag.String("addr", ":18080", "listen address")
	secret := flag.String("jwt-secret", "xiaozhi_admin_secret_key", "JWT secret")
	verbose := flag.Bool("verbose", false, "enable verbose websocket logs")
	flag.Parse()

	server := NewTestServer(*secret, *verbose)

	mux := http.NewServeMux()
	mux.HandleFunc("/ws/openclaw", server.handleWSOpenClaw)
	mux.HandleFunc("/debug/ws-auth", server.handleWSDebug)
	mux.HandleFunc("/api/send", server.handleSend)
	mux.HandleFunc("/api/connections", server.handleConnections)
	mux.HandleFunc("/api/responses", server.handleResponses)
	mux.HandleFunc("/healthz", server.handleHealth)

	httpServer := &http.Server{
		Addr:              *addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Printf("test_openclaw_server started on %s", *addr)
	log.Printf("ws endpoint: ws://127.0.0.1%s/ws/openclaw?token=...", *addr)
	log.Printf("http apis: /healthz /api/connections /api/send /api/responses")
	log.Printf("verbose logging: %v", *verbose)

	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, context.Canceled) {
		log.Fatalf("server exit with error: %v", err)
	}
}
