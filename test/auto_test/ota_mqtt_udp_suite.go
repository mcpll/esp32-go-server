package main

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	neturl "net/url"
	"strings"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

type otaResponse struct {
	Mqtt      *otaMqttInfo `json:"mqtt,omitempty"`
	Websocket struct {
		URL   string `json:"url"`
		Token string `json:"token"`
	} `json:"websocket"`
	ServerTime struct {
		Timestamp      int64 `json:"timestamp"`
		TimezoneOffset int   `json:"timezone_offset"`
	} `json:"server_time"`
	Firmware struct {
		Version string `json:"version"`
		URL     string `json:"url"`
	} `json:"firmware"`
	Activation *otaActivationInfo `json:"activation,omitempty"`
}

type otaMqttInfo struct {
	Endpoint       string `json:"endpoint"`
	ClientID       string `json:"client_id"`
	Username       string `json:"username"`
	Password       string `json:"password"`
	PublishTopic   string `json:"publish_topic"`
	SubscribeTopic string `json:"subscribe_topic"`
}

type otaActivationInfo struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Challenge string `json:"challenge"`
	TimeoutMs int    `json:"timeout_ms"`
}

type otaActivationPayload struct {
	Algorithm    string `json:"algorithm"`
	SerialNumber string `json:"serial_number"`
	Challenge    string `json:"challenge"`
	HMAC         string `json:"hmac"`
}

type otaActivationRequest struct {
	Payload otaActivationPayload `json:"Payload"`
}

type mqttProtocolRuntime struct {
	client       mqtt.Client
	deviceID     string
	publishTopic string
	udpClient    *autoUDPClient
	sessionID    string

	eventMu sync.Mutex
	events  []protocolEvent

	helloAckCh     chan ServerMessage
	speakRequestCh chan ServerMessage
	ttsStartCh     chan struct{}
	ttsStopCh      chan struct{}
	sttCh          chan ServerMessage
	outputCh       chan ServerMessage
	iotCh          chan ServerMessage
	goodbyeCh      chan ServerMessage
	udpAudioCh     chan []byte
	mcpCh          chan protocolEvent
	mcpSendMsgChan chan []byte
	mcpRecvMsgChan chan []byte
}

type autoUDPClient struct {
	conn     *net.UDPConn
	aesKey   string
	aesNonce string
	localSeq uint32
}

