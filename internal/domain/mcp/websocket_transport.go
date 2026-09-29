package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"

	log "xiaozhi-esp32-server-golang/logger"
)

const (
	// DefaultRequestTimeout default request timeout
	DefaultRequestTimeout = 30 * time.Second
	// DefaultCloseTimeout default close timeout
	DefaultCloseTimeout = 5 * time.Second
)

type pendingResponseResult struct {
	response *transport.JSONRPCResponse
	err      error
}

type pendingResponse struct {
	resultCh chan pendingResponseResult
	once     sync.Once
}

func newPendingResponse() *pendingResponse {
	return &pendingResponse{
		resultCh: make(chan pendingResponseResult, 1),
	}
}

func (p *pendingResponse) resolve(response *transport.JSONRPCResponse, err error) {
	if p == nil {
		return
	}
	p.once.Do(func() {
		p.resultCh <- pendingResponseResult{
			response: response,
			err:      err,
		}
	})
}

type jsonRPCMessageEnvelope struct {
	Method string           `json:"method"`
	ID     *json.RawMessage `json:"id"`
}

func classifyJSONRPCMessage(message []byte) (method string, hasID bool, err error) {
	var envelope jsonRPCMessageEnvelope
	if err := json.Unmarshal(message, &envelope); err != nil {
		return "", false, err
	}
	return envelope.Method, envelope.ID != nil, nil
}

func requestIDKey(id mcp.RequestId) string {
	raw, err := id.MarshalJSON()
	if err == nil {
		return string(raw)
	}
	return id.String()
}

func isTransportTimeoutErr(err error) bool {
	if err == nil {
		return false
	}
	lowerErr := strings.ToLower(err.Error())
	return strings.Contains(lowerErr, "timeout") || strings.Contains(err.Error(), "超时")
}

/**
// Interface for the transport layer.
type Interface interface {
	// Start the connection. Start should only be called once.
	Start(ctx context.Context) error

	// SendRequest sends a json RPC request and returns the response synchronously.
	SendRequest(ctx context.Context, request JSONRPCRequest) (*JSONRPCResponse, error)

	// SendNotification sends a json RPC Notification to the server.
	SendNotification(ctx context.Context, notification mcp.JSONRPCNotification) error

	// SetNotificationHandler sets the handler for notifications.
	// Any notification before the handler is set will be discarded.
	SetNotificationHandler(handler func(notification mcp.JSONRPCNotification))

	// Close the connection.
	Close() error
}
*/

type WebsocketTransport struct {
	url  string
	conn *websocket.Conn

	notifyHandler func(notification mcp.JSONRPCNotification)
	// add close callback
	onCloseHandler func(reason string)

	// response channel management
	respChans    map[string]*pendingResponse
	respChansMux sync.RWMutex

	// message listener control
	readDone chan struct{}
	ctx      context.Context
	cancel   context.CancelFunc

	// connection state
	closed    bool
	closedMux sync.RWMutex

	// WebSocket write lock to prevent concurrent writes
	writeMux sync.Mutex

	// timeout config
	requestTimeout time.Duration
	closeTimeout   time.Duration
}

func (t *WebsocketTransport) Send(ctx context.Context, msg []byte) error {
	// check connection state
	t.closedMux.RLock()
	if t.closed {
		t.closedMux.RUnlock()
		return fmt.Errorf("connection is closed")
	}
	t.closedMux.RUnlock()

	// send message (mutex-protected write)
	t.writeMux.Lock()
	err := t.conn.WriteMessage(websocket.TextMessage, msg)
	t.writeMux.Unlock()
	return err
}

func NewWebsocketTransport(conn *websocket.Conn) (*WebsocketTransport, error) {
	ctx, cancel := context.WithCancel(context.Background())

	wst := &WebsocketTransport{
		conn:           conn,
		respChans:      make(map[string]*pendingResponse),
		readDone:       make(chan struct{}),
		ctx:            ctx,
		cancel:         cancel,
		requestTimeout: DefaultRequestTimeout,
		closeTimeout:   DefaultCloseTimeout,
	}
	// start the message listener goroutine
	go wst.readMessages()

	return wst, nil
}

// implements Interface
func (t *WebsocketTransport) Start(ctx context.Context) error {
	return nil
}

func (t *WebsocketTransport) popPending(id string) *pendingResponse {
	t.respChansMux.Lock()
	defer t.respChansMux.Unlock()

	pending := t.respChans[id]
	if pending != nil {
		delete(t.respChans, id)
	}
	return pending
}

func (t *WebsocketTransport) failAllPending(err error) {
	t.respChansMux.Lock()
	pending := make([]*pendingResponse, 0, len(t.respChans))
	for id, pendingResp := range t.respChans {
		pending = append(pending, pendingResp)
		delete(t.respChans, id)
	}
	t.respChansMux.Unlock()

	for _, pendingResp := range pending {
		pendingResp.resolve(nil, err)
	}
}

// readMessages continuously listens for WebSocket messages
func (t *WebsocketTransport) readMessages() {
	defer close(t.readDone)

	for {
		select {
		case <-t.ctx.Done():
			return
		default:
			// use Go-level timeout control
			_, message, err := t.conn.ReadMessage()
			if err != nil {
				t.closedMux.Lock()
				t.closed = true
				t.closedMux.Unlock()
				t.failAllPending(fmt.Errorf("connection is closed"))

				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					log.Errorf("WebSocket read error: %v", err)
				}

				// notify the client layer when the connection closes
				if t.onCloseHandler != nil {
					reason := "connection_closed"
					if websocket.IsCloseError(err, websocket.CloseNormalClosure) {
						reason = "normal_closure"
					} else if websocket.IsUnexpectedCloseError(err) {
						reason = "unexpected_closure"
					}
					t.onCloseHandler(reason)
				}

				return
			}

			// handle received messages
			t.handleMessage(message)
		}
	}
}

