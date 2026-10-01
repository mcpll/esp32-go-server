package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"xiaozhi-esp32-server-golang/internal/domain/audio"
	"xiaozhi-esp32-server-golang/internal/domain/tts"
	"xiaozhi-esp32-server-golang/internal/util"

	"github.com/gorilla/websocket"
)

var detectStartTs int64
var waitInput = make(chan struct{}, 1)
var status = "idle"

const defaultDetectText = "你好小智"

// Message type constants
const (
	MessageTypeHello   = "hello"
	MessageTypeListen  = "listen"
	MessageTypeAbort   = "abort"
	MessageTypeIot     = "iot"
	MessageTypeMcp     = "mcp"
	MessageTypeGoodBye = "goodbye"
)

// Server message type constants
const (
	ServerMessageTypeSTT  = "stt"
	ServerMessageTypeTTS  = "tts"
	ServerMessageTypeLLM  = "llm"
	ServerMessageTypeText = "text"
)

// Message status constants
const (
	MessageStateStart         = "start"
	MessageStateStop          = "stop"
	MessageStateDetect        = "detect"
	MessageStateSuccess       = "success"
	MessageStateError         = "error"
	MessageStateAbort         = "abort"
	MessageStateSentenceStart = "sentence_start"
	MessageStateSentenceEnd   = "sentence_end"
)

// ClientMessage is a client message
type ClientMessage struct {
	Type           string               `json:"type"`
	DeviceID       string               `json:"device_id,omitempty"`
	SessionID      string               `json:"session_id,omitempty"`
	Text           string               `json:"text,omitempty"`
	Mode           string               `json:"mode,omitempty"`
	State          string               `json:"state,omitempty"`
	Token          string               `json:"token,omitempty"`
	DeviceMac      string               `json:"device_mac,omitempty"`
	Version        int                  `json:"version,omitempty"`
	Transport      string               `json:"transport,omitempty"`
	AudioParams    *AudioFormat         `json:"audio_params,omitempty"`
	SpeakUDPConfig *SpeakReadyUDPConfig `json:"udp_config,omitempty"`
	Features       map[string]bool      `json:"features,omitempty"`
	PayLoad        json.RawMessage      `json:"payload,omitempty"`
}

// ServerMessage is a server message
type ServerMessage struct {
	Type        string              `json:"type"`
	Text        string              `json:"text,omitempty"`
	State       string              `json:"state,omitempty"`
	SessionID   string              `json:"session_id,omitempty"`
	Version     int                 `json:"version,omitempty"`
	Transport   string              `json:"transport,omitempty"`
	AudioFormat *AudioFormat        `json:"audio_params,omitempty"`
	AutoListen  *bool               `json:"auto_listen,omitempty"`
	Udp         *UDPTransportConfig `json:"udp,omitempty"`
	PayLoad     json.RawMessage     `json:"payload,omitempty"`
}

type UDPTransportConfig struct {
	Server string `json:"server"`
	Port   int    `json:"port"`
	Key    string `json:"key"`
	Nonce  string `json:"nonce"`
}

type SpeakReadyUDPConfig struct {
	Ready         bool `json:"ready"`
	ReuseExisting bool `json:"reuse_existing,omitempty"`
}

// AudioFormat describes audio format
type AudioFormat struct {
	SampleRate    int    `json:"sample_rate"`
	Channels      int    `json:"channels"`
	FrameDuration int    `json:"frame_duration"`
	Format        string `json:"format"`
}

// Opus encoding constants
var (
	// Opus sample rate
	SampleRate = 16000
	// Audio channel count
	Channels = 1
	// Frame duration (ms)
	FrameDurationMs = 20
	// PCM buffer size = sampleRate * channels * frameDuration(sec)
	PCMBufferSize = SampleRate * Channels * FrameDurationMs / 1000

	mode = "auto"

	addMcp = false
)

