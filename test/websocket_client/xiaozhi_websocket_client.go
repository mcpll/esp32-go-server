package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
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

// message type constants
const (
	MessageTypeHello  = "hello"
	MessageTypeListen = "listen"
	MessageTypeAbort  = "abort"
	MessageTypeIot    = "iot"
	MessageTypeMcp    = "mcp"
)

// message state constants
const (
	MessageStateStart   = "start"
	MessageStateStop    = "stop"
	MessageStateDetect  = "detect"
	MessageStateSuccess = "success"
	MessageStateError   = "error"
	MessageStateAbort   = "abort"
)

// ClientMessage represents a client message
type ClientMessage struct {
	Type        string          `json:"type"`
	DeviceID    string          `json:"device_id"`
	Text        string          `json:"text,omitempty"`
	Mode        string          `json:"mode,omitempty"`
	State       string          `json:"state,omitempty"`
	Token       string          `json:"token,omitempty"`
	DeviceMac   string          `json:"device_mac,omitempty"`
	Version     int             `json:"version,omitempty"`
	Transport   string          `json:"transport,omitempty"`
	AudioParams *AudioFormat    `json:"audio_params,omitempty"`
	Features    map[string]bool `json:"features,omitempty"`
	PayLoad     json.RawMessage `json:"payload,omitempty"`
}

// ServerMessage represents a server message
type ServerMessage struct {
	Type        string          `json:"type"`
	Text        string          `json:"text,omitempty"`
	State       string          `json:"state,omitempty"`
	SessionID   string          `json:"session_id,omitempty"`
	Transport   string          `json:"transport,omitempty"`
	AudioFormat *AudioFormat    `json:"audio_format,omitempty"`
	PayLoad     json.RawMessage `json:"payload,omitempty"`
}

// AudioFormat represents audio format
type AudioFormat struct {
	SampleRate    int    `json:"sample_rate"`
	Channels      int    `json:"channels"`
	FrameDuration int    `json:"frame_duration"`
	Format        string `json:"format"`
}

// Opus encoding constants
var (
	// Opus encoding sample rate
	SampleRate = 16000
	// audio channel count
	Channels = 1
	// frame duration (ms)
	FrameDurationMs = 20
	// PCM buffer size = sample rate * channels * frame duration (seconds)
	PCMBufferSize = SampleRate * Channels * FrameDurationMs / 1000

	mode = "auto"

	addMcp = false
)

var speectText = "你好测试"
var clientId = "e4b0c442-98fc-4e1b-8c3d-6a5b6a5b6a6d"
var token = "test-token"
var ttsProviderName = "edge_offline"

// serverVisionURL is the vision API URL parsed from the server MCP initialize message
var (
	serverVisionURL   string
	serverVisionURLMu sync.RWMutex
)

// parseAndSaveVisionURL parses params.capabilities.vision.url from the MCP initialize payload and saves it
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

// GetServerVisionURL returns the vision API URL from the server, or empty if none was sent
func GetServerVisionURL() string {
	serverVisionURLMu.RLock()
	defer serverVisionURLMu.RUnlock()
	return serverVisionURL
}

func main() {
	// parse command-line flags
	serverAddr := flag.String("server", "ws://localhost:8989/xiaozhi/v1/", "服务器地址")
	deviceID := flag.String("device", "test-device-001", "设备ID")
	audioFile := flag.String("audio", "", "音频文件路径")
	text := flag.String("text", "你好测试", "文本")
	modeFlag := flag.String("mode", "auto", "模式")
	ttsProviderFlag := flag.String("tts_provider", "edge_offline", "TTS provider (edge_offline|edge|cosyvoice)")
	sampleRate := flag.Int("sample_rate", 16000, "sampleRate")
	frameDurationsMs := flag.Int("frame_ms", 20, "frame duration ms")
	addMcpFlag := flag.Bool("mcp", false, "是否启用mcp")

	flag.Parse()

	fmt.Printf("运行小智客户端\n服务器: %s\n设备ID: %s\n音频文件: %s\n",
		*serverAddr, *deviceID, *audioFile)

	speectText = *text
	SampleRate = *sampleRate
	FrameDurationMs = *frameDurationsMs
	mode = *modeFlag
	ttsProviderName = strings.TrimSpace(*ttsProviderFlag)
	addMcp = *addMcpFlag

	// run client
	if err := runClient(*serverAddr, *deviceID, *audioFile); err != nil {
		log.Fatalf("客户端运行失败: %v", err)
	}
}

