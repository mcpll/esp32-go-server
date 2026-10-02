package main

import (
	"bufio"
	"context"
	"crypto/md5"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"

	"xiaozhi-esp32-server-golang/constants"
	"xiaozhi-esp32-server-golang/internal/domain/tts"
)

var sendAudioEndTs int64
var firstTts bool
var firstAudio bool
var awaitFirstTTSAudio bool
var awaitFirstTTSAudioTs int64
var opusData [][]byte

var audioRate = 16000
var frameDuration = 60

var allowChat = make(chan struct{}, 1)
var ttsProviderName = constants.TtsTypeCosyvoice

const (
	deviceStateIdle         = "idle"
	deviceStateConnecting   = "connecting"
	deviceStateConversation = "conversation"
	deviceStateListening    = "listening"
	deviceStateSpeaking     = "speaking"
	speakRequestReuseWindow = 60 * time.Second
)

type speakRequestPlaybackState struct {
	SessionID  string
	AutoListen bool
}

// ServerMessage represents a server message
type ServerMessage struct {
	Type        string      `json:"type"`
	Text        string      `json:"text,omitempty"`
	SessionID   string      `json:"session_id,omitempty"`
	Version     int         `json:"version"`
	State       string      `json:"state,omitempty"`
	Transport   string      `json:"transport,omitempty"`
	AudioFormat AudioFormat `json:"audio_params,omitempty"`
	Emotion     string      `json:"emotion,omitempty"`
	AutoListen  *bool       `json:"auto_listen,omitempty"`
}

type AudioFormat struct {
	Format        string `json:"format,omitempty"`
	SampleRate    int    `json:"sample_rate,omitempty"`
	Channels      int    `json:"channels,omitempty"`
	FrameDuration int    `json:"frame_duration,omitempty"`
}

// UDPConfig represents the UDP configuration structure
type UDPConfig struct {
	Type      string `json:"type"`
	Version   int    `json:"version"`
	SessionID string `json:"session_id"`
	Transport string `json:"transport"`
	UDP       struct {
		Server     string `json:"server"`
		Port       int    `json:"port"`
		Encryption string `json:"encryption"`
		Key        string `json:"key"`
		Nonce      string `json:"nonce"`
	} `json:"udp"`
	AudioParams struct {
		Format        string `json:"format"`
		SampleRate    int    `json:"sample_rate"`
		Channels      int    `json:"channels"`
		FrameDuration int    `json:"frame_duration"`
	} `json:"audio_params"`
}

var globalChannel chan *UDPConfig
var serverConfig *ServerResponse
var helloResponseMu sync.Mutex
var pendingHelloResponse chan *UDPConfig
var runtimeMu sync.RWMutex
var currentUDPConfig *UDPConfig
var currentUDPClient *UDPClient
var currentSessionID string
var currentDeviceState = deviceStateConnecting
var lastUDPTrafficAt time.Time
var pendingSpeakRequestPlayback *speakRequestPlaybackState
var armedAutoListenSessionID string

func releaseAllowChat() {
	select {
	case allowChat <- struct{}{}:
	default:
	}
}

func setDeviceState(state string) {
	runtimeMu.Lock()
	defer runtimeMu.Unlock()
	currentDeviceState = state
}

func getDeviceState() string {
	runtimeMu.RLock()
	defer runtimeMu.RUnlock()
	return currentDeviceState
}

func getCurrentSessionID() string {
	runtimeMu.RLock()
	defer runtimeMu.RUnlock()
	return currentSessionID
}

func getCurrentUDPClient() *UDPClient {
	runtimeMu.RLock()
	defer runtimeMu.RUnlock()
	return currentUDPClient
}

func setPendingSpeakRequestPlayback(sessionID string, autoListen bool) {
	runtimeMu.Lock()
	defer runtimeMu.Unlock()
	pendingSpeakRequestPlayback = &speakRequestPlaybackState{
		SessionID:  strings.TrimSpace(sessionID),
		AutoListen: autoListen,
	}
}

func consumePendingSpeakRequestPlayback() (*speakRequestPlaybackState, bool) {
	runtimeMu.Lock()
	defer runtimeMu.Unlock()
	if pendingSpeakRequestPlayback == nil {
		return nil, false
	}
	state := *pendingSpeakRequestPlayback
	pendingSpeakRequestPlayback = nil
	return &state, true
}

func clearPendingSpeakRequestPlayback() {
	runtimeMu.Lock()
	defer runtimeMu.Unlock()
	pendingSpeakRequestPlayback = nil
}

func armAutoListen(sessionID string) {
	runtimeMu.Lock()
	defer runtimeMu.Unlock()
	armedAutoListenSessionID = strings.TrimSpace(sessionID)
}