func runOTAMetadataCase(serverAddr, deviceID string, _ *protocolTestCase) error {
	resp, err := requestOTAConfig(serverAddr, deviceID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(resp.Websocket.URL) == "" {
		return fmt.Errorf("OTA websocket.url 为空")
	}
	if resp.ServerTime.Timestamp <= 0 {
		return fmt.Errorf("OTA server_time.timestamp 非法: %d", resp.ServerTime.Timestamp)
	}
	if strings.TrimSpace(resp.Firmware.Version) == "" {
		return fmt.Errorf("OTA firmware.version 为空")
	}
	if resp.Mqtt != nil {
		if strings.TrimSpace(resp.Mqtt.Endpoint) == "" {
			return fmt.Errorf("OTA mqtt.endpoint 为空")
		}
		if strings.TrimSpace(resp.Mqtt.ClientID) == "" {
			return fmt.Errorf("OTA mqtt.client_id 为空")
		}
		if strings.TrimSpace(resp.Mqtt.PublishTopic) == "" {
			return fmt.Errorf("OTA mqtt.publish_topic 为空")
		}
	}
	if resp.Activation != nil {
		if strings.TrimSpace(resp.Activation.Code) == "" {
			return fmt.Errorf("OTA activation.code 为空")
		}
		if strings.TrimSpace(resp.Activation.Challenge) == "" {
			return fmt.Errorf("OTA activation.challenge 为空")
		}
		if strings.TrimSpace(resp.Activation.Message) == "" {
			return fmt.Errorf("OTA activation.message 为空")
		}
	}
	return nil
}

func runOTAInvalidAlgorithmCase(serverAddr, deviceID string, _ *protocolTestCase) error {
	statusCode, body, err := requestOTAActivate(serverAddr, deviceID, otaActivationPayload{
		Algorithm:    "invalid-algorithm",
		SerialNumber: "AUTO-TEST-SN",
		Challenge:    "invalid-challenge",
		HMAC:         "invalid-hmac",
	})
	if err != nil {
		return err
	}
	if statusCode != http.StatusBadRequest {
		return fmt.Errorf("invalid algorithm 预期返回 400, got=%d body=%s", statusCode, string(body))
	}
	return nil
}

func runOTAInvalidChallengeCase(serverAddr, deviceID string, _ *protocolTestCase) error {
	resp, err := requestOTAConfig(serverAddr, deviceID)
	if err != nil {
		return err
	}
	if resp.Activation == nil {
		return skipCase("OTA 未返回 activation，可能 auth 未开启或当前设备已激活")
	}

	statusCode, body, err := requestOTAActivate(serverAddr, deviceID, otaActivationPayload{
		Algorithm:    "hmac-sha256",
		SerialNumber: "AUTO-TEST-SN",
		Challenge:    strings.TrimSpace(resp.Activation.Challenge) + "-invalid",
		HMAC:         "invalid-hmac",
	})
	if err != nil {
		return err
	}
	if statusCode != http.StatusAccepted {
		return fmt.Errorf("invalid challenge 预期返回 202, got=%d body=%s", statusCode, string(body))
	}
	return nil
}

func runMqttUDPHelloCase(serverAddr, deviceID string, testCase *protocolTestCase) error {
	resp, err := requestOTAConfig(serverAddr, deviceID)
	if err != nil {
		return err
	}
	if resp.Mqtt == nil {
		return skipCase("OTA 未返回 mqtt 配置，请先开启 ota.test/external.mqtt.enable")
	}

	rt, err := newMqttProtocolRuntime(deviceID, resp.Mqtt)
	if err != nil {
		return err
	}
	defer rt.close()

	if err := rt.publish(ClientMessage{
		Type:        MessageTypeHello,
		DeviceID:    deviceID,
		Version:     3,
		Transport:   "udp",
		AudioParams: defaultAudioFormat(),
	}); err != nil {
		return err
	}
	firstHello, err := waitForMessage(rt.helloAckCh, testCase.Timeout, "mqtt hello ack")
	if err != nil {
		return err
	}
	if err := assertMqttHelloMessage(firstHello); err != nil {
		return err
	}
	firstNonce := firstHello.Udp.Nonce

	if err := rt.publish(ClientMessage{
		Type:        MessageTypeHello,
		DeviceID:    deviceID,
		Version:     3,
		Transport:   "udp",
		AudioParams: defaultAudioFormat(),
	}); err != nil {
		return err
	}
	secondHello, err := waitForMessage(rt.helloAckCh, testCase.Timeout, "mqtt duplicate hello ack")
	if err != nil {
		return err
	}
	if err := assertMqttHelloMessage(secondHello); err != nil {
		return err
	}
	if firstNonce == secondHello.Udp.Nonce {
		return fmt.Errorf("duplicate hello 后 UDP nonce 未变化: %s", firstNonce)
	}

	events := rt.snapshotEvents()
	return assertHelloTransport(events, "udp", 2)
}

func runMqttUDPInjectedMessageCase(serverAddr, deviceID string, testCase *protocolTestCase) error {
	resp, err := requestOTAConfig(serverAddr, deviceID)
	if err != nil {
		return err
	}
	if resp.Mqtt == nil {
		return skipCase("OTA 未返回 mqtt 配置，请先开启 ota.test/external.mqtt.enable")
	}

	rt, err := newMqttProtocolRuntime(deviceID, resp.Mqtt)
	if err != nil {
		return err
	}
	defer rt.close()

	if err := rt.publish(ClientMessage{
		Type:        MessageTypeHello,
		DeviceID:    deviceID,
		Version:     3,
		Transport:   "udp",
		AudioParams: defaultAudioFormat(),
	}); err != nil {
		return err
	}
	helloMsg, err := waitForMessage(rt.helloAckCh, testCase.Timeout, "mqtt injected hello ack")
	if err != nil {
		return err
	}
	if err := assertMqttHelloMessage(helloMsg); err != nil {
		return err
	}

	udpClient, err := newAutoUDPClient(helloMsg.Udp.Server, helloMsg.Udp.Port, helloMsg.Udp.Key, helloMsg.Udp.Nonce)
	if err != nil {
		return err
	}
	rt.udpClient = udpClient
	if err := udpClient.ReceiveAudioData(func(audioData []byte) {
		rt.recordIncomingBinary(len(audioData), true)
		select {
		case rt.udpAudioCh <- audioData:
		default:
		}
	}); err != nil {
		return err
	}
	if err := udpClient.SendAudioData(nil); err != nil {
		return fmt.Errorf("建立 UDP addr 绑定失败: %v", err)
	}

	injectErrCh := make(chan error, 1)
	go func() {
		injectErrCh <- postInjectMessage(serverAddr, deviceID, testCase.InputText, true, false)
	}()

	var speakRequest ServerMessage
	select {
	case speakRequest = <-rt.speakRequestCh:
	case err := <-injectErrCh:
		if err != nil {
			return err
		}
		return fmt.Errorf("inject_msg 在收到 speak_request 前已完成")
	case <-time.After(testCase.Timeout):
		return fmt.Errorf("等待 mqtt speak_request 超时")
	}
	if speakRequest.SessionID != helloMsg.SessionID {
		return fmt.Errorf("speak_request session_id 不匹配: got=%s want=%s", speakRequest.SessionID, helloMsg.SessionID)
	}
	if speakRequest.AutoListen == nil || *speakRequest.AutoListen {
		return fmt.Errorf("speak_request auto_listen 预期为 false")
	}

	if err := rt.publish(ClientMessage{
		Type:      "speak_ready",
		SessionID: speakRequest.SessionID,
		State:     "ready",
		SpeakUDPConfig: &SpeakReadyUDPConfig{
			Ready:         true,
			ReuseExisting: true,
		},
	}); err != nil {
		return err
	}
	select {
	case err := <-injectErrCh:
		if err != nil {
			return err
		}
	case <-time.After(testCase.Timeout):
		return fmt.Errorf("等待 inject_msg 响应完成超时")
	}

	if err := waitForSignal(rt.ttsStartCh, testCase.Timeout, "mqtt tts start"); err != nil {
		return err
	}
	if _, err := waitForMessage(rt.outputCh, testCase.Timeout, "mqtt output"); err != nil {
		return err
	}
	select {
	case <-rt.udpAudioCh:
	case <-time.After(testCase.Timeout):
		return fmt.Errorf("等待 UDP TTS 音频超时")
	}
	if err := waitForSignal(rt.ttsStopCh, testCase.Timeout, "mqtt tts stop"); err != nil {
		return err
	}
	if _, err := waitForMessage(rt.goodbyeCh, testCase.Timeout, "mqtt server goodbye"); err != nil {
		return err
	}

	events := rt.snapshotEvents()
	if err := assertHelloTransport(events, "udp", 1); err != nil {
		return err
	}
	if err := assertOutputText(events); err != nil {
		return err
	}
	if err := assertTTSLifecycle(events); err != nil {
		return err
	}
	if err := assertBinaryAcceptedAfterTTSStart(events); err != nil {
		return err
	}
	if err := assertHasServerGoodbye(events); err != nil {
		return err
	}
	if err := assertHasSpeakRequest(events); err != nil {
		return err
	}
	return nil
}

func buildOTAURL(serverAddr string) (string, error) {
	parsed, err := neturl.Parse(serverAddr)
	if err != nil {
		return "", err
	}
	switch parsed.Scheme {
	case "ws":
		parsed.Scheme = "http"
	case "wss":
		parsed.Scheme = "https"
	}
	parsed.Path = "/xiaozhi/ota/"
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func requestOTAConfig(serverAddr, deviceID string) (*otaResponse, error) {
	otaURL, err := buildOTAURL(serverAddr)
	if err != nil {
		return nil, err
	}

	body, err := json.Marshal(buildOTADeviceInfo(deviceID))
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, otaURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Device-Id", deviceID)
	req.Header.Set("Client-Id", clientId)
	req.Header.Set("Activation-Version", "1")
	req.Header.Set("User-Agent", "auto-test/xiaozhi")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OTA 请求失败: status=%d body=%s", resp.StatusCode, string(respBody))
	}

	var result otaResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func requestOTAActivate(serverAddr, deviceID string, payload otaActivationPayload) (int, []byte, error) {
	otaURL, err := buildOTAURL(serverAddr)
	if err != nil {
		return 0, nil, err
	}
	if !strings.HasSuffix(otaURL, "/") {
		otaURL += "/"
	}
	otaURL += "activate"

	body, err := json.Marshal(otaActivationRequest{Payload: payload})
	if err != nil {
		return 0, nil, err
	}
	req, err := http.NewRequest(http.MethodPost, otaURL, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Device-Id", deviceID)
	req.Header.Set("Client-Id", clientId)
	req.Header.Set("Activation-Version", "1")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, err
	}
	return resp.StatusCode, respBody, nil
}

func buildOTADeviceInfo(deviceID string) map[string]interface{} {
	return map[string]interface{}{
		"version":                2,
		"flash_size":             16777216,
		"minimum_free_heap_size": 7265024,
		"mac_address":            deviceID,
		"uuid":                   clientId,
		"chip_model_name":        "esp32s3",
		"chip_info": map[string]interface{}{
			"model":    9,
			"cores":    2,
			"revision": 0,
			"features": 20,
		},
		"application": map[string]interface{}{
			"name":         "xiaozhi",
			"version":      "1.6.0",
			"compile_time": "2026-05-10T12:00:00Z",
			"idf_version":  "v5.3.2",
		},
		"partition_table": []map[string]interface{}{
			{"label": "nvs", "type": 1, "subtype": 2, "address": 36864, "size": 24576},
			{"label": "otadata", "type": 1, "subtype": 0, "address": 61440, "size": 8192},
			{"label": "app0", "type": 0, "subtype": 0, "address": 65536, "size": 1966080},
		},
		"ota": map[string]interface{}{
			"label": "app0",
		},
		"board": map[string]interface{}{
			"type":    "auto-test",
			"name":    "auto-test-board",
			"feature": []string{"wifi", "psram"},
			"ip":      "127.0.0.1",
			"mac":     deviceID,
		},
	}
}

func newMqttProtocolRuntime(deviceID string, info *otaMqttInfo) (*mqttProtocolRuntime, error) {
	if info == nil {
		return nil, fmt.Errorf("mqtt 配置为空")
	}

	rt := &mqttProtocolRuntime{
		deviceID:       deviceID,
		publishTopic:   info.PublishTopic,
		helloAckCh:     make(chan ServerMessage, 4),
		speakRequestCh: make(chan ServerMessage, 4),
		ttsStartCh:     make(chan struct{}, 4),
		ttsStopCh:      make(chan struct{}, 4),
		sttCh:          make(chan ServerMessage, 8),
		outputCh:       make(chan ServerMessage, 16),
		iotCh:          make(chan ServerMessage, 4),
		goodbyeCh:      make(chan ServerMessage, 4),
		udpAudioCh:     make(chan []byte, 16),
		mcpCh:          make(chan protocolEvent, 16),
		mcpSendMsgChan: make(chan []byte, 10),
		mcpRecvMsgChan: make(chan []byte, 10),
	}

	go func() {
		for msg := range rt.mcpSendMsgChan {
			clientMsg := ClientMessage{
				Type:      MessageTypeMcp,
				DeviceID:  deviceID,
				SessionID: "",
				PayLoad:   msg,
			}
			body, err := json.Marshal(clientMsg)
			if err != nil {
				continue
			}
			event := newMCPEvent("send", msg)
			rt.recordEvent(event)
			token := rt.client.Publish(rt.publishTopic, 0, false, body)
			token.Wait()
		}
	}()
	go func() {
		NewMcpServer(rt.mcpSendMsgChan, rt.mcpRecvMsgChan)
	}()

	brokerURL, tlsConfig, err := buildMQTTBrokerConfig(info.Endpoint)
	if err != nil {
		return nil, err
	}
	opts := mqtt.NewClientOptions()
	opts.AddBroker(brokerURL)
	opts.SetClientID(info.ClientID)
	opts.SetUsername(info.Username)
	opts.SetPassword(info.Password)
	opts.SetCleanSession(true)
	opts.SetAutoReconnect(true)
	opts.SetDefaultPublishHandler(rt.handleMessage)
	if tlsConfig != nil {
		opts.SetTLSConfig(tlsConfig)
	}

	client := mqtt.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		return nil, token.Error()
	}
	rt.client = client
	return rt, nil
}

func buildMQTTBrokerConfig(endpoint string) (string, *tls.Config, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return "", nil, fmt.Errorf("mqtt endpoint 为空")
	}
	if strings.Contains(endpoint, "://") {
		parsed, err := neturl.Parse(endpoint)
		if err != nil {
			return "", nil, err
		}
		if parsed.Scheme == "tls" || parsed.Scheme == "ssl" {
			return endpoint, &tls.Config{InsecureSkipVerify: true, ServerName: parsed.Hostname()}, nil
		}
		return endpoint, nil, nil
	}

	host := endpoint
	port := "8883"
	if strings.Contains(endpoint, ":") {
		lastColon := strings.LastIndex(endpoint, ":")
		host = endpoint[:lastColon]
		port = endpoint[lastColon+1:]
	} else if isPrivateMQTTHost(endpoint) {
		port = "2883"
	}
	scheme := "tls"
	var tlsConfig *tls.Config
	if port != "8883" {
		scheme = "tcp"
	} else {
		tlsConfig = &tls.Config{InsecureSkipVerify: true, ServerName: host}
	}
	return fmt.Sprintf("%s://%s:%s", scheme, host, port), tlsConfig, nil
}