var OpusData [][]byte
var firstRecvFrame bool

// runClient runs the Xiaozhi client
func runClient(serverAddr, deviceID, audioFile string) error {
	OpusData = [][]byte{}
	// build WebSocket URL
	wsURL := serverAddr
	fmt.Printf("正在连接服务器: %s\n", wsURL)

	// set HTTP headers
	header := http.Header{}
	header.Set("Device-Id", deviceID)
	header.Set("Content-Type", "application/json")
	header.Set("Authorization", "Bearer "+token)
	header.Set("Protocol-Version", "1")
	header.Set("Client-Id", clientId)

	// connect to the WebSocket server
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		return fmt.Errorf("连接失败: %v", err)
	}
	defer conn.Close()

	fmt.Println("已连接到服务器")

	mcpSendMsgChan := make(chan []byte, 10)
	mcpRecvMsgChan := make(chan []byte, 10)

	go func() {
		for msg := range mcpSendMsgChan {
			fmt.Printf("发送mcp消息: %s\n", string(msg))
			/*respMsg := ClientMessage{
				Type:     MessageTypeMcp,
				DeviceID: deviceID,
				PayLoad:  msg,
			}
			jsonByte, _ := json.Marshal(respMsg)*/
			conn.WriteMessage(websocket.TextMessage, msg)
		}
	}()

	// set message handling
	done := make(chan struct{})

	var startTs int64
	_ = startTs
	// start a goroutine to handle messages from the server
	go func() {
		var iLock sync.Mutex
		defer close(done)
		//var recvInterval int64
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

				if serverMsg.Type == "mcp" {
					// parse vision_url from the server MCP initialize message
					if len(serverMsg.PayLoad) > 0 {
						parseAndSaveVisionURL(serverMsg.PayLoad)
					}
					select {
					case mcpRecvMsgChan <- serverMsg.PayLoad:
					default:
						fmt.Printf("mcp消息队列已满, 丢弃消息: %s\n", string(serverMsg.PayLoad))
					}
				}

				if serverMsg.Type == "tts" && serverMsg.State == "stop" {
					//OpusToWav(OpusData, 24000, 1, "ws_output_24000.wav")
					select {
					case waitInput <- struct{}{}:
					default:
					}
				}
			} else if messageType == websocket.BinaryMessage {
				if !firstRecvFrame {
					iLock.Lock()
					if !firstRecvFrame {
						firstRecvFrame = true
						fmt.Printf("首帧到达时间: %d 毫秒\n", time.Now().UnixMilli()-detectStartTs)
					}
					iLock.Unlock()
					//os.WriteFile("ws_output_first_frame.wav", message, 0644)
				}
				OpusData = append(OpusData, message)
				//fmt.Printf("received audio data: %d bytes, interval: %d ms\n", len(message), time.Now().UnixMilli()-recvInterval)
				//recvInterval = time.Now().UnixMilli()
			}
		}
	}()

	go func() {
		NewMcpServer(mcpSendMsgChan, mcpRecvMsgChan)
	}()

	// send hello message
	helloMsg := ClientMessage{
		Type:      MessageTypeHello,
		DeviceID:  deviceID,
		Transport: "websocket",
		Version:   1,
		Features:  map[string]bool{},
		AudioParams: &AudioFormat{
			SampleRate:    SampleRate,
			Channels:      Channels,
			FrameDuration: FrameDurationMs,
			Format:        "opus",
		},
	}

	if addMcp {
		helloMsg.Features["mcp"] = true
	}

	if err := sendJSONMessage(conn, helloMsg); err != nil {
		return fmt.Errorf("发送hello消息失败: %v", err)
	}

	// wait for the server response
	time.Sleep(1 * time.Second)

	// if an audio file is specified, send it
	if audioFile != "" {
		// send listen start auto signal
		if err := sendListenStart(conn, deviceID, "auto"); err != nil {
			return fmt.Errorf("发送listen start消息失败: %v", err)
		}
		fmt.Println("已发送 listen start auto 信令")

		// brief wait so the server is ready for audio
		time.Sleep(100 * time.Millisecond)

		fmt.Println("开始发送音频数据...")
		// read and send an audio file (Opus-encoded)
		if err := sendWavFileWithOpusEncoding(conn, audioFile); err != nil {
			return fmt.Errorf("发送音频数据失败: %v\n", err)
		}
		fmt.Println("音频数据发送完成，等待服务器响应...")
		// exit after waiting 10 seconds
		time.Sleep(10 * time.Second)
		return nil
	}

	// if no audio file is given, use TTS mode
	waitInput <- struct{}{}
	if err := sendTextToSpeech(conn, deviceID); err != nil {
		return fmt.Errorf("发送文本到语音失败: %v", err)
	}
	/*
		for i := 0; i < 1; i++ {
			detectStartTs = time.Now().UnixMilli()
			if err := sendListenDetect(conn, deviceID, speectText); err != nil {
				return fmt.Errorf("failed to send text: %v", err)
			}
			time.Sleep(5000 * time.Millisecond)
		}

		saveOpusData()
		OpusToWav(OpusData, 24000, 1, "ws_output_24000.wav")
	*/

	time.Sleep(100 * time.Millisecond)
	// send listen stop message
	/*listenStopMsg := ClientMessage{
		Type:     MessageTypeListen,
		DeviceID: deviceID,
		State:    MessageStateStop,
	}

	if err := sendJSONMessage(conn, listenStopMsg); err != nil {
		return fmt.Errorf("failed to send listen stop message: %v", err)
	}*/

	fmt.Println("已发送停止消息，等待服务器响应...")

	// wait a while to receive the server response
	time.Sleep(30 * time.Second)

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