func consumeAutoListen(sessionID string) bool {
	runtimeMu.Lock()
	defer runtimeMu.Unlock()

	if armedAutoListenSessionID == "" {
		return false
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID != "" && armedAutoListenSessionID != sessionID {
		return false
	}

	armedAutoListenSessionID = ""
	return true
}

func clearAutoListen() {
	runtimeMu.Lock()
	defer runtimeMu.Unlock()
	armedAutoListenSessionID = ""
}

func markUDPTraffic() {
	runtimeMu.Lock()
	defer runtimeMu.Unlock()
	lastUDPTrafficAt = time.Now()
}

func shouldReuseExistingUDP() bool {
	runtimeMu.RLock()
	defer runtimeMu.RUnlock()

	if currentUDPClient == nil || currentUDPConfig == nil {
		return false
	}
	if lastUDPTrafficAt.IsZero() {
		return false
	}
	return time.Since(lastUDPTrafficAt) <= speakRequestReuseWindow
}

func setPendingHelloResponse(ch chan *UDPConfig) {
	helloResponseMu.Lock()
	defer helloResponseMu.Unlock()
	pendingHelloResponse = ch
}

func clearPendingHelloResponse(ch chan *UDPConfig) {
	helloResponseMu.Lock()
	defer helloResponseMu.Unlock()
	if pendingHelloResponse == ch {
		pendingHelloResponse = nil
	}
}

func dispatchHelloResponse(cfg *UDPConfig) {
	helloResponseMu.Lock()
	ch := pendingHelloResponse
	if ch != nil {
		pendingHelloResponse = nil
	}
	helloResponseMu.Unlock()

	if ch != nil {
		select {
		case ch <- cfg:
		default:
			fmt.Println("⚠️ speak_request hello response channel full, dropping this hello response")
		}
		return
	}

	select {
	case globalChannel <- cfg:
	default:
		fmt.Println("⚠️ no waiter for the hello response, dropping this config")
	}
}

func startUDPReceiver(udpClient *UDPClient, udpConfig *UDPConfig) error {
	hexKey, err := hex.DecodeString(udpConfig.UDP.Key)
	if err != nil {
		return fmt.Errorf("failed to parse UDP key: %w", err)
	}

	return udpClient.ReceiveAudioData(hexKey, func(key []byte, audioData []byte) {
		markUDPTraffic()

		decryptedData, err := udpClient.decryptAudioData(key, audioData)
		if err != nil {
			fmt.Println("decrypt failed:", err)
			return
		}
		if len(decryptedData) == 0 {
			fmt.Println("ℹ️ received empty UDP audio packet, ignoring")
			return
		}
		if awaitFirstTTSAudio {
			fmt.Printf("time from audio end to first frame: %d ms\n", time.Now().UnixMilli()-awaitFirstTTSAudioTs)
			awaitFirstTTSAudio = false
			_ = os.WriteFile("mqtt_output_first_frame.wav", decryptedData, 0644)
		}
		if !firstAudio {
			firstAudio = true
			fmt.Printf("received first audio message, elapsed: %d ms\n", time.Now().UnixMilli()-sendAudioEndTs)
		}

		opusData = append(opusData, decryptedData)
	})
}

func replaceUDPClient(udpConfig *UDPConfig) (*UDPClient, error) {
	if udpConfig == nil {
		return nil, errors.New("udp config is empty")
	}

	udpClient, err := NewUDPClient(udpConfig.UDP.Server, udpConfig.UDP.Port, udpConfig.UDP.Key, udpConfig.UDP.Nonce)
	if err != nil {
		return nil, err
	}
	if err := startUDPReceiver(udpClient, udpConfig); err != nil {
		udpClient.Close()
		return nil, err
	}

	runtimeMu.Lock()
	oldClient := currentUDPClient
	currentUDPClient = udpClient
	currentUDPConfig = udpConfig
	currentSessionID = udpConfig.SessionID
	lastUDPTrafficAt = time.Now()
	runtimeMu.Unlock()

	if oldClient != nil {
		oldClient.Close()
	}

	fmt.Printf("✅ UDP audio channel ready, session_id=%s, server=%s:%d\n", udpConfig.SessionID, udpConfig.UDP.Server, udpConfig.UDP.Port)
	return udpClient, nil
}

func waitForHelloResponse(mqttClient mqtt.Client, timeout time.Duration) (*UDPConfig, error) {
	ch := make(chan *UDPConfig, 1)
	setPendingHelloResponse(ch)
	defer clearPendingHelloResponse(ch)

	if err := publicHello(serverConfig.MQTT.PublishTopic, mqttClient); err != nil {
		return nil, err
	}

	select {
	case cfg := <-ch:
		return cfg, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf("timed out waiting for repeated hello response")
	}
}

func test_aes_encrypt(plainText string) []byte {
	md5Data := md5.Sum([]byte(plainText))
	md5Str := hex.EncodeToString(md5Data[:])
	fmt.Println("md5Str before encrypt:", md5Str)

	// 32-byte key (256-bit)
	key, _ := hex.DecodeString("7f99ed0bf6647d38666628c322bc6a49")
	// 16-byte IV (128-bit)
	iv, _ := hex.DecodeString("010000003c2075c40000000000000000")

	//md5 iv
	ivMd5 := md5.Sum(iv)
	ivMd5Str := hex.EncodeToString(ivMd5[:])
	fmt.Println("ivMd5Str:", ivMd5Str)

	encryptedData, err := AesCTREncrypt(key, iv, []byte(plainText))
	if err != nil {
		fmt.Println("encrypt failed:", err)
		return nil
	}

	//Compute md5
	md5Data = md5.Sum(encryptedData)

	fmt.Println("md5 after encrypt:", hex.EncodeToString(md5Data[:]))
	return encryptedData
}

func test_aes_decrypt(data []byte) []byte {
	md5Data := md5.Sum(data)
	md5Str := hex.EncodeToString(md5Data[:])
	fmt.Println("md5Str before decrypt:", md5Str)

	// 32-byte key (256-bit)
	key, _ := hex.DecodeString("7f99ed0bf6647d38666628c322bc6a49")
	// 16-byte IV (128-bit)
	iv, _ := hex.DecodeString("010000003c2075c40000000000000000")

	decryptedData, err := AesCTRDecrypt(key, iv, data)
	if err != nil {
		fmt.Println("encrypt failed:", err)
		return nil
	}

	//Compute md5
	md5Data = md5.Sum(decryptedData)

	fmt.Println("md5 after decrypt:", hex.EncodeToString(md5Data[:]))
	return decryptedData
}

func main1() {
	plainText := "12345"
	fmt.Println("data before encrypt:", plainText)
	enc_data := test_aes_encrypt(plainText)
	dec_data := test_aes_decrypt(enc_data)
	fmt.Println("data after decrypt:", string(dec_data))
}

var listenMode = "manual" // Global: listen mode

func main() {
	otaUrl := flag.String("ota", "https://api.tenclass.net/xiaozhi/ota/", "OTA server address")
	deviceID := flag.String("device", "ba:8f:17:de:94:94", "device ID")
	mode := flag.String("mode", "manual", "listen mode: manual or auto")
	ttsProvider := flag.String("tts_provider", constants.TtsTypeCosyvoice, "TTS provider: cosyvoice|edge|edge_offline|indextts_vllm")
	flag.Parse()

	// Validate mode flag
	if *mode != "manual" && *mode != "auto" {
		fmt.Printf("❌ invalid mode: %s, only manual or auto is supported\n", *mode)
		os.Exit(1)
	}
	listenMode = *mode
	ttsProviderName = strings.ToLower(strings.TrimSpace(*ttsProvider))
	fmt.Printf("📋 listen mode: %s\n", listenMode)
	fmt.Printf("📋 TTS provider: %s\n", ttsProviderName)

	clientID := "e4b0c442-98fc-4e1b-8c3d-6a5b6a5b6a6d"
	boardName := "lc-esp32-s3"

	// Get device configuration
	deviceInfo := CreateDefaultDeviceInfo(clientID, *deviceID, boardName)

	// Generate serial number and HMAC key
	uuid1 := strings.ReplaceAll(uuid.New().String(), "-", "")
	uuid2 := strings.ReplaceAll(uuid.New().String(), "-", "")
	serialNumber := fmt.Sprintf("SN-%s-%s", strings.ToUpper(uuid1[:8]), uuid2[:12])

	// Generate HMAC key (32-byte hex string)
	//hmacKey := strings.ReplaceAll(uuid.New().String(), "-", "")
	hmacKey := "b05df1f583419f4a088c812533b4774b97d3ff5e22d5735d3aab8dff160ebef6"

	fmt.Printf("generated serial number: %s\n", serialNumber)
	fmt.Printf("generated HMAC key: %s\n", hmacKey)

	config, err := GetDeviceConfig(deviceInfo, *deviceID, clientID, *otaUrl)
	if err != nil {
		fmt.Println("failed to get device config:", err)
		os.Exit(1)
	}
	serverConfig = config

	if config.Activation.Code != "" {
		fmt.Println("device activating, code: ", config.Activation.Code)
		// Send activation request
		_, err := activateDevice(*deviceID, clientID, serialNumber, hmacKey, config.Activation.Challenge, *otaUrl)
		if err != nil {
			fmt.Println("device activation failed:", err)
			os.Exit(1)
		}
	} else {
		fmt.Println("device activated")
	}

	globalChannel = make(chan *UDPConfig, 1)

	// v3.1.1
	mqttClient, ok := connectMQTT(config)
	if !ok {
		fmt.Println("❌ MQTT connection failed")
		os.Exit(1)
	}

	var udpConfig *UDPConfig
	select {
	case udpConfig = <-globalChannel:
		fmt.Println("received UDP message")
	case <-time.After(10 * time.Second):
		fmt.Println("timed out waiting for hello message")
		return
	}

	connectUdqAndSendAudio(udpConfig, mqttClient)

	// Keep the process running
	select {}
}

func connectMQTT(config *ServerResponse) (mqtt.Client, bool) {
	// Setup MQTT client with configuration from server
	opts := mqtt.NewClientOptions()

	endpoint := config.MQTT.Endpoint
	port := "8883"
	protocol := "tls"
	if strings.Contains(endpoint, ":") {
		parts := strings.Split(endpoint, ":")
		endpoint = parts[0]
		port = parts[1]
	}
	if port != "8883" {
		protocol = "tcp"
	}
	brokerUrl := fmt.Sprintf("%s://%s:%s", protocol, endpoint, port)

	// Configure TLS
	tlsConfig := &tls.Config{
		ServerName: endpoint,
		//InsecureSkipVerify: true, // Skip cert verify; test only
	}
	if protocol == "tls" {
		opts.SetTLSConfig(tlsConfig)
	}
	opts.AddBroker(brokerUrl)
	opts.SetClientID(config.MQTT.ClientID)
	opts.SetUsername(config.MQTT.Username)
	opts.SetPassword(config.MQTT.Password)

	opts.SetKeepAlive(60 * time.Second)
	opts.SetAutoReconnect(true)
	opts.SetMaxReconnectInterval(1 * time.Minute)
	opts.SetConnectTimeout(30 * time.Second)
	opts.SetCleanSession(true)

	// Set connect callback
	/*
		opts.SetOnConnectHandler(func(client mqtt.Client) {
			version := "v3.1.1"
			if useV5 {
				version = "v5.0"
			}
			fmt.Printf("✅ MQTT %s connected\n", version)
		})*/

	// Set disconnect callback
	opts.SetConnectionLostHandler(func(client mqtt.Client, err error) {
		fmt.Printf("⚠️ MQTT connection lost: %v\n", err)
	})

	// Set reconnect callback
	opts.SetReconnectingHandler(func(client mqtt.Client, opts *mqtt.ClientOptions) {
		fmt.Println("🔄 reconnecting to MQTT server...")
	})

	// Set default message handler
	opts.SetDefaultPublishHandler(onMessage)

	client := mqtt.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		fmt.Println("❌ connection failed:", token.Error())
		return nil, false
	}

	// Publish a test message
	err := publicHello(config.MQTT.PublishTopic, client)
	if err != nil {
		fmt.Println("❌ publish failed:", err)
		return nil, false
	}

	return client, true
}