func isPrivateMQTTHost(host string) bool {
	host = strings.TrimSpace(strings.ToLower(host))
	return host == "localhost" ||
		host == "127.0.0.1" ||
		strings.HasPrefix(host, "10.") ||
		strings.HasPrefix(host, "192.168.") ||
		strings.HasPrefix(host, "172.16.") ||
		strings.HasPrefix(host, "172.17.") ||
		strings.HasPrefix(host, "172.18.") ||
		strings.HasPrefix(host, "172.19.") ||
		strings.HasPrefix(host, "172.20.") ||
		strings.HasPrefix(host, "172.21.") ||
		strings.HasPrefix(host, "172.22.") ||
		strings.HasPrefix(host, "172.23.") ||
		strings.HasPrefix(host, "172.24.") ||
		strings.HasPrefix(host, "172.25.") ||
		strings.HasPrefix(host, "172.26.") ||
		strings.HasPrefix(host, "172.27.") ||
		strings.HasPrefix(host, "172.28.") ||
		strings.HasPrefix(host, "172.29.") ||
		strings.HasPrefix(host, "172.30.") ||
		strings.HasPrefix(host, "172.31.")
}

func (rt *mqttProtocolRuntime) close() {
	if rt.udpClient != nil {
		rt.udpClient.Close()
	}
	if rt.client != nil && rt.client.IsConnected() {
		rt.client.Disconnect(250)
	}
}