const (
	LocalModeAuto1    = "auto1"
	LocalModeAuto2    = "auto2"
	LocalModeManual   = "manual"
	LocalModeRealtime = "realtime"
)

var speectText = "你好测试"
var clientId = "e4b0c442-98fc-4e1b-8c3d-6a5b6a5b6a6d"
var token = "test-token"
var ttsProviderName = "edge_offline"

// serverVisionURL vision API URL parsed from server MCP initialize
var (
	serverVisionURL   string
	serverVisionURLMu sync.RWMutex
)

// parseAndSaveVisionURL parses and saves params.capabilities.vision.url from MCP initialize payload
func parseAndSaveVisionURL(payload json.RawMessage) {
	var mcpMsg struct {
		Method string `json:"method"`
		Params struct {
			Capabilities struct {
				Vision struct {
					URL string `json:"url"`
				} `json:"vision"`
			} `json:"capabilities"`
		} `json:"params"`
	}
	if err := json.Unmarshal(payload, &mcpMsg); err != nil {
		return
	}
	if mcpMsg.Method == "initialize" && mcpMsg.Params.Capabilities.Vision.URL != "" {
		serverVisionURLMu.Lock()
		serverVisionURL = mcpMsg.Params.Capabilities.Vision.URL
		serverVisionURLMu.Unlock()
		fmt.Printf("已保存服务器下发的 vision_url: %s\n", serverVisionURL)
	}
}

// GetServerVisionURL returns server vision URL, or empty if not sent
func GetServerVisionURL() string {
	serverVisionURLMu.RLock()
	defer serverVisionURLMu.RUnlock()
	return serverVisionURL
}

func resetSignals() {
	drainSignal(waitInput)
	serverVisionURLMu.Lock()
	serverVisionURL = ""
	serverVisionURLMu.Unlock()
}

func main() {
	// Parse command-line flags
	serverAddr := flag.String("server", "ws://localhost:8989/xiaozhi/v1/", "服务器地址")
	deviceID := flag.String("device", "test-device-001", "设备ID")
	audioFile := flag.String("audio", "", "音频文件路径")
	text := flag.String("text", "你好测试", "文本")
	runnerFlag := flag.String("runner", "manual", "运行方式(manual|auto)")
	modeFlag := flag.String("mode", LocalModeAuto1, "本地模式(auto1|auto2|manual|realtime，auto会映射到auto1)")
	casesFlag := flag.String("cases", "all", "自动化测试用例(all|manual_roundtrip,auto1_roundtrip,auto2_roundtrip,realtime_roundtrip,hello_metadata,injected_message_skip_llm,iot_roundtrip,tts_sentence_boundaries,manual_multi_turn,mcp_initialize,hello_without_mcp_no_initialize,mcp_duplicate_hello_no_reinitialize,agent_ws_endpoint_mcp,agent_ws_endpoint_mcp_keepalive,invalid_hello_missing_audio_params,invalid_hello_unsupported_transport,duplicate_hello_rehandshake,listen_before_hello_ignored,abort_after_listen_start,abort_during_tts,realtime_interrupt,realtime_listen_stop,realtime_duplicate_start_ignored,goodbye_then_resume,ota_metadata,ota_activate_invalid_algorithm,ota_activate_invalid_challenge_if_required,mqtt_udp_hello,mqtt_udp_injected_message)")
	caseTimeoutFlag := flag.Duration("case_timeout", 20*time.Second, "自动化单用例超时时间")
	turnsFlag := flag.Int("turns", 1, "自动化测试每个用例发言轮次")
	ttsProviderFlag := flag.String("tts_provider", "edge_offline", "TTS provider (edge_offline|edge|cosyvoice)")
	sampleRate := flag.Int("sample_rate", 16000, "sampleRate")
	frameDurationsMs := flag.Int("frame_ms", 20, "frame duration ms")
	addMcpFlag := flag.Bool("mcp", false, "是否启用mcp")
	endpointAuthTokenFlag := flag.String("endpoint_auth_token", defaultAgentEndpointAuthToken, "智能体 WebSocket MCP endpoint JWT 签名密钥")

	flag.Parse()

	fmt.Printf("运行小智客户端\n服务器: %s\n设备ID: %s\n音频文件: %s\n",
		*serverAddr, *deviceID, *audioFile)

	speectText = *text
	SampleRate = *sampleRate
	FrameDurationMs = *frameDurationsMs
	normalizedMode, err := normalizeLocalMode(*modeFlag)
	if err != nil {
		log.Fatalf("invalid mode: %v", err)
	}
	mode = normalizedMode
	runnerMode = strings.ToLower(strings.TrimSpace(*runnerFlag))
	autoCasesFilter = strings.TrimSpace(*casesFlag)
	autoCaseTimeout = *caseTimeoutFlag
	autoTurns = *turnsFlag
	ttsProviderName = strings.TrimSpace(*ttsProviderFlag)
	addMcp = *addMcpFlag
	agentEndpointAuthToken = strings.TrimSpace(*endpointAuthTokenFlag)

	if strings.TrimSpace(*modeFlag) != mode {
		fmt.Printf("本地模式 %s 已映射为 %s\n", strings.TrimSpace(*modeFlag), mode)
	}
	fmt.Printf("运行方式: %s\n本地策略模式: %s, 协议 listen.mode: %s\n", runnerMode, mode, protocolMode())

	if runnerMode == "auto" {
		if err := runAutomationSuite(*serverAddr, *deviceID, *audioFile); err != nil {
			log.Fatalf("auto test failed: %v", err)
		}
		return
	}
	if runnerMode != "manual" {
		log.Fatalf("unsupported run mode: %s, options: manual|auto", runnerMode)
	}

	// Run the client
	if err := runClient(*serverAddr, *deviceID, *audioFile, nil); err != nil {
		log.Fatalf("client run failed: %v", err)
	}
}