func publicHello(publishTopic string, client mqtt.Client) error {
	message := ServerMessage{
		Type:      "hello",
		Version:   3,
		Transport: "udp",
		AudioFormat: AudioFormat{
			Format:        "opus",
			SampleRate:    audioRate,
			Channels:      1,
			FrameDuration: frameDuration,
		},
	}
	jsonData, err := json.Marshal(message)
	if err != nil {
		return err
	}
	fmt.Println("📤 published to topic:", publishTopic, string(jsonData))

	// Use MQTT v5.0 publish options
	token := client.Publish(publishTopic, byte(0), false, jsonData)
	if token.Wait() && token.Error() != nil {
		return token.Error()
	}
	fmt.Println("✅ publish succeeded")
	return nil
}

func encodeHexPayload(payload []byte) string {
	return hex.EncodeToString(payload)
}

func onMessage(client mqtt.Client, msg mqtt.Message) {
	fmt.Printf("📩 received message: time: %d, topic: [%s] %s\n", time.Now().UnixMilli(), msg.Topic(), string(msg.Payload()))

	// Parse message
	var message map[string]interface{}
	if err := json.Unmarshal(msg.Payload(), &message); err != nil {
		fmt.Printf("❌ message parse error: %v, msg: %s\n", err, string(msg.Payload()))
		return
	}

	// Dispatch by message type
	msgType, ok := message["type"].(string)
	if !ok {
		fmt.Println("❌ bad message format: missing type field")
		return
	}

	switch msgType {
	case "hello":
		handleHello(client, msg)
	case "speak_request":
		handleSpeakRequest(client, msg)
	case "tts":
		handleTTS(client, msg)
	case "llm":
		handleLLM(client, msg)
	case "stt":
		handleStt(client, msg)
	case "goodbye":
		handleGoodbye(client, msg)
	default:
		fmt.Printf("⚠️ unknown message type: %s\n", msgType)
	}
}