func (rt *mqttProtocolRuntime) publish(msg ClientMessage) error {
	if msg.DeviceID == "" {
		msg.DeviceID = rt.deviceID
	}
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	rt.recordOutgoingMessage(msg)
	token := rt.client.Publish(rt.publishTopic, 0, false, body)
	token.Wait()
	return token.Error()
}

func (rt *mqttProtocolRuntime) handleMessage(_ mqtt.Client, msg mqtt.Message) {
	var serverMsg ServerMessage
	if err := json.Unmarshal(msg.Payload(), &serverMsg); err != nil {
		return
	}
	rt.recordIncomingMessage(serverMsg)

	switch serverMsg.Type {
	case MessageTypeHello:
		rt.sessionID = serverMsg.SessionID
		select {
		case rt.helloAckCh <- serverMsg:
		default:
		}
	case MessageTypeMcp:
		if len(serverMsg.PayLoad) > 0 {
			event := newMCPEvent("recv", serverMsg.PayLoad)
			rt.recordEvent(event)
			select {
			case rt.mcpCh <- event:
			default:
			}
			select {
			case rt.mcpRecvMsgChan <- serverMsg.PayLoad:
			default:
			}
		}
	case "speak_request":
		select {
		case rt.speakRequestCh <- serverMsg:
		default:
		}
	case ServerMessageTypeSTT:
		select {
		case rt.sttCh <- serverMsg:
		default:
		}
	case MessageTypeIot:
		select {
		case rt.iotCh <- serverMsg:
		default:
		}
	case ServerMessageTypeLLM, ServerMessageTypeText:
		select {
		case rt.outputCh <- serverMsg:
		default:
		}
	case ServerMessageTypeTTS:
		switch serverMsg.State {
		case MessageStateStart:
			select {
			case rt.ttsStartCh <- struct{}{}:
			default:
			}
		case MessageStateStop:
			select {
			case rt.ttsStopCh <- struct{}{}:
			default:
			}
		case MessageStateSentenceStart, MessageStateSentenceEnd:
			if strings.TrimSpace(serverMsg.Text) != "" {
				select {
				case rt.outputCh <- serverMsg:
				default:
				}
			}
		}
	case MessageTypeGoodBye:
		select {
		case rt.goodbyeCh <- serverMsg:
		default:
		}
	}
}

