package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
	"xiaozhi-esp32-server-golang/internal/app/mqtt_server"
	"xiaozhi-esp32-server-golang/internal/app/server/chat"
	"xiaozhi-esp32-server-golang/internal/app/server/mqtt_udp"
	"xiaozhi-esp32-server-golang/internal/app/server/types"
	"xiaozhi-esp32-server-golang/internal/app/server/websocket"
	user_config "xiaozhi-esp32-server-golang/internal/domain/config"
	"xiaozhi-esp32-server-golang/internal/domain/config/pocketbase"
	"xiaozhi-esp32-server-golang/internal/domain/config/providertest"
	config_types "xiaozhi-esp32-server-golang/internal/domain/config/types"
	"xiaozhi-esp32-server-golang/internal/domain/mcp"
	"xiaozhi-esp32-server-golang/internal/domain/openclaw"
	"xiaozhi-esp32-server-golang/internal/pool"
	log "xiaozhi-esp32-server-golang/logger"

	cmap "github.com/orcaman/concurrent-map/v2"
	"github.com/spf13/viper"
)

// App manages all protocol services and ChatManagers

type App struct {
	wsServer       *websocket.WebSocketServer
	mqttUdpAdapter *mqtt_udp.MqttUdpAdapter
	mqttUdpMu      sync.RWMutex

	// ChatManager map - concurrent map
	chatManagers cmap.ConcurrentMap[string, *chat.ChatManager]
}

func NewApp() *App {
	var err error
	app := &App{
		chatManagers: cmap.New[*chat.ChatManager](),
	}
	mcp.RegisterCurrentDeviceTransportResolver(func(deviceID string) string {
		chatManager, exists := app.GetChatManager(deviceID)
		if !exists || chatManager == nil {
			return ""
		}
		return chatManager.GetTransportType()
	})
	app.wsServer = app.newWebSocketServer()
	app.mqttUdpAdapter, err = app.newMqttUdpAdapter()
	if err != nil {
		log.Errorf("newMqttUdpAdapter err: %+v", err)
		return nil
	}
	return app
}

func (a *App) Run() {
	go a.wsServer.Start()
	log.Infof("enter Run, mqtt_server.enable: %v", viper.GetBool("mqtt_server.enable"))
	if viper.GetBool("mqtt_server.enable") {
		go func() {
			err := a.startMqttServer()
			if err != nil {
				log.Errorf("startMqttServer err: %+v", err)
			}
		}()
	}
	a.mqttUdpMu.RLock()
	adapter := a.mqttUdpAdapter
	a.mqttUdpMu.RUnlock()
	if adapter != nil {
		go adapter.Start() // Non-blocking; connect/retry runs inside the adapter
	}

	// Register chat-related local MCP tools
	a.registerChatMCPTools()

	a.registerHandler()

	a.initEventHandle()

	// Start pool stats monitor (log every 5 minutes)
	ctx := context.Background()
	pool.StartStatsMonitor(ctx, 5*time.Minute)

	// Start pool stats reporter (every 5s)
	pool.StartStatsReporter(ctx)

	select {} // Block the main goroutine
}

func (app *App) initEventHandle() {
	eventHandle, err := NewEventHandle(app)
	if err != nil {
		log.Errorf("Failed to init EventHandle: %v", err)
		return
	}
	if err := eventHandle.Start(); err != nil {
		log.Errorf("Failed to start EventHandle: %v", err)
		return
	}

	// Init message worker (always on; Redis short memory and long-term memory provider)
	NewMessageWorker()
	log.Info("Message handler initialized")
}

func (app *App) currentMqttConfig() *mqtt_udp.MqttConfig {
	if !viper.GetBool("mqtt.enable") {
		return nil
	}
	return &mqtt_udp.MqttConfig{
		Broker:   viper.GetString("mqtt.broker"),
		Type:     viper.GetString("mqtt.type"),
		Port:     viper.GetInt("mqtt.port"),
		ClientID: viper.GetString("mqtt.client_id"),
		Username: viper.GetString("mqtt.username"),
		Password: viper.GetString("mqtt.password"),
	}
}