func handleHello(client mqtt.Client, msg mqtt.Message) {
	fmt.Printf("handling hello message: %s\n", string(msg.Payload()))
	//Parse msg into HelloMessage
	var helloMessage UDPConfig
	if err := json.Unmarshal(msg.Payload(), &helloMessage); err != nil {
		fmt.Printf("❌ message parse error: %v\n", err)
		return
	}

	dispatchHelloResponse(&helloMessage)

	fmt.Printf("handling hello message: %+v\n", helloMessage)

}

type SpeakReadyUDPConfig struct {
	Ready         bool `json:"ready"`
	ReuseExisting bool `json:"reuse_existing,omitempty"`
}

func handleSpeakRequest(mqttClient mqtt.Client, msg mqtt.Message) {
	var request ServerMessage
	if err := json.Unmarshal(msg.Payload(), &request); err != nil {
		fmt.Printf("❌ speak_request parse failed: %v\n", err)
		return
	}

	if request.SessionID == "" {
		fmt.Println("❌ speak_request missing session_id, ignoring")
		return
	}
	if getDeviceState() != deviceStateIdle {
		fmt.Printf("⚠️ device state=%s, ignoring speak_request\n", getDeviceState())
		return
	}

	autoListen := true
	if request.AutoListen != nil {
		autoListen = *request.AutoListen
	}
	fmt.Printf("🔔 received speak_request: session_id=%s auto_listen=%v preview=%q\n", request.SessionID, autoListen, request.Text)
	clearPendingSpeakRequestPlayback()
	clearAutoListen()

	setDeviceState(deviceStateConnecting)
	reuseExisting := shouldReuseExistingUDP()
	if !reuseExisting {
		fmt.Println("ℹ️ UDP link cooled down, sending repeated hello to get the UDP config again")
		udpConfig, err := waitForHelloResponse(mqttClient, 10*time.Second)
		if err != nil {
			fmt.Printf("❌ speak_request repeated hello failed: %v\n", err)
			clearPendingSpeakRequestPlayback()
			clearAutoListen()
			setDeviceState(deviceStateIdle)
			releaseAllowChat()
			return
		}
		if _, err := replaceUDPClient(udpConfig); err != nil {
			fmt.Printf("❌ speak_request failed to rebuild UDP client: %v\n", err)
			clearPendingSpeakRequestPlayback()
			clearAutoListen()
			setDeviceState(deviceStateIdle)
			releaseAllowChat()
			return
		}
	} else {
		fmt.Println("ℹ️ reusing the existing UDP link for speak_request")
	}

	setDeviceState(deviceStateSpeaking)
	if err := sendSpeakReady(mqttClient, request.SessionID, reuseExisting); err != nil {
		fmt.Printf("❌ failed to send speak_ready: %v\n", err)
		clearPendingSpeakRequestPlayback()
		setDeviceState(deviceStateIdle)
		releaseAllowChat()
		return
	}
	setPendingSpeakRequestPlayback(request.SessionID, autoListen)
	firstAudio = false
	firstTts = false
	opusData = make([][]byte, 0)
	sendAudioEndTs = time.Now().UnixMilli()
}