func sendListenStart(conn *websocket.Conn, deviceID string, mode string) error {
	// send listen start message
	listenStartMsg := ClientMessage{
		Type:     MessageTypeListen,
		DeviceID: deviceID,
		State:    MessageStateStart,
		Mode:     mode,
	}

	if err := sendJSONMessage(conn, listenStartMsg); err != nil {
		return fmt.Errorf("发送listen start消息失败: %v", err)
	}
	return nil
}

func sendListenStop(conn *websocket.Conn, deviceID string) error {
	// send listen start message
	listenStartMsg := ClientMessage{
		Type:     MessageTypeListen,
		DeviceID: deviceID,
		State:    MessageStateStop,
		Mode:     "manual",
	}

	if err := sendJSONMessage(conn, listenStartMsg); err != nil {
		return fmt.Errorf("发送listen stop消息失败: %v", err)
	}

	return nil
}

func sendAbort(conn *websocket.Conn, deviceID string) error {
	// send listen start message
	listenStartMsg := ClientMessage{
		Type:     MessageTypeAbort,
		DeviceID: deviceID,
	}

	if err := sendJSONMessage(conn, listenStartMsg); err != nil {
		return fmt.Errorf("发送listen start消息失败: %v", err)
	}
	return nil
}

func sendListenDetect(conn *websocket.Conn, deviceID string, text string) error {
	// send listen start message
	listenStartMsg := ClientMessage{
		Type:     MessageTypeListen,
		DeviceID: deviceID,
		State:    MessageStateDetect,
		Text:     text,
	}

	if err := sendJSONMessage(conn, listenStartMsg); err != nil {
		return fmt.Errorf("发送listen detect消息失败: %v", err)
	}
	return nil
}

// send JSON message
func sendJSONMessage(conn *websocket.Conn, msg interface{}) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	fmt.Printf("发送消息: %s\n", string(data))
	return conn.WriteMessage(websocket.TextMessage, data)
}