func (app *App) newMqttUdpAdapter() (*mqtt_udp.MqttUdpAdapter, error) {
	mqttConfig := app.currentMqttConfig()
	if mqttConfig == nil {
		return nil, nil
	}

	udpServer, err := app.newUdpServer()
	if err != nil {
		return nil, err
	}

	return mqtt_udp.NewMqttUdpAdapter(
		mqttConfig,
		mqtt_udp.WithUdpServer(udpServer),
		mqtt_udp.WithOnNewConnection(app.OnNewConnection),
		mqtt_udp.WithOnDeviceOnline(app.DeviceOnline),
		mqtt_udp.WithOnDeviceOffline(app.DeviceOffline),
		mqtt_udp.WithOnTransportReady(app.onMqttTransportReady),
		mqtt_udp.WithOfflineGracePeriod(app.mqttOfflineGracePeriod()),
	), nil
}

func (app *App) mqttOfflineGracePeriod() time.Duration {
	if duration := viper.GetDuration("mqtt.transport_offline_grace_period"); duration > 0 {
		return duration
	}
	if seconds := viper.GetInt("mqtt.transport_offline_grace_period_seconds"); seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	return 2 * time.Minute
}

func (app *App) newUdpServer() (*mqtt_udp.UdpServer, error) {
	udpPort := viper.GetInt("udp.listen_port")
	externalHost := viper.GetString("udp.external_host")
	externalPort := viper.GetInt("udp.external_port")

	udpServer := mqtt_udp.NewUDPServer(udpPort, externalHost, externalPort)
	err := udpServer.Start()
	if err != nil {
		log.Fatalf("udpServer.Start err: %+v", err)
		return nil, err
	}
	return udpServer, nil
}

func (app *App) newWebSocketServer() *websocket.WebSocketServer {
	port := viper.GetInt("websocket.port")
	return websocket.NewWebSocketServer(
		port,
		websocket.WithOnNewConnection(app.OnNewConnection),
		websocket.WithOnOpenClawResponse(app.OnOpenClawResponse),
	)
}

func (app *App) startMqttServer() error {
	return mqtt_server.StartMqttServer()
}

// ReloadMqttServer hot-reload MQTT Server: stop first, then start only if mqtt_server.enable
func (app *App) ReloadMqttServer() {
	_ = mqtt_server.StopMqttServer()
	if !viper.GetBool("mqtt_server.enable") {
		return
	}
	if err := app.startMqttServer(); err != nil {
		log.Errorf("ReloadMqttServer start: %v", err)
	}
}

// ReloadMqttUdp hot-reload MQTT+UDP: stop old adapter, then start only if mqtt.enable
func (app *App) ReloadMqttUdp() {
	app.mqttUdpMu.Lock()
	old := app.mqttUdpAdapter
	app.mqttUdpAdapter = nil
	app.mqttUdpMu.Unlock()
	if old != nil {
		old.Stop()
	}
	if !viper.GetBool("mqtt.enable") {
		return
	}
	adapter, err := app.newMqttUdpAdapter()
	if err != nil {
		log.Errorf("ReloadMqttUdp newMqttUdpAdapter: %v", err)
		return
	}
	app.mqttUdpMu.Lock()
	app.mqttUdpAdapter = adapter
	app.mqttUdpMu.Unlock()
	time.Sleep(500 * time.Millisecond)
	go adapter.Start()
}