func handleLLM(client mqtt.Client, msg mqtt.Message) {
	fmt.Printf("time from audio end to LLM message: %d ms\n", time.Now().UnixMilli()-sendAudioEndTs)
}

func handleStt(client mqtt.Client, msg mqtt.Message) {
	fmt.Printf("time from audio end to STT message: %d ms\n", time.Now().UnixMilli()-sendAudioEndTs)
}

func handleTTS(client mqtt.Client, msg mqtt.Message) {
	fmt.Printf("handling TTS message: %s\n", string(msg.Payload()))
	type st struct {
		Type  string `json:"type"`
		State string `json:"state"`
	}
	// TODO: Implement TTS state update
	var ttsState st
	if err := json.Unmarshal(msg.Payload(), &ttsState); err != nil {
		fmt.Printf("❌ message parse error: %v\n", err)
		return
	}
	fmt.Printf("handling TTS message: %s\n", ttsState)
	if ttsState.Type == "tts" && !firstTts {
		if ttsState.State == "sentence_start" {
			fmt.Printf("time from audio end to TTS start: %d ms\n", time.Now().UnixMilli()-sendAudioEndTs)
			firstTts = true
		}
	}

	if ttsState.State == "start" || ttsState.State == "sentence_start" {
		setDeviceState(deviceStateSpeaking)
	}

	if ttsState.State == "stop" {
		//pcmDataList, err := OpusToWav(opusData, audioRate, 1, "output_16000.wav")
		saveOpusData()
		pcmDataList, err := OpusToWav(opusData, 24000, 1, "output_24000.wav")
		if err != nil {
			fmt.Println("failed to convert WAV file:", err)
			return
		}
		fmt.Printf("TTS finished, audio data length: %d\n", len(pcmDataList))

		if playback, ok := consumePendingSpeakRequestPlayback(); ok {
			if playback.AutoListen {
				if err := enterAutoListen(client, playback.SessionID); err != nil {
					fmt.Printf("❌ speak_request.auto_listen failed to start: %v\n", err)
					clearAutoListen()
					setDeviceState(deviceStateIdle)
				}
				return
			}
			clearAutoListen()
			setDeviceState(deviceStateIdle)
			fmt.Println("ℹ️ speak_request playback finished, auto_listen=false, back to idle")
			return
		}

		clearAutoListen()
		setDeviceState(deviceStateIdle)
		releaseAllowChat()
	}
}

func saveOpusData() error {
	f, err := os.Create("opus_udp.data")
	if err != nil {
		return err
	}
	defer f.Close()

	for _, data := range opusData {
		f.Write(data)
	}

	f.Close()

	return nil
}