// read a WAV file and send it Opus-encoded
func sendWavFileWithOpusEncoding(conn *websocket.Conn, filePath string) error {
	// open WAV file
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("打开WAV文件失败: %v", err)
	}
	defer file.Close()

	// read file contents
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
		// send Opus frame
		if err := conn.WriteMessage(websocket.BinaryMessage, frame); err != nil {
			return fmt.Errorf("发送Opus帧失败: %v", err)
		}
		// pace sends to simulate a real-time audio stream
		time.Sleep(time.Duration(FrameDurationMs) * time.Millisecond)
	}

	// send 200ms of silence audio
	silenceDurationMs := 1000
	silenceFrameCount := silenceDurationMs / FrameDurationMs
	fmt.Printf("开始发送 %dms 静音音频数据，共 %d 帧\n", silenceDurationMs, silenceFrameCount)

	// generate silent Opus data
	emptyOpusData := genEmptyOpusData(SampleRate, Channels, FrameDurationMs, 1)
	if emptyOpusData == nil {
		return fmt.Errorf("生成静音Opus数据失败")
	}

	// loop sending silence frames
	for i := 0; i < silenceFrameCount; i++ {
		if err := conn.WriteMessage(websocket.BinaryMessage, emptyOpusData); err != nil {
			return fmt.Errorf("发送静音Opus帧失败: %v", err)
		}
		// pace sends to simulate a real-time audio stream
		time.Sleep(time.Duration(FrameDurationMs) * time.Millisecond)
	}
	fmt.Printf("静音音频数据发送完成\n")

	return nil
}

// read and send an audio file (raw; no Opus encoding)
func sendAudioFile(conn *websocket.Conn, filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("打开音频文件失败: %v", err)
	}
	defer file.Close()

	// read the file and send it in chunks
	// read and send fixed-size chunks
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
			// send binary audio data
			if err := conn.WriteMessage(websocket.BinaryMessage, buffer[:n]); err != nil {
				return fmt.Errorf("发送音频数据失败: %v", err)
			}

			// pace sends to simulate a real-time audio stream
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

// call TTS to synthesize speech, encode to Opus, and send to the server
func sendTextToSpeech(conn *websocket.Conn, deviceID string) error {
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
		audioData, err := ttsProvider.TextToSpeech(context.Background(), "what is your name?")
		if err != nil {
			fmt.Printf("TTS failed: %v\n", err)
			return fmt.Errorf("TTS failed: %v", err)
		}
	*/

	emptyOpusData := genEmptyOpusData(SampleRate, 1, FrameDurationMs, 1000)

	genAndSendAudio := func(msg string, count int) error {
		audioChan, err := ttsProvider.TextToSpeechStream(context.Background(), msg, SampleRate, 1, FrameDurationMs)
		if err != nil {
			fmt.Printf("生成语音失败: %v\n", err)
			return fmt.Errorf("生成语音失败: %v", err)
		}

		for audioData := range audioChan {
			//fmt.Printf("sent audio data length: %d\n", len(audioData))
			conn.WriteMessage(websocket.BinaryMessage, audioData)
			time.Sleep(time.Duration(FrameDurationMs) * time.Millisecond)
		}

		detectStartTs = time.Now().UnixMilli()

		for i := 0; i <= count; i++ {
			conn.WriteMessage(websocket.BinaryMessage, emptyOpusData)
			time.Sleep(time.Duration(FrameDurationMs) * time.Millisecond)
		}

		firstRecvFrame = false
		return nil
	}

	//send detect message
	sendListenDetect(conn, deviceID, "你好小智")

	// also: wait for user text input
	reader := bufio.NewReader(os.Stdin)

	var stopEmptyOpusChan = make(chan struct{})
	var resumeChan = make(chan struct{})
	go func() {
		if mode == "realtime" {
			//keep sending emptyOpusData until a stop signal
			for {
				select {
				case <-stopEmptyOpusChan:
					resumeChan <- struct{}{}
					<-resumeChan
				default:
					conn.WriteMessage(websocket.BinaryMessage, emptyOpusData)
					time.Sleep(time.Duration(FrameDurationMs) * time.Millisecond)
				}
			}
		}
	}()

	for {

		fmt.Print("请输入要合成的文本（回车发送，直接回车退出）：")
		input, err := reader.ReadString('\n')
		if err != nil {
			fmt.Printf("读取输入失败: %v\n", err)
			continue
		}
		input = strings.TrimSpace(input)
		if input == "" {
			//send abort
			sendAbort(conn, deviceID)
			select {
			case waitInput <- struct{}{}:
				status = "idle"
			default:
			}
			continue
		}
		f := func() {
			if mode == "realtime" {
				stopEmptyOpusChan <- struct{}{}
				<-resumeChan
			}
			sendListenStart(conn, deviceID, mode)
			genAndSendAudio(input, 100)
			if mode == "manual" {
				sendListenStop(conn, deviceID)
			}
			if mode == "realtime" {
				resumeChan <- struct{}{}
			}
		}
		if mode == "realtime" {
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