func (rt *mqttProtocolRuntime) recordOutgoingMessage(msg ClientMessage) {
	rt.recordEvent(protocolEvent{
		Direction: "send",
		Type:      msg.Type,
		State:     msg.State,
		Mode:      msg.Mode,
		Text:      msg.Text,
		At:        time.Now(),
	})
}

func (rt *mqttProtocolRuntime) recordIncomingMessage(msg ServerMessage) {
	rt.recordEvent(protocolEvent{
		Direction: "recv",
		Type:      msg.Type,
		State:     msg.State,
		Text:      msg.Text,
		SessionID: msg.SessionID,
		Transport: msg.Transport,
		SampleRate: func() int {
			if msg.AudioFormat == nil {
				return 0
			}
			return msg.AudioFormat.SampleRate
		}(),
		Channels: func() int {
			if msg.AudioFormat == nil {
				return 0
			}
			return msg.AudioFormat.Channels
		}(),
		FrameMs: func() int {
			if msg.AudioFormat == nil {
				return 0
			}
			return msg.AudioFormat.FrameDuration
		}(),
		AudioFormat: func() string {
			if msg.AudioFormat == nil {
				return ""
			}
			return msg.AudioFormat.Format
		}(),
		At: time.Now(),
	})
}

func (rt *mqttProtocolRuntime) recordIncomingBinary(size int, accepted bool) {
	note := "accepted"
	if !accepted {
		note = "ignored"
	}
	rt.recordEvent(protocolEvent{
		Direction:   "recv_binary",
		Type:        "binary",
		BinaryBytes: size,
		Note:        note,
		At:          time.Now(),
	})
}

