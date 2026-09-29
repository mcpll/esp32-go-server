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

// ServerMessage 表示服务器消息
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
			fmt.Println("⚠️ speak_request hello 响应通道已满，丢弃本次 hello 响应")
		}
		return
	}

	select {
	case globalChannel <- cfg:
	default:
		fmt.Println("⚠️ 无等待方处理 hello 响应，丢弃本次配置")
	}
}

func startUDPReceiver(udpClient *UDPClient, udpConfig *UDPConfig) error {
	hexKey, err := hex.DecodeString(udpConfig.UDP.Key)
	if err != nil {
		return fmt.Errorf("解析 UDP key 失败: %w", err)
	}

	return udpClient.ReceiveAudioData(hexKey, func(key []byte, audioData []byte) {
		markUDPTraffic()

		decryptedData, err := udpClient.decryptAudioData(key, audioData)
		if err != nil {
			fmt.Println("解密失败:", err)
			return
		}
		if len(decryptedData) == 0 {
			fmt.Println("ℹ️ 收到空 UDP 音频包，忽略")
			return
		}
		if awaitFirstTTSAudio {
			fmt.Printf("发送音频结束至收到首帧耗时: %d ms\n", time.Now().UnixMilli()-awaitFirstTTSAudioTs)
			awaitFirstTTSAudio = false
			_ = os.WriteFile("mqtt_output_first_frame.wav", decryptedData, 0644)
		}
		if !firstAudio {
			firstAudio = true
			fmt.Printf("收到第一条音频消息, 耗时: %d ms\n", time.Now().UnixMilli()-sendAudioEndTs)
		}

		opusData = append(opusData, decryptedData)
	})
}

func replaceUDPClient(udpConfig *UDPConfig) (*UDPClient, error) {
	if udpConfig == nil {
		return nil, errors.New("udp 配置为空")
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

	fmt.Printf("✅ UDP 音频通道已就绪, session_id=%s, server=%s:%d\n", udpConfig.SessionID, udpConfig.UDP.Server, udpConfig.UDP.Port)
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
		return nil, fmt.Errorf("等待重复 hello 响应超时")
	}
}

func test_aes_encrypt(plainText string) []byte {
	md5Data := md5.Sum([]byte(plainText))
	md5Str := hex.EncodeToString(md5Data[:])
	fmt.Println("加密前 md5Str:", md5Str)

	// 32字节的密钥 (256位)
	key, _ := hex.DecodeString("7f99ed0bf6647d38666628c322bc6a49")
	// 16字节的IV (128位)
	iv, _ := hex.DecodeString("010000003c2075c40000000000000000")

	//md5 iv
	ivMd5 := md5.Sum(iv)
	ivMd5Str := hex.EncodeToString(ivMd5[:])
	fmt.Println("ivMd5Str:", ivMd5Str)

	encryptedData, err := AesCTREncrypt(key, iv, []byte(plainText))
	if err != nil {
		fmt.Println("加密失败:", err)
		return nil
	}

	//计算md5
	md5Data = md5.Sum(encryptedData)

	fmt.Println("加密后的md5:", hex.EncodeToString(md5Data[:]))
	return encryptedData
}

func test_aes_decrypt(data []byte) []byte {
	md5Data := md5.Sum(data)
	md5Str := hex.EncodeToString(md5Data[:])
	fmt.Println("解密前 md5Str:", md5Str)

	// 32字节的密钥 (256位)
	key, _ := hex.DecodeString("7f99ed0bf6647d38666628c322bc6a49")
	// 16字节的IV (128位)
	iv, _ := hex.DecodeString("010000003c2075c40000000000000000")

	decryptedData, err := AesCTRDecrypt(key, iv, data)
	if err != nil {
		fmt.Println("加密失败:", err)
		return nil
	}

	//计算md5
	md5Data = md5.Sum(decryptedData)

	fmt.Println("解密后 md5:", hex.EncodeToString(md5Data[:]))
	return decryptedData
}

func main1() {
	plainText := "12345"
	fmt.Println("加密前数据:", plainText)
	enc_data := test_aes_encrypt(plainText)
	dec_data := test_aes_decrypt(enc_data)
	fmt.Println("解密后的数据:", string(dec_data))
}

var listenMode = "manual" // 全局变量，用于存储拾音模式