var OpusData [][]byte
var firstRecvFrame bool

// runClient runs the Xiaozhi client
func runClient(serverAddr, deviceID, audioFile string, testCase *protocolTestCase) error {
	OpusData = [][]byte{}
	// Build WebSocket URL
	wsURL := serverAddr
	fmt.Printf("正在连接服务器: %s\n", wsURL)

	// Connect to WebSocket server
	conn, _, err := dialServer(wsURL, deviceID)
	if err != nil {
		return err
	}
	defer conn.Close()
	runtime := startSessionRuntime(conn, deviceID)

	fmt.Println("已连接到服务器")

	// Send hello message
	effectiveAddMcp := addMcp || (testCase != nil && testCase.EnableMCP)
	if err := sendHello(runtime, deviceID, effectiveAddMcp, "websocket", defaultAudioFormat()); err != nil {
		return fmt.Errorf("发送hello消息失败: %v", err)
	}
	if err := waitForHelloAck(runtime, defaultHelloTimeout); err != nil {
		return fmt.Errorf("hello 握手失败: %v", err)
	}
	fmt.Println("hello 握手完成")

	if testCase != nil {
		switch testCase.Kind {
		case protocolCaseMCP, protocolCaseNoMCP, protocolCaseMCPDuplicateHello, protocolCaseAbort, protocolCaseHelloMetadata, protocolCaseIot:
			return runProtocolTestCase(runtime, testCase, nil)
		}
	}

	// If audio file given, send it
	if audioFile != "" {
		if err := sendListenStart(runtime, protocolMode()); err != nil {
			return fmt.Errorf("发送listen start消息失败: %v", err)
		}
		fmt.Printf("已发送 listen start %s 信令\n", protocolMode())

		// Short wait so server is ready for audio
		time.Sleep(100 * time.Millisecond)

		fmt.Println("开始发送音频数据...")
		// Read and send audio file (Opus-encoded)
		if err := sendWavFileWithOpusEncoding(conn, audioFile); err != nil {
			return fmt.Errorf("发送音频数据失败: %v\n", err)
		}
		fmt.Println("音频数据发送完成，等待服务器响应...")
		// Wait 10 seconds then exit
		time.Sleep(10 * time.Second)
		return nil
	}

	// If no audio file, use TTS mode
	if err := sendTextToSpeech(runtime, testCase); err != nil {
		return fmt.Errorf("发送文本到语音失败: %v", err)
	}

	return nil
}