// ReloadMqttUdpWithFlags hot-reload MQTT+UDP based on change flags
func (app *App) ReloadMqttUdpWithFlags(doMqttReload, doUdpReload bool) {
	if !doMqttReload && !doUdpReload {
		return
	}
	if !viper.GetBool("mqtt.enable") {
		log.Infof("ReloadMqttUdpWithFlags: mqtt disabled, stopping mqtt+udp")
		app.ReloadMqttUdp()
		return
	}

	app.mqttUdpMu.RLock()
	adapter := app.mqttUdpAdapter
	app.mqttUdpMu.RUnlock()

	if adapter == nil {
		log.Infof("ReloadMqttUdpWithFlags: mqtt enabled but adapter is nil, starting mqtt+udp")
		newAdapter, err := app.newMqttUdpAdapter()
		if err != nil {
			log.Errorf("ReloadMqttUdpWithFlags newMqttUdpAdapter: %v", err)
			return
		}
		if newAdapter == nil {
			return
		}
		app.mqttUdpMu.Lock()
		app.mqttUdpAdapter = newAdapter
		app.mqttUdpMu.Unlock()
		time.Sleep(500 * time.Millisecond)
		go newAdapter.Start()
		return
	}

	if doMqttReload && doUdpReload {
		log.Infof("ReloadMqttUdpWithFlags: mqtt & udp config changed, reloading mqtt+udp")
		app.ReloadMqttUdp()
		return
	}
	if doMqttReload {
		log.Infof("ReloadMqttUdpWithFlags: mqtt config changed, reloading mqtt only")
		mqttConfig := app.currentMqttConfig()
		if mqttConfig == nil {
			app.ReloadMqttUdp()
			return
		}
		adapter.ReloadMqttClient(mqttConfig)
		return
	}
	if doUdpReload {
		log.Infof("ReloadMqttUdpWithFlags: udp listen changed, reloading udp only")
		udpServer, err := app.newUdpServer()
		if err != nil {
			log.Errorf("ReloadMqttUdpWithFlags newUdpServer: %v", err)
			return
		}
		adapter.ReloadUdpServer(udpServer)
	}
}

// ReloadMCP hot-reload MCP: if disabled stop global MCP; if enabled restart or start the cluster
func (app *App) ReloadMCP() error {
	if !viper.GetBool("mcp.global.enabled") {
		// Disabled: stop only; avoid relying on Start() checks or merge timing
		if err := mcp.GetGlobalMCPManager().Stop(); err != nil {
			return err
		}
		return nil
	}
	mgr := mcp.GetMCPManager()
	if mgr.IsStarted() {
		if err := mgr.RestartManager("global"); err != nil {
			return err
		}
		return nil
	}
	if err := mcp.StartMCPManagers(); err != nil {
		return err
	}
	return nil
}

// All new protocol connections enter here
func (a *App) OnNewConnection(transport types.IConn) {
	deviceID := transport.GetDeviceID()
	transportType := transport.GetTransportType()
	notifyLifecycleOnManager := transportType != types.TransportTypeMqttUdp

	// Check for an existing ChatManager for the device
	if existingManager, exists := a.chatManagers.Get(deviceID); exists {
		log.Infof("Device %s already has ChatManager, closing old connection first", deviceID)
		// Close the old ChatManager
		existingManager.Close()
		a.chatManagers.Remove(deviceID)
	}

	// Create a new ChatManager
	chatManager, err := chat.NewChatManager(deviceID, transport)
	if err != nil {
		log.Errorf("Failed to create chatManager: %v", err)
		return
	}

	// Store ChatManager
	a.chatManagers.Set(deviceID, chatManager)

	if notifyLifecycleOnManager {
		a.DeviceOnline(deviceID)
	}

	log.Infof("Device %s ChatManager created and stored", deviceID)

	// OpenClaw offline message replay (delayed retry until session is ready)
	go a.replayOpenClawOfflineMessages(deviceID)

	// Start ChatManager
	go func() {
		defer func() {
			// Remove from map when ChatManager ends
			if storedManager, exists := a.chatManagers.Get(deviceID); exists && storedManager == chatManager {
				a.chatManagers.Remove(deviceID)
				log.Infof("Device %s ChatManager removed from map", deviceID)
				if notifyLifecycleOnManager {
					a.DeviceOffline(deviceID)
				}
			}
		}()

		if err := chatManager.Start(); err != nil {
			log.Errorf("ChatManager start failed: %v", err)
		}
	}()
}