func handleGoodbye(client mqtt.Client, msg mqtt.Message) {
	fmt.Printf("handling goodbye message: %s\n", string(msg.Payload()))
	clearPendingSpeakRequestPlayback()
	clearAutoListen()
	setDeviceState(deviceStateIdle)
	releaseAllowChat()
}

func enterAutoListen(mqttClient mqtt.Client, sessionID string) error {
	sessionID = resolveActiveSessionID(sessionID)
	if sessionID == "" {
		return fmt.Errorf("auto_listen missing a valid session_id")
	}
	if err := sendListenStart(mqttClient, sessionID); err != nil {
		return err
	}
	armAutoListen(sessionID)
	setDeviceState(deviceStateListening)
	fmt.Printf("🎤 speak_request playback finished, auto_listen=true, entering listening state: session_id=%s\n", sessionID)
	releaseAllowChat()
	return nil
}

func resolveActiveSessionID(fallback string) string {
	sessionID := strings.TrimSpace(getCurrentSessionID())
	if sessionID != "" {
		return sessionID
	}
	return strings.TrimSpace(fallback)
}

func connectUdqAndSendAudio(udpConfig *UDPConfig, mqttClient mqtt.Client) error {
	if _, err := replaceUDPClient(udpConfig); err != nil {
		fmt.Println(err)
		return err
	}

	setDeviceState(deviceStateIdle)
	releaseAllowChat()
	sendTextToSpeech(mqttClient)

	/*

				sendListenStart(mqttClient, sessionId)
			time.Sleep(100 * time.Millisecond)
				err = sendWavFileWithOpusEncoding(udpInstance, "test.wav")
				if err != nil {
					fmt.Println(err)
					return err
				}
			fmt.Printf("Finished sending audio: %d\n", time.Now().UnixMilli())
		//sendListenStop(mqttClient, sessionId)
		fmt.Printf("Finished sending stop message: %d\n", time.Now().UnixMilli())
		sendAudioEndTs = time.Now().UnixMilli()
	*/

	return nil
}

// Read WAV, Opus-encode, and send
func sendWavFileWithOpusEncoding(udpInstance *UDPClient, filePath string) error {
	sampleRate := audioRate
	channels := 1
	// Open WAV file
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open WAV file: %v", err)
	}
	defer file.Close()

	// Read file contents
	fileContent, err := io.ReadAll(file)
	if err != nil {
		return fmt.Errorf("failed to read file content: %v", err)
	}
	fmt.Printf("file content length: %d\n", len(fileContent))
	file.Close()

	opusFrames, err := WavToOpus(fileContent, sampleRate, channels, 0)
	if err != nil {
		return fmt.Errorf("failed to convert WAV file: %v", err)
	}

	fmt.Printf("sending audio data: %d frames\n", len(opusFrames))

	for i, frame := range opusFrames {
		fmt.Printf("Opus frame %d length: %d\n", i, len(frame))
		// Send Opus frame
		if err := udpInstance.SendAudioData(frame); err != nil {
			return fmt.Errorf("failed to send Opus frame: %v", err)
		}
		// Throttle send rate to simulate realtime audio
		time.Sleep(60 * time.Millisecond)
	}
	fmt.Printf("total frames sent: %d\n", len(opusFrames))

	//Keep sending empty audio
	/*emptyFrame := make([]byte, 50)
	for {
		if err := conn.WriteMessage(websocket.BinaryMessage, emptyFrame); err != nil {
			return fmt.Errorf("Failed to send empty audio: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}*/

	return nil
}

// ClientMessage represents a client message
type ClientMessage struct {
	Type           string               `json:"type"`
	DeviceID       string               `json:"device_id,omitempty"`
	SessionID      string               `json:"session_id"`
	Text           string               `json:"text,omitempty"`
	Mode           string               `json:"mode,omitempty"`
	State          string               `json:"state,omitempty"`
	Token          string               `json:"token,omitempty"`
	DeviceMac      string               `json:"device_mac,omitempty"`
	Version        int                  `json:"version,omitempty"`
	Transport      string               `json:"transport,omitempty"`
	SpeakUDPConfig *SpeakReadyUDPConfig `json:"udp_config,omitempty"`
	Descriptors    []string             `json:"descriptors,omitempty"`
	States         []string             `json:"states,omitempty"`
}

// ClientMessage represents a client message
type IotClientMessage struct {
	Type        string   `json:"type"`
	SessionID   string   `json:"session_id"`
	Descriptors []string `json:"descriptors"`
}

// ClientMessage represents a client message
type IotStatesClientMessage struct {
	Type      string   `json:"type"`
	SessionID string   `json:"session_id"`
	States    []string `json:"states"`
}

func sendListenStart(mqttClient mqtt.Client, sessionID string) error {
	//sendIotMessage(mqttClient, sessionID)
	time.Sleep(1 * time.Second)
	message := ClientMessage{
		Type:      "listen",
		State:     "start",
		Mode:      listenMode,
		SessionID: sessionID,
	}
	jsonData, err := json.Marshal(message)
	if err != nil {
		return err
	}
	fmt.Println("📤 published to topic:", "", string(jsonData))

	token := mqttClient.Publish(serverConfig.MQTT.PublishTopic, byte(0), false, jsonData)
	if token.Wait() && token.Error() != nil {
		return token.Error()
	}
	return nil
}