func saveOpusData() error {
	f, err := os.Create("opus_ws.data")
	if err != nil {
		return err
	}
	defer f.Close()

	for _, data := range OpusData {
		f.Write(data)
	}

	f.Close()

	return nil
}

func startSessionRuntime(conn *websocket.Conn, deviceID string) *sessionRuntime {
	runtime := newSessionRuntime(conn, deviceID)
	mcpSendMsgChan := make(chan []byte, 10)
	mcpRecvMsgChan := make(chan []byte, 10)

	go func() {
		for msg := range mcpSendMsgChan {
			fmt.Printf("发送mcp消息: %s\n", string(msg))
			runtime.recordOutgoingMCP(msg)
			if err := runtime.writeText(msg); err != nil {
				fmt.Printf("发送mcp消息失败: %v\n", err)
				return
			}
		}
	}()

	go func() {
		ttsReceiving := false
		for {
			messageType, message, err := conn.ReadMessage()
			if err != nil {
				fmt.Printf("读取消息失败: %v\n", err)
				return
			}

			if messageType == websocket.TextMessage {
				fmt.Printf("收到服务器消息: %+v\n", string(message))
				var serverMsg ServerMessage
				if err := json.Unmarshal(message, &serverMsg); err != nil {
					fmt.Printf("解析消息失败: %v\n", err)
					continue
				}
				runtime.recordIncomingMessage(serverMsg)

				if serverMsg.Type == MessageTypeMcp {
					if len(serverMsg.PayLoad) > 0 {
						parseAndSaveVisionURL(serverMsg.PayLoad)
					}
					runtime.recordIncomingMCP(serverMsg)
					select {
					case mcpRecvMsgChan <- serverMsg.PayLoad:
					default:
						fmt.Printf("mcp消息队列已满, 丢弃消息: %s\n", string(serverMsg.PayLoad))
					}
				}

				if serverMsg.Type == MessageTypeHello {
					runtime.notifyHelloAck(serverMsg)
				}

				if serverMsg.Type == MessageTypeIot {
					runtime.notifyIot(serverMsg)
				}

				if serverMsg.Type == ServerMessageTypeSTT {
					runtime.notifySTT(serverMsg)
				}

				if serverMsg.Type == ServerMessageTypeLLM || serverMsg.Type == ServerMessageTypeText {
					runtime.notifyOutput(serverMsg)
				}

				if serverMsg.Type == ServerMessageTypeTTS {
					switch serverMsg.State {
					case MessageStateStart:
						ttsReceiving = true
						OpusData = [][]byte{}
						firstRecvFrame = false
						runtime.notifyTTSStart()
						fmt.Println("收到 tts start，准备接收音频")
					case MessageStateStop:
						ttsReceiving = false
						runtime.notifyTTSStop()
						fmt.Println("收到 tts stop")
						if err := handleTTSStopForStrategy(runtime); err != nil {
							fmt.Printf("处理 tts stop 失败: %v\n", err)
						}
					case MessageStateSentenceStart, MessageStateSentenceEnd:
						if strings.TrimSpace(serverMsg.Text) != "" {
							runtime.notifyOutput(serverMsg)
						}
					}
				}
			} else if messageType == websocket.BinaryMessage {
				runtime.recordIncomingBinary(len(message), ttsReceiving)
				if !ttsReceiving {
					continue
				}
				if !firstRecvFrame {
					firstRecvFrame = true
					fmt.Printf("首帧到达时间: %d 毫秒\n", time.Now().UnixMilli()-detectStartTs)
				}
				OpusData = append(OpusData, message)
			}
		}
	}()

	go func() {
		NewMcpServer(mcpSendMsgChan, mcpRecvMsgChan)
	}()

	return runtime
}