func (rt *mqttProtocolRuntime) recordEvent(event protocolEvent) {
	rt.eventMu.Lock()
	defer rt.eventMu.Unlock()
	rt.events = append(rt.events, event)
}

func (rt *mqttProtocolRuntime) snapshotEvents() []protocolEvent {
	rt.eventMu.Lock()
	defer rt.eventMu.Unlock()
	out := make([]protocolEvent, len(rt.events))
	copy(out, rt.events)
	return out
}

func assertMqttHelloMessage(msg ServerMessage) error {
	if msg.Type != MessageTypeHello {
		return fmt.Errorf("hello 消息类型错误: %s", msg.Type)
	}
	if strings.TrimSpace(msg.Transport) != "udp" {
		return fmt.Errorf("mqtt hello transport 非 udp: %s", msg.Transport)
	}
	if strings.TrimSpace(msg.SessionID) == "" {
		return fmt.Errorf("mqtt hello session_id 为空")
	}
	if msg.AudioFormat == nil {
		return fmt.Errorf("mqtt hello audio_params 为空")
	}
	if msg.Udp == nil {
		return fmt.Errorf("mqtt hello udp 配置为空")
	}
	if strings.TrimSpace(msg.Udp.Server) == "" || msg.Udp.Port <= 0 {
		return fmt.Errorf("mqtt hello udp server/port 非法: %+v", msg.Udp)
	}
	if strings.TrimSpace(msg.Udp.Key) == "" || strings.TrimSpace(msg.Udp.Nonce) == "" {
		return fmt.Errorf("mqtt hello udp key/nonce 为空")
	}
	return nil
}