func sendListenStop(mqttClient mqtt.Client, sessionID string) error {
	message := ClientMessage{
		Type:      "listen",
		State:     "stop",
		Mode:      listenMode,
		SessionID: sessionID,
	}
	jsonData, err := json.Marshal(message)
	if err != nil {
		return err
	}
	fmt.Println("📤 published to topic:", "", string(jsonData))

	token := mqttClient.Publish(serverConfig.MQTT.PublishTopic, byte(0), false, jsonData)
	if token.Wait() && token.Error() != nil {
		return token.Error()
	}
	return nil
}

func sendSpeakReady(mqttClient mqtt.Client, sessionID string, reuseExisting bool) error {
	message := ClientMessage{
		Type:      "speak_ready",
		State:     "ready",
		SessionID: sessionID,
		SpeakUDPConfig: &SpeakReadyUDPConfig{
			Ready:         true,
			ReuseExisting: reuseExisting,
		},
	}
	jsonData, err := json.Marshal(message)
	if err != nil {
		return err
	}
	fmt.Println("📤 published speak_ready to topic:", serverConfig.MQTT.PublishTopic, string(jsonData))

	token := mqttClient.Publish(serverConfig.MQTT.PublishTopic, byte(0), false, jsonData)
	if token.Wait() && token.Error() != nil {
		return token.Error()
	}
	return nil
}

func sendListenDetect(mqttClient mqtt.Client, sessionID string, text string) error {
	message := ClientMessage{
		Type:      "listen",
		State:     "detect",
		Text:      text,
		Mode:      listenMode,
		SessionID: sessionID,
	}
	jsonData, err := json.Marshal(message)
	if err != nil {
		return err
	}
	fmt.Println("📤 published to topic:", "", string(jsonData))

	token := mqttClient.Publish(serverConfig.MQTT.PublishTopic, byte(0), false, jsonData)
	if token.Wait() && token.Error() != nil {
		return token.Error()
	}
	return nil
}

func sendIotMessage(mqttClient mqtt.Client, sessionID string) error {
	message := IotClientMessage{
		Type:        "iot",
		SessionID:   sessionID,
		Descriptors: []string{},
	}
	jsonData, err := json.Marshal(message)
	if err != nil {
		return err
	}
	fmt.Println("📤 published to topic:", "", string(jsonData))

	token := mqttClient.Publish(serverConfig.MQTT.PublishTopic, byte(0), false, jsonData)
	if token.Wait() && token.Error() != nil {
		return token.Error()
	}

	messageStates := IotStatesClientMessage{
		Type:      "iot",
		SessionID: sessionID,
		States:    []string{},
	}
	jsonData, err = json.Marshal(messageStates)
	if err != nil {
		return err
	}
	fmt.Println("📤 published to topic:", "", string(jsonData))

	token = mqttClient.Publish(serverConfig.MQTT.PublishTopic, byte(0), false, jsonData)
	if token.Wait() && token.Error() != nil {
		return token.Error()
	}
	return nil
}

func sendAbort(mqttClient mqtt.Client, sessionID string) error {
	message := ClientMessage{
		Type:      "abort",
		SessionID: sessionID,
	}
	jsonData, err := json.Marshal(message)
	if err != nil {
		return err
	}
	fmt.Println("📤 published to topic:", "", string(jsonData))
	token := mqttClient.Publish(serverConfig.MQTT.PublishTopic, byte(0), false, jsonData)
	if token.Wait() && token.Error() != nil {
		return token.Error()
	}
	return nil
}

func getTTSProviderConfig() (string, map[string]interface{}, error) {
	cosyVoiceConfig := map[string]interface{}{
		"api_url":        "https://tts.linkerai.cn/tts",
		"spk_id":         "OUeAo1mhq6IBExi",
		"frame_duration": frameDuration,
		"target_sr":      audioRate,
		"audio_format":   "mp3",
		"instruct_text":  "你好", // Chinese kept: cosyvoice instruction text
	}
	edgeConfig := map[string]interface{}{
		"voice":           "zh-CN-XiaoxiaoNeural",
		"rate":            "+0%",
		"volume":          "+0%",
		"pitch":           "+0Hz",
		"connect_timeout": 10,
		"receive_timeout": 60,
	}
	edgeOfflineConfig := map[string]interface{}{
		"server_url":        "ws://192.168.208.214:8081/tts",
		"timeout":           30.0,
		"handshake_timeout": 10.0,
	}
	indexTTSVLLMConfig := map[string]interface{}{
		"api_url":         "http://127.0.0.1:7860/audio/speech",
		"model":           "indextts-vllm",
		"voice":           "zh-CN-XiaoxiaoNeural",
		"response_format": "wav",
		"stream":          false,
	}

	providerName := strings.TrimSpace(ttsProviderName)
	switch providerName {
	case constants.TtsTypeCosyvoice:
		return providerName, cosyVoiceConfig, nil
	case constants.TtsTypeEdge:
		return providerName, edgeConfig, nil
	case constants.TtsTypeEdgeOffline:
		return providerName, edgeOfflineConfig, nil
	case constants.TtsTypeIndexTTSVLLM:
		return providerName, indexTTSVLLMConfig, nil
	default:
		return "", nil, fmt.Errorf("unsupported tts provider: %s, choose one of: cosyvoice|edge|edge_offline|indextts_vllm", providerName)
	}
}