func main() {
	otaUrl := flag.String("ota", "https://api.tenclass.net/xiaozhi/ota/", "OTA服务器地址")
	deviceID := flag.String("device", "ba:8f:17:de:94:94", "设备ID")
	mode := flag.String("mode", "manual", "拾音模式: manual(手动) 或 auto(自动)")
	ttsProvider := flag.String("tts_provider", constants.TtsTypeCosyvoice, "TTS provider: cosyvoice|edge|edge_offline|indextts_vllm")
	flag.Parse()

	// 验证模式参数
	if *mode != "manual" && *mode != "auto" {
		fmt.Printf("❌ 无效的模式: %s，只支持 manual 或 auto\n", *mode)
		os.Exit(1)
	}
	listenMode = *mode
	ttsProviderName = strings.ToLower(strings.TrimSpace(*ttsProvider))
	fmt.Printf("📋 拾音模式: %s\n", listenMode)
	fmt.Printf("📋 TTS 提供商: %s\n", ttsProviderName)

	clientID := "e4b0c442-98fc-4e1b-8c3d-6a5b6a5b6a6d"
	boardName := "lc-esp32-s3"

	// Get device configuration
	deviceInfo := CreateDefaultDeviceInfo(clientID, *deviceID, boardName)

	// 生成序列号和HMAC密钥
	uuid1 := strings.ReplaceAll(uuid.New().String(), "-", "")
	uuid2 := strings.ReplaceAll(uuid.New().String(), "-", "")
	serialNumber := fmt.Sprintf("SN-%s-%s", strings.ToUpper(uuid1[:8]), uuid2[:12])

	// 生成HMAC密钥 (32字节的十六进制字符串)
	//hmacKey := strings.ReplaceAll(uuid.New().String(), "-", "")
	hmacKey := "b05df1f583419f4a088c812533b4774b97d3ff5e22d5735d3aab8dff160ebef6"

	fmt.Printf("生成的序列号: %s\n", serialNumber)
	fmt.Printf("生成的HMAC密钥: %s\n", hmacKey)

	config, err := GetDeviceConfig(deviceInfo, *deviceID, clientID, *otaUrl)
	if err != nil {
		fmt.Println("获取设备配置失败:", err)
		os.Exit(1)
	}
	serverConfig = config

	if config.Activation.Code != "" {
		fmt.Println("设备激活中, 验证码: ", config.Activation.Code)
		// 进行激活请求
		_, err := activateDevice(*deviceID, clientID, serialNumber, hmacKey, config.Activation.Challenge, *otaUrl)
		if err != nil {
			fmt.Println("设备激活失败:", err)
			os.Exit(1)
		}
	} else {
		fmt.Println("设备已激活")
	}

	globalChannel = make(chan *UDPConfig, 1)

	// v3.1.1
	mqttClient, ok := connectMQTT(config)
	if !ok {
		fmt.Println("❌ MQTT 连接失败")
		os.Exit(1)
	}

	var udpConfig *UDPConfig
	select {
	case udpConfig = <-globalChannel:
		fmt.Println("收到UDP消息")
	case <-time.After(10 * time.Second):
		fmt.Println("等待hello消息超时")
		return
	}

	connectUdqAndSendAudio(udpConfig, mqttClient)

	// 保持程序运行
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

	// 设置 TLS 配置
	tlsConfig := &tls.Config{
		ServerName: endpoint,
		//InsecureSkipVerify: true, // 跳过证书验证，仅用于测试环境
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

	// 设置连接回调
	/*
		opts.SetOnConnectHandler(func(client mqtt.Client) {
			version := "v3.1.1"
			if useV5 {
				version = "v5.0"
			}
			fmt.Printf("✅ MQTT %s 连接成功\n", version)
		})*/

	// 设置断开连接回调
	opts.SetConnectionLostHandler(func(client mqtt.Client, err error) {
		fmt.Printf("⚠️ MQTT 连接断开: %v\n", err)
	})

	// 设置重连回调
	opts.SetReconnectingHandler(func(client mqtt.Client, opts *mqtt.ClientOptions) {
		fmt.Println("🔄 正在重新连接 MQTT 服务器...")
	})

	// 设置默认消息处理函数
	opts.SetDefaultPublishHandler(onMessage)

	client := mqtt.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		fmt.Println("❌ 连接失败:", token.Error())
		return nil, false
	}

	// 发布一条测试消息
	err := publicHello(config.MQTT.PublishTopic, client)
	if err != nil {
		fmt.Println("❌ 发布消息失败:", err)
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
	fmt.Println("📤 发布消息to topic:", publishTopic, string(jsonData))

	// 使用 MQTT v5.0 的发布选项
	token := client.Publish(publishTopic, byte(0), false, jsonData)
	if token.Wait() && token.Error() != nil {
		return token.Error()
	}
	fmt.Println("✅ 发布消息成功")
	return nil
}

func encodeHexPayload(payload []byte) string {
	return hex.EncodeToString(payload)
}

func onMessage(client mqtt.Client, msg mqtt.Message) {
	fmt.Printf("📩 收到消息: 时间: %d, topic: [%s] %s\n", time.Now().UnixMilli(), msg.Topic(), string(msg.Payload()))

	// 解析消息
	var message map[string]interface{}
	if err := json.Unmarshal(msg.Payload(), &message); err != nil {
		fmt.Printf("❌ 消息解析错误: %v, msg: %s\n", err, string(msg.Payload()))
		return
	}

	// 根据消息类型处理
	msgType, ok := message["type"].(string)
	if !ok {
		fmt.Println("❌ 消息格式错误: 缺少type字段")
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
		fmt.Printf("⚠️ 未知消息类型: %s\n", msgType)
	}
}

func handleHello(client mqtt.Client, msg mqtt.Message) {
	fmt.Printf("处理 hello 消息: %s\n", string(msg.Payload()))
	//解析msg到HelloMessage
	var helloMessage UDPConfig
	if err := json.Unmarshal(msg.Payload(), &helloMessage); err != nil {
		fmt.Printf("❌ 消息解析错误: %v\n", err)
		return
	}

	dispatchHelloResponse(&helloMessage)

	fmt.Printf("处理 hello 消息: %+v\n", helloMessage)

}

type SpeakReadyUDPConfig struct {
	Ready         bool `json:"ready"`
	ReuseExisting bool `json:"reuse_existing,omitempty"`
}

func handleSpeakRequest(mqttClient mqtt.Client, msg mqtt.Message) {
	var request ServerMessage
	if err := json.Unmarshal(msg.Payload(), &request); err != nil {
		fmt.Printf("❌ speak_request 解析失败: %v\n", err)
		return
	}

	if request.SessionID == "" {
		fmt.Println("❌ speak_request 缺少 session_id，忽略")
		return
	}
	if getDeviceState() != deviceStateIdle {
		fmt.Printf("⚠️ 当前设备状态=%s，忽略 speak_request\n", getDeviceState())
		return
	}

	autoListen := true
	if request.AutoListen != nil {
		autoListen = *request.AutoListen
	}
	fmt.Printf("🔔 收到 speak_request: session_id=%s auto_listen=%v preview=%q\n", request.SessionID, autoListen, request.Text)
	clearPendingSpeakRequestPlayback()
	clearAutoListen()

	setDeviceState(deviceStateConnecting)
	reuseExisting := shouldReuseExistingUDP()
	if !reuseExisting {
		fmt.Println("ℹ️ UDP 链路已冷却，发送重复 hello 重新获取 UDP 配置")
		udpConfig, err := waitForHelloResponse(mqttClient, 10*time.Second)
		if err != nil {
			fmt.Printf("❌ speak_request 重复 hello 失败: %v\n", err)
			clearPendingSpeakRequestPlayback()
			clearAutoListen()
			setDeviceState(deviceStateIdle)
			releaseAllowChat()
			return
		}
		if _, err := replaceUDPClient(udpConfig); err != nil {
			fmt.Printf("❌ speak_request 重建 UDP 客户端失败: %v\n", err)
			clearPendingSpeakRequestPlayback()
			clearAutoListen()
			setDeviceState(deviceStateIdle)
			releaseAllowChat()
			return
		}
	} else {
		fmt.Println("ℹ️ 复用现有 UDP 链路响应 speak_request")
	}

	setDeviceState(deviceStateSpeaking)
	if err := sendSpeakReady(mqttClient, request.SessionID, reuseExisting); err != nil {
		fmt.Printf("❌ 发送 speak_ready 失败: %v\n", err)
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
	fmt.Printf("从发送音频结束至 LLM 消息 耗时: %d ms\n", time.Now().UnixMilli()-sendAudioEndTs)
}

func handleStt(client mqtt.Client, msg mqtt.Message) {
	fmt.Printf("从发送音频结束至 STT 消息 耗时: %d ms\n", time.Now().UnixMilli()-sendAudioEndTs)
}

func handleTTS(client mqtt.Client, msg mqtt.Message) {
	fmt.Printf("处理 TTS 消息: %s\n", string(msg.Payload()))
	type st struct {
		Type  string `json:"type"`
		State string `json:"state"`
	}
	// TODO: 实现 TTS 状态更新
	var ttsState st
	if err := json.Unmarshal(msg.Payload(), &ttsState); err != nil {
		fmt.Printf("❌ 消息解析错误: %v\n", err)
		return
	}
	fmt.Printf("处理 TTS 消息: %s\n", ttsState)
	if ttsState.Type == "tts" && !firstTts {
		if ttsState.State == "sentence_start" {
			fmt.Printf("从发送音频结束至TTS 开始 耗时: %d ms\n", time.Now().UnixMilli()-sendAudioEndTs)
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
			fmt.Println("转换WAV文件失败:", err)
			return
		}
		fmt.Printf("TTS 结束, 音频数据长度: %d\n", len(pcmDataList))

		if playback, ok := consumePendingSpeakRequestPlayback(); ok {
			if playback.AutoListen {
				if err := enterAutoListen(client, playback.SessionID); err != nil {
					fmt.Printf("❌ speak_request.auto_listen 启动失败: %v\n", err)
					clearAutoListen()
					setDeviceState(deviceStateIdle)
				}
				return
			}
			clearAutoListen()
			setDeviceState(deviceStateIdle)
			fmt.Println("ℹ️ speak_request 播放完成，auto_listen=false，已回到 idle")
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
	fmt.Printf("处理 goodbye 消息: %s\n", string(msg.Payload()))
	clearPendingSpeakRequestPlayback()
	clearAutoListen()
	setDeviceState(deviceStateIdle)
	releaseAllowChat()
}

func enterAutoListen(mqttClient mqtt.Client, sessionID string) error {
	sessionID = resolveActiveSessionID(sessionID)
	if sessionID == "" {
		return fmt.Errorf("auto_listen 缺少有效 session_id")
	}
	if err := sendListenStart(mqttClient, sessionID); err != nil {
		return err
	}
	armAutoListen(sessionID)
	setDeviceState(deviceStateListening)
	fmt.Printf("🎤 speak_request 播放完成，auto_listen=true，进入 listening 状态: session_id=%s\n", sessionID)
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
			fmt.Printf("发送音频数据结束: %d\n", time.Now().UnixMilli())
		//sendListenStop(mqttClient, sessionId)
		fmt.Printf("发送停止消息结束: %d\n", time.Now().UnixMilli())
		sendAudioEndTs = time.Now().UnixMilli()
	*/

	return nil
}

// 读取WAV文件并使用Opus编码发送
func sendWavFileWithOpusEncoding(udpInstance *UDPClient, filePath string) error {
	sampleRate := audioRate
	channels := 1
	// 打开WAV文件
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("打开WAV文件失败: %v", err)
	}
	defer file.Close()

	// 读取文件内容
	fileContent, err := io.ReadAll(file)
	if err != nil {
		return fmt.Errorf("读取文件内容失败: %v", err)
	}
	fmt.Printf("文件内容长度: %d\n", len(fileContent))
	file.Close()

	opusFrames, err := WavToOpus(fileContent, sampleRate, channels, 0)
	if err != nil {
		return fmt.Errorf("转换WAV文件失败: %v", err)
	}

	fmt.Printf("开始发送音频数据\n", len(opusFrames))

	for i, frame := range opusFrames {
		fmt.Printf("Opus帧 %d 长度: %d\n", i, len(frame))
		// 发送Opus帧
		if err := udpInstance.SendAudioData(frame); err != nil {
			return fmt.Errorf("发送Opus帧失败: %v", err)
		}
		// 控制发送速率，模拟实时音频流
		time.Sleep(60 * time.Millisecond)
	}
	fmt.Printf("总共发送: %d 个帧\n", len(opusFrames))

	//持续发送空的音频数据
	/*emptyFrame := make([]byte, 50)
	for {
		if err := conn.WriteMessage(websocket.BinaryMessage, emptyFrame); err != nil {
			return fmt.Errorf("发送空音频数据失败: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}*/

	return nil
}

// ClientMessage 表示客户端消息
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

// ClientMessage 表示客户端消息
type IotClientMessage struct {
	Type        string   `json:"type"`
	SessionID   string   `json:"session_id"`
	Descriptors []string `json:"descriptors"`
}

// ClientMessage 表示客户端消息
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
	fmt.Println("📤 发布消息to topic:", "", string(jsonData))

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
	fmt.Println("📤 发布消息to topic:", "", string(jsonData))

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
	fmt.Println("📤 发布 speak_ready 到 topic:", serverConfig.MQTT.PublishTopic, string(jsonData))

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
	fmt.Println("📤 发布消息to topic:", "", string(jsonData))

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
	fmt.Println("📤 发布消息to topic:", "", string(jsonData))

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
	fmt.Println("📤 发布消息to topic:", "", string(jsonData))

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
	fmt.Println("📤 发布消息to topic:", "", string(jsonData))
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
		"instruct_text":  "你好",
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
		return "", nil, fmt.Errorf("不支持的tts provider: %s, 可选: cosyvoice|edge|edge_offline|indextts_vllm", providerName)
	}
}

// 调用tts服务生成语音, 并编码至opus发送至服务端
func sendTextToSpeech(mqttClient mqtt.Client) error {
	providerName, providerConfig, err := getTTSProviderConfig()
	if err != nil {
		return err
	}
	fmt.Printf("使用 TTS provider: %s\n", providerName)

	//调用tts服务生成语音
	ttsProvider, err := tts.GetTTSProvider(providerName, providerConfig)
	if err != nil {
		return fmt.Errorf("获取tts服务失败(provider=%s): %v", providerName, err)
	}

	/*
		audioData, err := ttsProvider.TextToSpeech(context.Background(), "你叫什么名字?")
		if err != nil {
			fmt.Printf("生成语音失败: %v\n", err)
			return fmt.Errorf("生成语音失败: %v", err)
		}
	*/

	opusData = make([][]byte, 0)

	var audioCtx context.Context
	var audioCancel context.CancelFunc

	genAndSendAudio := func(ctx context.Context, msg string, count int) error {
		sessionID := getCurrentSessionID()
		if sessionID == "" {
			return fmt.Errorf("当前 session_id 为空，无法发送音频")
		}
		udpInstance := getCurrentUDPClient()
		if udpInstance == nil {
			return fmt.Errorf("当前 UDP 客户端未初始化")
		}

		firstAudio = false
		firstTts = false
		opusData = make([][]byte, 0)
		setDeviceState(deviceStateConversation)
		autoListenArmed := consumeAutoListen(sessionID)
		if autoListenArmed {
			fmt.Printf("🎙️ 复用 auto_listen 已开启的 listening 状态: session_id=%s\n", sessionID)
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
			//fmt.Printf("生成语音失败: %v\n", err)
			return fmt.Errorf("生成语音失败: %v", err)
		}

		for audioData := range audioChan {
			select {
			case <-ctx.Done():
				return nil
			default:
			}
			fmt.Printf("生成语音数据长度: %d\n", len(audioData))
			if err := udpInstance.SendAudioData(audioData); err != nil {
				return fmt.Errorf("发送 UDP 音频失败: %v", err)
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

	// 新增：等待用户输入文本
	reader := bufio.NewReader(os.Stdin)

	f := func() bool {
		fmt.Print("请输入要合成的文本（回车发送，直接回车退出）：")
		input, err := reader.ReadString('\n')
		if err != nil {
			fmt.Printf("读取输入失败: %v\n", err)
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
			fmt.Printf("❌ 发送测试音频失败: %v\n", err)
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

	//genAndSendAudio("你好", 100)
	//time.Sleep(30 * time.Second)
	/*genAndSendAudio("再来一个", 20)
	time.Sleep(30 * time.Second)
	genAndSendAudio("你今天穿的衣服真好看", 20)
	time.Sleep(30 * time.Second)
	genAndSendAudio("明天准备穿什么", 20)
	time.Sleep(30 * time.Second)*/

	return nil
}