func assertHasServerGoodbye(events []protocolEvent) error {
	for _, event := range events {
		if event.Direction == "recv" && event.Type == MessageTypeGoodBye {
			return nil
		}
	}
	return fmt.Errorf("未收到服务端 goodbye")
}

func assertHasSpeakRequest(events []protocolEvent) error {
	for _, event := range events {
		if event.Direction == "recv" && event.Type == "speak_request" {
			return nil
		}
	}
	return fmt.Errorf("未收到 speak_request")
}

func newAutoUDPClient(serverAddr string, port int, aesKey, aesNonce string) (*autoUDPClient, error) {
	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", serverAddr, port))
	if err != nil {
		return nil, err
	}
	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return nil, err
	}
	return &autoUDPClient{
		conn:     conn,
		aesKey:   aesKey,
		aesNonce: aesNonce,
	}, nil
}

func (c *autoUDPClient) Close() {
	if c.conn != nil {
		_ = c.conn.Close()
	}
}

func (c *autoUDPClient) SendAudioData(audioData []byte) error {
	c.localSeq = (c.localSeq + 1) & 0xFFFFFFFF
	nonceHex := c.aesNonce[:4] +
		fmt.Sprintf("%04x", len(audioData)) +
		c.aesNonce[8:24] +
		fmt.Sprintf("%08x", c.localSeq)

	key, err := hex.DecodeString(c.aesKey)
	if err != nil {
		return err
	}
	nonceBytes, err := hex.DecodeString(nonceHex)
	if err != nil {
		return err
	}
	encryptedData, err := aesCTREncrypt(key, nonceBytes, audioData)
	if err != nil {
		return err
	}
	packet := append(nonceBytes, encryptedData...)
	_, err = c.conn.Write(packet)
	return err
}

func (c *autoUDPClient) ReceiveAudioData(cb func(audioData []byte)) error {
	key, err := hex.DecodeString(c.aesKey)
	if err != nil {
		return err
	}
	go func() {
		buffer := make([]byte, 4096)
		for {
			n, _, err := c.conn.ReadFromUDP(buffer)
			if err != nil {
				return
			}
			audioData, err := decryptUDPData(key, buffer[:n])
			if err != nil {
				continue
			}
			cb(audioData)
		}
	}()
	return nil
}

func decryptUDPData(key []byte, data []byte) ([]byte, error) {
	if len(data) < 16 {
		return nil, fmt.Errorf("udp 包长度不足: %d", len(data))
	}
	nonce := data[:16]
	ciphertext := data[16:]
	return aesCTRDecrypt(key, nonce, ciphertext)
}

func aesCTREncrypt(key, nonce, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	stream := cipher.NewCTR(block, nonce)
	ciphertext := make([]byte, len(plaintext))
	stream.XORKeyStream(ciphertext, plaintext)
	return ciphertext, nil
}

func aesCTRDecrypt(key, nonce, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	stream := cipher.NewCTR(block, nonce)
	plaintext := make([]byte, len(ciphertext))
	stream.XORKeyStream(plaintext, ciphertext)
	return plaintext, nil
}