func defaultAudioFormat() *AudioFormat {
	return &AudioFormat{
		SampleRate:    SampleRate,
		Channels:      Channels,
		FrameDuration: FrameDurationMs,
		Format:        "opus",
	}
}

func buildHelloMessage(deviceID string, enableMCP bool, transport string, audioParams *AudioFormat) ClientMessage {
	msg := ClientMessage{
		Type:        MessageTypeHello,
		DeviceID:    deviceID,
		Transport:   transport,
		Version:     1,
		Features:    map[string]bool{},
		AudioParams: audioParams,
	}
	if enableMCP {
		msg.Features["mcp"] = true
	}
	return msg
}

func sendHello(runtime *sessionRuntime, deviceID string, enableMCP bool, transport string, audioParams *AudioFormat) error {
	return sendJSONMessage(runtime, buildHelloMessage(deviceID, enableMCP, transport, audioParams))
}

func sendListenStart(runtime *sessionRuntime, mode string) error {
	// Send listen start message
	listenStartMsg := ClientMessage{
		Type:     MessageTypeListen,
		DeviceID: runtime.deviceID,
		State:    MessageStateStart,
		Mode:     mode,
	}

	if err := sendJSONMessage(runtime, listenStartMsg); err != nil {
		return fmt.Errorf("发送listen start消息失败: %v", err)
	}
	return nil
}

func sendListenStop(runtime *sessionRuntime) error {
	// Send listen start message
	listenStartMsg := ClientMessage{
		Type:     MessageTypeListen,
		DeviceID: runtime.deviceID,
		State:    MessageStateStop,
		Mode:     "manual",
	}

	if err := sendJSONMessage(runtime, listenStartMsg); err != nil {
		return fmt.Errorf("发送listen stop消息失败: %v", err)
	}

	return nil
}

func sendAbort(runtime *sessionRuntime) error {
	// Send listen start message
	listenStartMsg := ClientMessage{
		Type:     MessageTypeAbort,
		DeviceID: runtime.deviceID,
	}

	if err := sendJSONMessage(runtime, listenStartMsg); err != nil {
		return fmt.Errorf("发送listen start消息失败: %v", err)
	}
	return nil
}

func sendIot(runtime *sessionRuntime, text string) error {
	msg := ClientMessage{
		Type:     MessageTypeIot,
		DeviceID: runtime.deviceID,
		Text:     text,
	}
	if err := sendJSONMessage(runtime, msg); err != nil {
		return fmt.Errorf("发送iot消息失败: %v", err)
	}
	return nil
}

func sendGoodbye(runtime *sessionRuntime) error {
	msg := ClientMessage{
		Type:     MessageTypeGoodBye,
		DeviceID: runtime.deviceID,
	}
	if err := sendJSONMessage(runtime, msg); err != nil {
		return fmt.Errorf("发送goodbye消息失败: %v", err)
	}
	return nil
}

func sendListenDetect(runtime *sessionRuntime, text string) error {
	// Send listen start message
	listenStartMsg := ClientMessage{
		Type:     MessageTypeListen,
		DeviceID: runtime.deviceID,
		State:    MessageStateDetect,
		Text:     text,
	}

	if err := sendJSONMessage(runtime, listenStartMsg); err != nil {
		return fmt.Errorf("发送listen detect消息失败: %v", err)
	}
	return nil
}

func normalizeLocalMode(raw string) (string, error) {
	localMode := strings.ToLower(strings.TrimSpace(raw))
	switch localMode {
	case "", "auto":
		return LocalModeAuto1, nil
	case LocalModeAuto1, LocalModeAuto2, LocalModeManual, LocalModeRealtime:
		return localMode, nil
	default:
		return "", fmt.Errorf("不支持的模式: %s, 可选: auto1|auto2|manual|realtime", raw)
	}
}

