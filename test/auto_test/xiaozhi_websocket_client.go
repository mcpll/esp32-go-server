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

const defaultDetectText = "你好小智" // Chinese kept: wake word sent on the wire

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

var speectText = "你好测试" // Chinese kept: text synthesized by the Chinese default voice
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
		fmt.Printf("saved vision_url sent by the server: %s\n", serverVisionURL)
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
	serverAddr := flag.String("server", "ws://localhost:8989/xiaozhi/v1/", "server address")
	deviceID := flag.String("device", "test-device-001", "device ID")
	audioFile := flag.String("audio", "", "audio file path")
	text := flag.String("text", "你好测试", "text") // Chinese kept: default text for the Chinese default voice
	runnerFlag := flag.String("runner", "manual", "runner (manual|auto)")
	modeFlag := flag.String("mode", LocalModeAuto1, "local mode (auto1|auto2|manual|realtime; auto maps to auto1)")
	casesFlag := flag.String("cases", "all", "automated test cases (all|manual_roundtrip,auto1_roundtrip,auto2_roundtrip,realtime_roundtrip,hello_metadata,injected_message_skip_llm,iot_roundtrip,tts_sentence_boundaries,manual_multi_turn,mcp_initialize,hello_without_mcp_no_initialize,mcp_duplicate_hello_no_reinitialize,agent_ws_endpoint_mcp,agent_ws_endpoint_mcp_keepalive,invalid_hello_missing_audio_params,invalid_hello_unsupported_transport,duplicate_hello_rehandshake,listen_before_hello_ignored,abort_after_listen_start,abort_during_tts,realtime_interrupt,realtime_listen_stop,realtime_duplicate_start_ignored,goodbye_then_resume,ota_metadata,ota_activate_invalid_algorithm,ota_activate_invalid_challenge_if_required,mqtt_udp_hello,mqtt_udp_injected_message)")
	caseTimeoutFlag := flag.Duration("case_timeout", 20*time.Second, "timeout per automated case")
	turnsFlag := flag.Int("turns", 1, "speech turns per automated case")
	ttsProviderFlag := flag.String("tts_provider", "edge_offline", "TTS provider (edge_offline|edge|cosyvoice)")
	sampleRate := flag.Int("sample_rate", 16000, "sampleRate")
	frameDurationsMs := flag.Int("frame_ms", 20, "frame duration ms")
	addMcpFlag := flag.Bool("mcp", false, "enable mcp")
	endpointAuthTokenFlag := flag.String("endpoint_auth_token", defaultAgentEndpointAuthToken, "JWT signing key for the agent WebSocket MCP endpoint")

	flag.Parse()

	fmt.Printf("Running xiaozhi client\nserver: %s\ndevice ID: %s\naudio file: %s\n",
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
		fmt.Printf("local mode %s mapped to %s\n", strings.TrimSpace(*modeFlag), mode)
	}
	fmt.Printf("runner: %s\nlocal strategy mode: %s, protocol listen.mode: %s\n", runnerMode, mode, protocolMode())

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
	fmt.Printf("connecting to server: %s\n", wsURL)

	// Connect to WebSocket server
	conn, _, err := dialServer(wsURL, deviceID)
	if err != nil {
		return err
	}
	defer conn.Close()
	runtime := startSessionRuntime(conn, deviceID)

	fmt.Println("connected to server")

	// Send hello message
	effectiveAddMcp := addMcp || (testCase != nil && testCase.EnableMCP)
	if err := sendHello(runtime, deviceID, effectiveAddMcp, "websocket", defaultAudioFormat()); err != nil {
		return fmt.Errorf("failed to send hello message: %v", err)
	}
	if err := waitForHelloAck(runtime, defaultHelloTimeout); err != nil {
		return fmt.Errorf("hello handshake failed: %v", err)
	}
	fmt.Println("hello handshake done")

	if testCase != nil {
		switch testCase.Kind {
		case protocolCaseMCP, protocolCaseNoMCP, protocolCaseMCPDuplicateHello, protocolCaseAbort, protocolCaseHelloMetadata, protocolCaseIot:
			return runProtocolTestCase(runtime, testCase, nil)
		}
	}

	// If audio file given, send it
	if audioFile != "" {
		if err := sendListenStart(runtime, protocolMode()); err != nil {
			return fmt.Errorf("failed to send listen start message: %v", err)
		}
		fmt.Printf("sent listen start %s message\n", protocolMode())

		// Short wait so server is ready for audio
		time.Sleep(100 * time.Millisecond)

		fmt.Println("sending audio data...")
		// Read and send audio file (Opus-encoded)
		if err := sendWavFileWithOpusEncoding(conn, audioFile); err != nil {
			return fmt.Errorf("failed to send audio data: %v\n", err)
		}
		fmt.Println("audio data sent, waiting for server response...")
		// Wait 10 seconds then exit
		time.Sleep(10 * time.Second)
		return nil
	}

	// If no audio file, use TTS mode
	if err := sendTextToSpeech(runtime, testCase); err != nil {
		return fmt.Errorf("failed to send text to speech: %v", err)
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
			fmt.Printf("sending mcp message: %s\n", string(msg))
			runtime.recordOutgoingMCP(msg)
			if err := runtime.writeText(msg); err != nil {
				fmt.Printf("failed to send mcp message: %v\n", err)
				return
			}
		}
	}()

	go func() {
		ttsReceiving := false
		for {
			messageType, message, err := conn.ReadMessage()
			if err != nil {
				fmt.Printf("failed to read message: %v\n", err)
				return
			}

			if messageType == websocket.TextMessage {
				fmt.Printf("received server message: %+v\n", string(message))
				var serverMsg ServerMessage
				if err := json.Unmarshal(message, &serverMsg); err != nil {
					fmt.Printf("failed to parse message: %v\n", err)
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
						fmt.Printf("mcp message queue full, dropping message: %s\n", string(serverMsg.PayLoad))
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
						fmt.Println("received tts start, ready to receive audio")
					case MessageStateStop:
						ttsReceiving = false
						runtime.notifyTTSStop()
						fmt.Println("received tts stop")
						if err := handleTTSStopForStrategy(runtime); err != nil {
							fmt.Printf("failed to handle tts stop: %v\n", err)
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
					fmt.Printf("first frame arrived after %d ms\n", time.Now().UnixMilli()-detectStartTs)
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
		return fmt.Errorf("failed to send listen start message: %v", err)
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
		return fmt.Errorf("failed to send listen stop message: %v", err)
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
		return fmt.Errorf("failed to send listen start message: %v", err)
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
		return fmt.Errorf("failed to send iot message: %v", err)
	}
	return nil
}

func sendGoodbye(runtime *sessionRuntime) error {
	msg := ClientMessage{
		Type:     MessageTypeGoodBye,
		DeviceID: runtime.deviceID,
	}
	if err := sendJSONMessage(runtime, msg); err != nil {
		return fmt.Errorf("failed to send goodbye message: %v", err)
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
		return fmt.Errorf("failed to send listen detect message: %v", err)
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
		return "", fmt.Errorf("unsupported mode: %s, choose one of: auto1|auto2|manual|realtime", raw)
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
		fmt.Println("local strategy auto1: send listen detect first, enter listen start auto after the welcome speech ends")
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
		fmt.Println("local strategy auto2: send listen start auto -> listen detect first, wait for the welcome speech to end")
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
		fmt.Println("local strategy realtime: send listen detect first, listen start realtime after the welcome speech ends")
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
		fmt.Printf("local strategy %s: send listen start %s again after tts stop\n", mode, protocolMode())
		allowNextInput()
	case LocalModeRealtime:
		fmt.Println("local strategy realtime: nothing extra after tts stop")
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
	fmt.Printf("sending message: %s\n", string(data))
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

	opusFrames, err := util.WavToOpus(fileContent, SampleRate, Channels, 0)
	if err != nil {
		return fmt.Errorf("failed to convert WAV file: %v", err)
	}

	fmt.Printf("Opus frames after conversion: %d\n", len(opusFrames))

	for i, frame := range opusFrames {
		fmt.Printf("Opus frame %d length: %d\n", i, len(frame))
		// Send Opus frame
		if err := conn.WriteMessage(websocket.BinaryMessage, frame); err != nil {
			return fmt.Errorf("failed to send Opus frame: %v", err)
		}
		// Pace sends to simulate realtime audio
		time.Sleep(time.Duration(FrameDurationMs) * time.Millisecond)
	}

	// Send 200ms of silence
	silenceDurationMs := 1000
	silenceFrameCount := silenceDurationMs / FrameDurationMs
	fmt.Printf("sending %dms of silent audio, %d frames\n", silenceDurationMs, silenceFrameCount)

	// Generate silent Opus data
	emptyOpusData := genEmptyOpusData(SampleRate, Channels, FrameDurationMs, 1)
	if emptyOpusData == nil {
		return fmt.Errorf("failed to generate silent Opus data")
	}

	// Loop sending silence frames
	for i := 0; i < silenceFrameCount; i++ {
		if err := conn.WriteMessage(websocket.BinaryMessage, emptyOpusData); err != nil {
			return fmt.Errorf("failed to send silent Opus frame: %v", err)
		}
		// Pace sends to simulate realtime audio
		time.Sleep(time.Duration(FrameDurationMs) * time.Millisecond)
	}
	fmt.Printf("silent audio data sent\n")

	return nil
}

// Read and send audio file (raw; no Opus)
func sendAudioFile(conn *websocket.Conn, filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open audio file: %v", err)
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
			return fmt.Errorf("failed to read audio data: %v", err)
		}

		if n > 0 {
			// Send binary audio data
			if err := conn.WriteMessage(websocket.BinaryMessage, buffer[:n]); err != nil {
				return fmt.Errorf("failed to send audio data: %v", err)
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
		return fmt.Errorf("unsupported tts provider: %s, choose one of: edge_offline|edge|cosyvoice", providerName)
	}
	fmt.Printf("using TTS provider: %s\n", providerName)
	ttsProvider, err := tts.GetTTSProvider(providerName, providerConfig)
	if err != nil {
		return fmt.Errorf("failed to get tts service (provider=%s): %v", providerName, err)
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
			fmt.Printf("failed to generate speech: %v\n", err)
			return fmt.Errorf("failed to generate speech: %v", err)
		}

		for audioData := range audioChan {
			//fmt.Printf("sent audio data length: %d\n", len(audioData))
			if err := runtime.writeBinary(audioData); err != nil {
				return fmt.Errorf("failed to send speech frame: %v", err)
			}
			time.Sleep(time.Duration(FrameDurationMs) * time.Millisecond)
		}

		detectStartTs = time.Now().UnixMilli()

		for i := 0; i <= count; i++ {
			if err := runtime.writeBinary(emptyOpusData); err != nil {
				return fmt.Errorf("failed to send silent frame: %v", err)
			}
			time.Sleep(time.Duration(FrameDurationMs) * time.Millisecond)
		}

		return nil
	}

	if err := sendInitialListenSequence(runtime); err != nil {
		return fmt.Errorf("failed to send initial listen sequence: %v", err)
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
						fmt.Printf("failed to send realtime silent frame: %v\n", err)
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
				return fmt.Errorf("failed to send listen start: %v", err)
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
				return fmt.Errorf("failed to send listen stop: %v", err)
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

		fmt.Print("Enter the text to synthesize (Enter to send, empty line to quit): ")
		input, err := reader.ReadString('\n')
		if err != nil {
			fmt.Printf("failed to read input: %v\n", err)
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
				fmt.Printf("failed to send audio: %v\n", err)
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
}