// handleMessage handle received messages
func (t *WebsocketTransport) handleMessage(message []byte) {
	method, hasID, err := classifyJSONRPCMessage(message)
	if err != nil {
		log.Warnf("Received unrecognized message: %s", string(message))
		return
	}

	if method != "" {
		if hasID {
			log.Warnf("Received unsupported JSON-RPC request: %s", method)
			return
		}

		var notification mcp.JSONRPCNotification
		if err := json.Unmarshal(message, &notification); err != nil {
			log.Warnf("Received malformed JSON-RPC notification: %s", string(message))
			return
		}
		t.handleNotification(&notification)
		return
	}

	if hasID {
		var response transport.JSONRPCResponse
		if err := json.Unmarshal(message, &response); err != nil {
			log.Warnf("Received malformed JSON-RPC response: %s", string(message))
			return
		}
		t.handleResponse(&response)
		return
	}

	// unrecognized message format
	log.Warnf("Received unrecognized message: %s", string(message))
}

// handleResponse handles a JSON-RPC response
func (t *WebsocketTransport) handleResponse(response *transport.JSONRPCResponse) {
	respByte, _ := json.Marshal(response)
	// convert ID to string for use as a key
	idStr := requestIDKey(response.ID)

	pending := t.popPending(idStr)
	if pending == nil {
		log.Warnf("No response channel found for ID: %s, response: %+v", idStr, string(respByte))
		return
	}
	pending.resolve(response, nil)
}

// handleNotification handles a JSON-RPC notification
func (t *WebsocketTransport) handleNotification(notification *mcp.JSONRPCNotification) {
	if t.notifyHandler != nil {
		t.notifyHandler(*notification)
	}
}

func (t *WebsocketTransport) SendRequest(ctx context.Context, request transport.JSONRPCRequest) (*transport.JSONRPCResponse, error) {
	// check connection state
	t.closedMux.RLock()
	if t.closed {
		t.closedMux.RUnlock()
		return nil, fmt.Errorf("connection is closed")
	}
	t.closedMux.RUnlock()

	// create the response channel
	idStr := requestIDKey(request.ID)
	pending := newPendingResponse()

	// register the response channel
	t.respChansMux.Lock()
	t.respChans[idStr] = pending
	t.respChansMux.Unlock()

	// send request (mutex-protected write)
	t.writeMux.Lock()
	err := t.conn.WriteJSON(request)
	t.writeMux.Unlock()
	if err != nil {
		// send failed; clean up the channel
		t.popPending(idStr)
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	// wait for the response with a Go-level timeout
	select {
	case result := <-pending.resultCh:
		if result.err != nil {
			return nil, result.err
		}
		return result.response, nil
	case <-ctx.Done():
		// context canceled; clean up the channel
		t.popPending(idStr)
		return nil, ctx.Err()
	case <-time.After(t.requestTimeout):
		// Go-level timeout control
		t.popPending(idStr)
		return nil, fmt.Errorf("request timeout")
	}
}

func (t *WebsocketTransport) SendNotification(ctx context.Context, notification mcp.JSONRPCNotification) error {
	// check connection state
	t.closedMux.RLock()
	if t.closed {
		t.closedMux.RUnlock()
		return fmt.Errorf("connection is closed")
	}
	t.closedMux.RUnlock()

	// send notification (mutex-protected write)
	t.writeMux.Lock()
	err := t.conn.WriteJSON(notification)
	t.writeMux.Unlock()
	return err
}

func (t *WebsocketTransport) SetNotificationHandler(handler func(notification mcp.JSONRPCNotification)) {
	t.notifyHandler = handler
}

// SetOnCloseHandler sets the connection-close callback
func (t *WebsocketTransport) SetOnCloseHandler(handler func(reason string)) {
	t.onCloseHandler = handler
}

func (t *WebsocketTransport) Close() error {
	// mark the connection as closed
	t.closedMux.Lock()
	t.closed = true
	t.closedMux.Unlock()
	t.failAllPending(fmt.Errorf("connection is closed"))

	// notify the client layer that the connection is closing
	if t.onCloseHandler != nil {
		t.onCloseHandler("manual_close")
	}

	// cancel the context
	t.cancel()

	// wait for the reader goroutine to finish
	select {
	case <-t.readDone:
	case <-time.After(t.closeTimeout):
		log.Warnf("Timeout waiting for read goroutine to finish")
	}

	// close the WebSocket connection
	return t.conn.Close()
}

func (t *WebsocketTransport) GetSessionId() string {
	return t.conn.RemoteAddr().String()
}

// IsClosed reports whether the connection is closed
func (t *WebsocketTransport) IsClosed() bool {
	t.closedMux.RLock()
	defer t.closedMux.RUnlock()
	return t.closed
}

// GetActiveRequests returns the number of active requests
func (t *WebsocketTransport) GetActiveRequests() int {
	t.respChansMux.RLock()
	defer t.respChansMux.RUnlock()
	return len(t.respChans)
}

// SetRequestTimeout sets the request timeout
func (t *WebsocketTransport) SetRequestTimeout(timeout time.Duration) {
	t.requestTimeout = timeout
}

// SetCloseTimeout sets the close timeout
func (t *WebsocketTransport) SetCloseTimeout(timeout time.Duration) {
	t.closeTimeout = timeout
}

// GetRequestTimeout returns the current request timeout
func (t *WebsocketTransport) GetRequestTimeout() time.Duration {
	return t.requestTimeout
}

// GetCloseTimeout returns the current close timeout
func (t *WebsocketTransport) GetCloseTimeout() time.Duration {
	return t.closeTimeout
}