func protocolMode() string {
	switch mode {
	case LocalModeAuto1, LocalModeAuto2:
		return "auto"
	default:
		return mode
	}
}

func allowNextInput() {
	select {
	case waitInput <- struct{}{}:
	default:
	}
}

func sendInitialListenSequence(runtime *sessionRuntime) error {
	switch mode {
	case LocalModeAuto1:
		fmt.Println("本地策略 auto1: 初始发送 listen detect，等待欢迎语结束后进入 listen start auto")
		if err := sendListenDetect(runtime, defaultDetectText); err != nil {
			return err
		}
		played, err := waitForOptionalTTSPlayback(runtime.ttsStartCh, runtime.ttsStopCh, runtime.outputCh, 2*time.Second, autoCaseTimeout, "auto1 welcome")
		if err != nil {
			return err
		}
		if !played {
			if err := sendListenStart(runtime, protocolMode()); err != nil {
				return err
			}
		}
	case LocalModeAuto2:
		fmt.Println("本地策略 auto2: 初始发送 listen start auto -> listen detect，等待欢迎语结束")
		if err := sendListenStart(runtime, protocolMode()); err != nil {
			return err
		}
		if err := sendListenDetect(runtime, defaultDetectText); err != nil {
			return err
		}
		if _, err := waitForOptionalTTSPlayback(runtime.ttsStartCh, runtime.ttsStopCh, runtime.outputCh, 2*time.Second, autoCaseTimeout, "auto2 welcome"); err != nil {
			return err
		}
	case LocalModeRealtime:
		fmt.Println("本地策略 realtime: 初始发送 listen detect，等待欢迎语结束后 listen start realtime")
		if err := sendListenDetect(runtime, defaultDetectText); err != nil {
			return err
		}
		if _, err := waitForOptionalTTSPlayback(runtime.ttsStartCh, runtime.ttsStopCh, runtime.outputCh, 2*time.Second, autoCaseTimeout, "realtime welcome"); err != nil {
			return err
		}
		if err := sendListenStart(runtime, protocolMode()); err != nil {
			return err
		}
	}
	return nil
}

func handleTTSStopForStrategy(runtime *sessionRuntime) error {
	switch mode {
	case LocalModeAuto1, LocalModeAuto2:
		if err := sendListenStart(runtime, protocolMode()); err != nil {
			return err
		}
		fmt.Printf("本地策略 %s: tts stop 后重新发送 listen start %s\n", mode, protocolMode())
		allowNextInput()
	case LocalModeRealtime:
		fmt.Println("本地策略 realtime: tts stop 后不做额外处理")
	case LocalModeManual:
		allowNextInput()
	default:
		allowNextInput()
	}
	return nil
}

// Send JSON message
func sendJSONMessage(runtime *sessionRuntime, msg interface{}) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	fmt.Printf("发送消息: %s\n", string(data))
	if clientMsg, ok := msg.(ClientMessage); ok {
		runtime.recordOutgoingMessage(clientMsg)
	}
	return runtime.writeText(data)
}