func (a *App) onMqttTransportReady(deviceID string) {
	chatManager, exists := a.GetChatManager(deviceID)
	if !exists || chatManager == nil {
		return
	}
	chatManager.HandleMqttTransportReady()
	chatManager.WarmupMcp()
}

// OnOpenClawResponse OpenClaw realtime response callback
func (a *App) OnOpenClawResponse(event openclaw.ResponseDelivery) bool {
	deviceID := strings.TrimSpace(event.DeviceID)
	if deviceID == "" {
		return false
	}
	chatManager, exists := a.GetChatManager(deviceID)
	if !exists || chatManager == nil {
		return false
	}
	if err := chatManager.InjectOpenClawResponse(event); err != nil {
		log.Warnf(
			"OpenClaw realtime message inject failed, device=%s correlation_id=%s start=%v end=%v err=%v",
			deviceID,
			strings.TrimSpace(event.CorrelationID),
			event.IsStart,
			event.IsEnd,
			err,
		)
		return false
	}
	return true
}

func (a *App) replayOpenClawOfflineMessages(deviceID string) {
	manager := openclaw.GetManager()
	const maxRetry = 10
	for i := 0; i < maxRetry; i++ {
		time.Sleep(1 * time.Second)
		delivered, remaining := manager.ReplayOfflineMessages(deviceID, func(msg openclaw.OfflineMessage) error {
			chatManager, exists := a.GetChatManager(deviceID)
			if !exists || chatManager == nil {
				return fmt.Errorf("chat manager not ready")
			}
			if strings.TrimSpace(msg.Text) == "" {
				return nil
			}
			return chatManager.InjectMessage(msg.Text, true, false)
		})
		if delivered > 0 {
			log.Infof("OpenClaw offline message replay succeeded, device=%s delivered=%d remaining=%d", deviceID, delivered, remaining)
		}
		if remaining == 0 {
			return
		}
	}
}

// GetChatManager gets ChatManager for a device
func (a *App) GetChatManager(deviceID string) (*chat.ChatManager, bool) {
	return a.chatManagers.Get(deviceID)
}

// CloseChatManager closes ChatManager for a device
func (a *App) CloseChatManager(deviceID string) bool {
	if manager, exists := a.chatManagers.Get(deviceID); exists {
		manager.Close()
		a.chatManagers.Remove(deviceID)
		log.Infof("Device %s ChatManager closed and removed", deviceID)
		return true
	}
	return false
}

// GetAllChatManagers returns a copy of all ChatManagers
func (a *App) GetAllChatManagers() map[string]*chat.ChatManager {
	// Return a copy to avoid concurrent access issues
	managers := make(map[string]*chat.ChatManager)
	for tuple := range a.chatManagers.IterBuffered() {
		managers[tuple.Key] = tuple.Val
	}
	return managers
}

// GetChatManagerCount returns active ChatManager count
func (a *App) GetChatManagerCount() int {
	return a.chatManagers.Count()
}

// CloseAllChatManagers closes all ChatManagers
func (a *App) CloseAllChatManagers() {
	for tuple := range a.chatManagers.IterBuffered() {
		tuple.Val.Close()
		log.Infof("Device %s ChatManager closed", tuple.Key)
	}

	// Clear the map
	a.chatManagers.Clear()
	log.Info("All ChatManagers closed")
}

// registerChatMCPTools Register chat-related local MCP tools
func (s *App) registerChatMCPTools() {
	// Call the chat package register helper
	chat.RegisterChatMCPTools()

	log.Info("Chat-related local MCP tools registration done")
}

func (s *App) DeviceOnline(deviceID string) {
	eventData := map[string]interface{}{
		"device_id": deviceID,
	}
	providerType := viper.GetString("config_provider.type")
	provider, err := user_config.GetProvider(providerType)
	if err != nil {
		log.Errorf("GetProvider err: %+v", err)
		return
	}
	provider.NotifyDeviceEvent(context.Background(), config_types.EventDeviceOnline, eventData)
}