// Call TTS, Opus-encode, and send to the server
func sendTextToSpeech(mqttClient mqtt.Client) error {
	providerName, providerConfig, err := getTTSProviderConfig()
	if err != nil {
		return err
	}
	fmt.Printf("using TTS provider: %s\n", providerName)

	//Call TTS to synthesize speech
	ttsProvider, err := tts.GetTTSProvider(providerName, providerConfig)
	if err != nil {
		return fmt.Errorf("failed to get tts service (provider=%s): %v", providerName, err)
	}

	/*
		audioData, err := ttsProvider.TextToSpeech(context.Background(), "What is your name?")
		if err != nil {
			fmt.Printf("Speech synthesis failed: %v\n", err)
			return fmt.Errorf("Speech synthesis failed: %v", err)
		}
	*/

	opusData = make([][]byte, 0)

	var audioCtx context.Context
	var audioCancel context.CancelFunc

	genAndSendAudio := func(ctx context.Context, msg string, count int) error {
		sessionID := getCurrentSessionID()
		if sessionID == "" {
			return fmt.Errorf("current session_id is empty, cannot send audio")
		}
		udpInstance := getCurrentUDPClient()
		if udpInstance == nil {
			return fmt.Errorf("UDP client not initialized")
		}

		firstAudio = false
		firstTts = false
		opusData = make([][]byte, 0)
		setDeviceState(deviceStateConversation)
		autoListenArmed := consumeAutoListen(sessionID)
		if autoListenArmed {
			fmt.Printf("🎙️ reusing the listening state opened by auto_listen: session_id=%s\n", sessionID)
		} else {
			if err := sendListenStart(mqttClient, sessionID); err != nil {
				return err
			}
		}
		defer func() {
			awaitFirstTTSAudio = true
			if listenMode == "manual" {
				sendListenStop(mqttClient, sessionID)
			}
			awaitFirstTTSAudioTs = time.Now().UnixMilli()
		}()
		audioChan, err := ttsProvider.TextToSpeechStream(context.Background(), msg, 16000, 1, 60)
		if err != nil {
			//fmt.Printf("Speech synthesis failed: %v\n", err)
			return fmt.Errorf("failed to generate speech: %v", err)
		}

		for audioData := range audioChan {
			select {
			case <-ctx.Done():
				return nil
			default:
			}
			fmt.Printf("generated speech data length: %d\n", len(audioData))
			if err := udpInstance.SendAudioData(audioData); err != nil {
				return fmt.Errorf("failed to send UDP audio: %v", err)
			}
			time.Sleep(60 * time.Millisecond)
		}

		/*
			emptyFrame := make([]byte, 50)
			for i := 0; i <= count; i++ {
				udpInstance.SendAudioData(emptyFrame)
				time.Sleep(60 * time.Millisecond)
			}*/
		return nil
	}

	// Added: wait for user text input
	reader := bufio.NewReader(os.Stdin)

	f := func() bool {
		fmt.Print("Enter the text to synthesize (Enter to send, empty line to quit): ")
		input, err := reader.ReadString('\n')
		if err != nil {
			fmt.Printf("failed to read input: %v\n", err)
			return false
		}
		input = strings.TrimSpace(input)
		if input == "" {
			sessionID := getCurrentSessionID()
			clearAutoListen()
			clearPendingSpeakRequestPlayback()
			if sessionID != "" {
				sendAbort(mqttClient, sessionID)
			}
			if audioCancel != nil {
				audioCancel()
			}
			setDeviceState(deviceStateIdle)
			releaseAllowChat()
			return false
		}

		audioCtx, audioCancel = context.WithCancel(context.Background())
		if err := genAndSendAudio(audioCtx, input, 50); err != nil {
			fmt.Printf("❌ failed to send test audio: %v\n", err)
			setDeviceState(deviceStateIdle)
			releaseAllowChat()
			return false
		}
		return true
	}
	for {
		_ = <-allowChat
		for {
			if f() {
				break
			}
		}
	}

	//genAndSendAudio("Hello", 100)
	//time.Sleep(30 * time.Second)
	/*genAndSendAudio("Another one", 20)
	time.Sleep(30 * time.Second)
	genAndSendAudio("Your outfit looks great today", 20)
	time.Sleep(30 * time.Second)
	genAndSendAudio("What will you wear tomorrow", 20)
	time.Sleep(30 * time.Second)*/
}