// Read WAV and send Opus-encoded
func sendWavFileWithOpusEncoding(conn *websocket.Conn, filePath string) error {
	// Open WAV file
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("打开WAV文件失败: %v", err)
	}
	defer file.Close()

	// Read file contents
	fileContent, err := io.ReadAll(file)
	if err != nil {
		return fmt.Errorf("读取文件内容失败: %v", err)
	}
	fmt.Printf("文件内容长度: %d\n", len(fileContent))
	file.Close()

	opusFrames, err := util.WavToOpus(fileContent, SampleRate, Channels, 0)
	if err != nil {
		return fmt.Errorf("转换WAV文件失败: %v", err)
	}

	fmt.Printf("转换后的Opus帧数: %d\n", len(opusFrames))

	for i, frame := range opusFrames {
		fmt.Printf("Opus帧 %d 长度: %d\n", i, len(frame))
		// Send Opus frame
		if err := conn.WriteMessage(websocket.BinaryMessage, frame); err != nil {
			return fmt.Errorf("发送Opus帧失败: %v", err)
		}
		// Pace sends to simulate realtime audio
		time.Sleep(time.Duration(FrameDurationMs) * time.Millisecond)
	}

	// Send 200ms of silence
	silenceDurationMs := 1000
	silenceFrameCount := silenceDurationMs / FrameDurationMs
	fmt.Printf("开始发送 %dms 静音音频数据，共 %d 帧\n", silenceDurationMs, silenceFrameCount)

	// Generate silent Opus data
	emptyOpusData := genEmptyOpusData(SampleRate, Channels, FrameDurationMs, 1)
	if emptyOpusData == nil {
		return fmt.Errorf("生成静音Opus数据失败")
	}

	// Loop sending silence frames
	for i := 0; i < silenceFrameCount; i++ {
		if err := conn.WriteMessage(websocket.BinaryMessage, emptyOpusData); err != nil {
			return fmt.Errorf("发送静音Opus帧失败: %v", err)
		}
		// Pace sends to simulate realtime audio
		time.Sleep(time.Duration(FrameDurationMs) * time.Millisecond)
	}
	fmt.Printf("静音音频数据发送完成\n")

	return nil
}

// Read and send audio file (raw; no Opus)
func sendAudioFile(conn *websocket.Conn, filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("打开音频文件失败: %v", err)
	}
	defer file.Close()

	// Read file and send in chunks
	// Read and send one fixed-size chunk at a time
	const chunkSize = 4096
	buffer := make([]byte, chunkSize)

	for {
		n, err := file.Read(buffer)
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("读取音频数据失败: %v", err)
		}

		if n > 0 {
			// Send binary audio data
			if err := conn.WriteMessage(websocket.BinaryMessage, buffer[:n]); err != nil {
				return fmt.Errorf("发送音频数据失败: %v", err)
			}

			// Pace sends to simulate realtime audio
			time.Sleep(100 * time.Millisecond)
		}
	}

	return nil
}

func genEmptyOpusData(sampleRate int, channels int, frameDurationMs int, count int) []byte {
	audioProcesser, err := audio.GetAudioProcesser(sampleRate, channels, frameDurationMs)
	if err != nil {
		return nil
	}

	frameSize := sampleRate * channels * frameDurationMs / 1000

	pcmFrame := make([]int16, frameSize)
	opusFrame := make([]byte, 1000)

	n, err := audioProcesser.Encoder(pcmFrame, opusFrame)
	if err != nil {
		return nil
	}

	tmp := make([]byte, n)
	copy(tmp, opusFrame)
	return tmp
}