func (s *App) DeviceOffline(deviceID string) {
	eventData := map[string]interface{}{
		"device_id": deviceID,
	}
	providerType := viper.GetString("config_provider.type")
	provider, err := user_config.GetProvider(providerType)
	if err != nil {
		log.Errorf("GetProvider err: %+v", err)
		return
	}
	provider.NotifyDeviceEvent(context.Background(), config_types.EventDeviceOffline, eventData)
}

func (a *App) registerHandler() {
	providerType := viper.GetString("config_provider.type")
	log.Infof("registerHandler: config_provider.type=%s", providerType)
	provider, err := user_config.GetProvider(providerType)
	if err != nil {
		log.Errorf("GetProvider err: %+v", err)
		return
	}
	provider.RegisterMessageEventHandler(context.Background(), config_types.EventHandleMessageInject, a.HandleInjectMsg)
	log.Infof("registerHandler: registered paths=[%s]", config_types.EventHandleMessageInject)

	pb, ok := provider.(*pocketbase.Provider)
	if !ok {
		return
	}
	pb.RegisterCommand("inject_msg", a.injectCommand)
	pb.RegisterCommand("provider_test", func(ctx context.Context, payload map[string]any) (any, error) {
		return providertest.FromCommand(ctx, payload)
	})
	pb.ArmCommands()
}

// injectCommand speaks the text, or feeds it into the chat, through the same
// path as a live session. On MQTT that path sends speak_request before the audio.
func (a *App) injectCommand(ctx context.Context, payload map[string]any) (any, error) {
	_, err := a.HandleInjectMsg(ctx, config_types.EventHandleMessageInject, map[string]interface{}{
		"device_id":   payload["device"],
		"message":     payload["message"],
		"skip_llm":    payload["skip_llm"],
		"auto_listen": payload["auto_listen"],
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"ok": true}, nil
}

// Inject a message to the client
func (a *App) HandleInjectMsg(ctx context.Context, eventType string, eventData map[string]interface{}) (string, error) {
	type InjectMsg struct {
		SkipLlm    bool   `json:"skip_llm"`
		AutoListen *bool  `json:"auto_listen"`
		DeviceId   string `json:"device_id"`
		Message    string `json:"message"`
	}
	bodyBytes, _ := json.Marshal(eventData)
	var msg InjectMsg
	err := json.Unmarshal(bodyBytes, &msg)
	if err != nil {
		log.Errorf("HandleInjectMsg error: %+v", err)
		return "", fmt.Errorf("HandleInjectMsg error")
	}

	// Validate required params
	if msg.DeviceId == "" {
		log.Errorf("HandleInjectMsg: device_id is required")
		return "", fmt.Errorf("device_id is required")
	}
	if msg.Message == "" {
		log.Errorf("HandleInjectMsg: message is required")
		return "", fmt.Errorf("message is required")
	}

	// gets ChatManager for a device
	chatManager, exists := a.GetChatManager(msg.DeviceId)
	if !exists {
		log.Errorf("HandleInjectMsg: device %s not found or offline", msg.DeviceId)
		return "", fmt.Errorf("device %s not found or offline", msg.DeviceId)
	}

	autoListen := true
	if msg.AutoListen != nil {
		autoListen = *msg.AutoListen
	}

	log.Debugf("HandleInjectMsg: injecting message to device %s, skip_llm: %v, auto_listen: %v, message: %s",
		msg.DeviceId, msg.SkipLlm, autoListen, msg.Message)

	// Inject via ChatManager's public method
	err = chatManager.InjectMessage(msg.Message, msg.SkipLlm, autoListen)
	if err != nil {
		log.Errorf("HandleInjectMsg: failed to inject message to device %s: %v", msg.DeviceId, err)
		return "", fmt.Errorf("failed to inject message: %v", err)
	}

	return "message injected successfully", nil
}