// Call TTS to synthesize speech, Opus-encode, send to server
func sendTextToSpeech(runtime *sessionRuntime, testCase *protocolTestCase) error {
	cosyVoiceConfig := map[string]interface{}{
		"api_url":        "https://tts.linkerai.cn/tts",
		"spk_id":         "OUeAo1mhq6IBExi",
		"frame_duration": FrameDurationMs,
		"target_sr":      SampleRate,
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
	providerName := strings.TrimSpace(ttsProviderName)
	var providerConfig map[string]interface{}
	switch providerName {
	case "edge_offline":
		providerConfig = edgeOfflineConfig
	case "edge":
		providerConfig = edgeConfig
	case "cosyvoice":
		providerConfig = cosyVoiceConfig
	default:
		return fmt.Errorf("不支持的tts provider: %s, 可选: edge_offline|edge|cosyvoice", providerName)
	}
	fmt.Printf("使用 TTS provider: %s\n", providerName)
	ttsProvider, err := tts.GetTTSProvider(providerName, providerConfig)
	if err != nil {
		return fmt.Errorf("获取tts服务失败(provider=%s): %v", providerName, err)
	}

	/*
		audioData, err := ttsProvider.TextToSpeech(context.Background(), "What is your name?")
		if err != nil {
			fmt.Printf("speech synthesis failed: %v\n", err)
			return fmt.Errorf("speech synthesis failed: %v", err)
		}
	}*/

	emptyOpusData := genEmptyOpusData(SampleRate, 1, FrameDurationMs, 1000)

	genAndSendAudio := func(msg string, count int) error {
		audioChan, err := ttsProvider.TextToSpeechStream(context.Background(), msg, SampleRate, 1, FrameDurationMs)
		if err != nil {
			fmt.Printf("生成语音失败: %v\n", err)
			return fmt.Errorf("生成语音失败: %v", err)
		}

		for audioData := range audioChan {
			//fmt.Printf("sent audio data length: %d\n", len(audioData))
			if err := runtime.writeBinary(audioData); err != nil {
				return fmt.Errorf("发送语音帧失败: %v", err)
			}
			time.Sleep(time.Duration(FrameDurationMs) * time.Millisecond)
		}

		detectStartTs = time.Now().UnixMilli()

		for i := 0; i <= count; i++ {
			if err := runtime.writeBinary(emptyOpusData); err != nil {
				return fmt.Errorf("发送静音帧失败: %v", err)
			}
			time.Sleep(time.Duration(FrameDurationMs) * time.Millisecond)
		}

		return nil
	}

	if err := sendInitialListenSequence(runtime); err != nil {
		return fmt.Errorf("发送初始 listen 序列失败: %v", err)
	}
	if mode != LocalModeRealtime {
		allowNextInput()
	}

	// Added: wait for user text input
	reader := bufio.NewReader(os.Stdin)

	var stopEmptyOpusChan = make(chan struct{})
	var resumeChan = make(chan struct{})
	go func() {
		if mode == LocalModeRealtime {
			// Keep sending emptyOpusData until stop signal
			for {
				select {
				case <-stopEmptyOpusChan:
					resumeChan <- struct{}{}
					<-resumeChan
				default:
					if err := runtime.writeBinary(emptyOpusData); err != nil {
						fmt.Printf("发送 realtime 静音帧失败: %v\n", err)
						return
					}
					time.Sleep(time.Duration(FrameDurationMs) * time.Millisecond)
				}
			}
		}
	}()

	runTurn := func(input string) error {
		if mode == LocalModeRealtime {
			stopEmptyOpusChan <- struct{}{}
			<-resumeChan
		}
		if mode == LocalModeManual {
			if err := sendListenStart(runtime, protocolMode()); err != nil {
				allowNextInput()
				return fmt.Errorf("发送 listen start 失败: %v", err)
			}
		}
		if err := genAndSendAudio(input, 100); err != nil {
			if mode == LocalModeRealtime {
				resumeChan <- struct{}{}
			}
			return err
		}
		if mode == LocalModeManual {
			if err := sendListenStop(runtime); err != nil {
				return fmt.Errorf("发送 listen stop 失败: %v", err)
			}
		}
		if mode == LocalModeRealtime {
			resumeChan <- struct{}{}
		}
		return nil
	}

	if testCase != nil {
		return runProtocolTestCase(runtime, testCase, runTurn)
	}

	for {

		fmt.Print("请输入要合成的文本（回车发送，直接回车退出）：")
		input, err := reader.ReadString('\n')
		if err != nil {
			fmt.Printf("读取输入失败: %v\n", err)
			continue
		}
		input = strings.TrimSpace(input)
		if input == "" {
			// Send abort
			sendAbort(runtime)
			allowNextInput()
			status = "idle"
			continue
		}
		f := func() {
			if err := runTurn(input); err != nil {
				fmt.Printf("发送音频失败: %v\n", err)
			}
		}
		if mode == LocalModeRealtime {
			go f()
			continue
		}
		select {
		case <-waitInput:
			go f()
		}
	}

	return nil
}
