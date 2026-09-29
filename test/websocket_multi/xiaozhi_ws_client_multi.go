package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"xiaozhi-esp32-server-golang/internal/domain/tts"

	"github.com/go-audio/audio"
	"github.com/go-audio/wav"
	"github.com/gorilla/websocket"
	"gopkg.in/hraban/opus.v2"
)

const (
	MessageTypeHello  = "hello"
	MessageTypeListen = "listen"
	MessageTypeAbort  = "abort"
	MessageTypeIot    = "iot"
)

const (
	MessageStateStart   = "start"
	MessageStateStop    = "stop"
	MessageStateDetect  = "detect"
	MessageStateSuccess = "success"
	MessageStateError   = "error"
	MessageStateAbort   = "abort"
)

type ClientMessage struct {
	Type        string       `json:"type"`
	DeviceID    string       `json:"device_id"`
	Text        string       `json:"text,omitempty"`
	Mode        string       `json:"mode,omitempty"`
	State       string       `json:"state,omitempty"`
	Token       string       `json:"token,omitempty"`
	DeviceMac   string       `json:"device_mac,omitempty"`
	Version     int          `json:"version,omitempty"`
	Transport   string       `json:"transport,omitempty"`
	AudioParams *AudioFormat `json:"audio_params,omitempty"`
}

type ServerMessage struct {
	Type        string       `json:"type"`
	Text        string       `json:"text,omitempty"`
	State       string       `json:"state,omitempty"`
	SessionID   string       `json:"session_id,omitempty"`
	Transport   string       `json:"transport,omitempty"`
	AudioFormat *AudioFormat `json:"audio_format,omitempty"`
}

type AudioFormat struct {
	SampleRate    int    `json:"sample_rate"`
	Channels      int    `json:"channels"`
	FrameDuration int    `json:"frame_duration"`
	Format        string `json:"format"`
}

const (
	SampleRate      = 16000
	Channels        = 1
	FrameDurationMs = 60
)

type WsClient struct {
	DeviceId          string
	ClientId          string
	Token             string
	ServerAddr        string
	Conn              *websocket.Conn
	ListenMode        string
	firstRecvFrame    bool
	detectStartTs     int64
	index             int
	audioOpusDataChan chan AudioOpusData
	metricsWriter     *MetricsWriter
}

var lock sync.RWMutex
var totalRequest int64
var avgResponseMs int64

type MetricEvent struct {
	TimestampMs int64  `json:"timestamp_ms"`
	ClientIndex int    `json:"client_index"`
	DeviceID    string `json:"device_id"`
	Event       string `json:"event"`
	LatencyMs   int64  `json:"latency_ms"`
	Detail      string `json:"detail,omitempty"`
}

type MetricsWriter struct {
	mu   sync.Mutex
	file *os.File
}

func NewMetricsWriter(path string) (*MetricsWriter, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	return &MetricsWriter{file: f}, nil
}

func (m *MetricsWriter) Close() error {
	if m == nil || m.file == nil {
		return nil
	}
	return m.file.Close()
}

func (m *MetricsWriter) WriteEvent(e MetricEvent) {
	if m == nil || m.file == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	line, err := json.Marshal(e)
	if err != nil {
		return
	}
	_, _ = m.file.Write(append(line, '\n'))
}

func main() {
	serverAddr := flag.String("server", "ws://localhost:8989/xiaozhi/v1/", "服务器地址")
	clientCount := flag.Int("count", 10, "客户端数量")
	chatText := flag.String("text", "你好", "聊天内容, 多句以逗号分隔会依次发送")
	deviceId := flag.String("device", "", "设备ID")
	listenMode := flag.String("listen_mode", "manual", "listen模式: manual/auto/realtime")
	audioWav := flag.String("audio_wav", "", "预置wav文件路径(逗号分隔)，启用后不调用云端TTS生成测试音频")
	rampMs := flag.Int("ramp_ms", 0, "启动客户端间隔毫秒，避免瞬时建连抖动")
	metricsJSONL := flag.String("metrics_jsonl", "", "指标输出文件(JSONL)")
	flag.Parse()

	fmt.Printf("运行小智客户端\n服务器: %s\n客户端数量: %d\n发送内容: %s\n", *serverAddr, *clientCount, *chatText)

	metricsWriter, err := NewMetricsWriter(*metricsJSONL)
	if err != nil {
		fmt.Printf("创建metrics输出失败: %v\n", err)
		return
	}
	defer func() {
		if metricsWriter != nil {
			_ = metricsWriter.Close()
		}
	}()

	textList := strings.Split(*chatText, ",")
	audioOpusDataList, err := genAudioOpusDataList(textList, *audioWav)
	if err != nil {
		fmt.Printf("生成音频数据失败: %v\n", err)
		return
	}

	for i := 0; i < *clientCount; i++ {
		idx := i
		go func() {
			client := &WsClient{
				ServerAddr:        *serverAddr,
				ListenMode:        *listenMode,
				index:             idx,
				audioOpusDataChan: make(chan AudioOpusData, 2),
				DeviceId:          *deviceId,
				metricsWriter:     metricsWriter,
			}

			if err := client.runClient(audioOpusDataList); err != nil {
				log.Printf("客户端运行失败(index=%d): %v", idx, err)
			}
		}()
		if *rampMs > 0 {
			time.Sleep(time.Duration(*rampMs) * time.Millisecond)
		}
	}

	go func() {
		for {
			time.Sleep(2 * time.Second)
			lock.Lock()
			fmt.Printf("请求%d次, 平均响应时间: %d 毫秒\n", totalRequest, avgResponseMs)
			lock.Unlock()
		}
	}()

	select {}
}

func (w *WsClient) runClient(audioOpusDataList []AudioOpusData) error {
	if len(audioOpusDataList) == 0 {
		return fmt.Errorf("音频数据列表为空")
	}
	fmt.Printf("%d 客户端开始运行\n", w.index)

	if w.DeviceId == "" {
		w.DeviceId = genDeviceId()
	}
	w.ClientId = genClientId()

	header := http.Header{}
	header.Set("Device-Id", w.DeviceId)
	header.Set("Content-Type", "application/json")
	header.Set("Authorization", "Bearer "+w.Token)
	header.Set("Protocol-Version", "1")
	header.Set("Client-Id", w.ClientId)

	var err error
	w.Conn, _, err = websocket.DefaultDialer.Dial(w.ServerAddr, header)
	if err != nil {
		return fmt.Errorf("连接失败: %v", err)
	}
	defer w.Conn.Close()

	fmt.Printf("%d 客户端已连接到服务器: %s\n", w.index, w.ServerAddr)

	audioDataIndex := 0
	go func() {
		for {
			messageType, message, err := w.Conn.ReadMessage()
			if err != nil {
				log.Printf("读取消息失败(index=%d): %v", w.index, err)
				return
			}

			if messageType == websocket.TextMessage {
				var serverMsg ServerMessage
				if err := json.Unmarshal(message, &serverMsg); err != nil {
					log.Printf("解析消息失败(index=%d): %v", w.index, err)
					continue
				}

				if serverMsg.Type == "hello" || (serverMsg.Type == "tts" && serverMsg.State == "stop") {
					if serverMsg.Type == "tts" && serverMsg.State == "stop" {
						w.metricsWriter.WriteEvent(MetricEvent{
							TimestampMs: time.Now().UnixMilli(),
							ClientIndex: w.index,
							DeviceID:    w.DeviceId,
							Event:       "tts_stop",
							LatencyMs:   time.Now().UnixMilli() - w.detectStartTs,
						})
					}
					if audioDataIndex >= len(audioOpusDataList) {
						audioDataIndex = 0
					}
					w.audioOpusDataChan <- audioOpusDataList[audioDataIndex]
					audioDataIndex++
				}
				continue
			}

			if messageType == websocket.BinaryMessage && !w.firstRecvFrame {
				w.firstRecvFrame = true
				diffMs := time.Now().UnixMilli() - w.detectStartTs
				lock.Lock()
				totalRequest++
				if avgResponseMs == 0 {
					avgResponseMs = diffMs
				} else {
					avgResponseMs = (avgResponseMs + diffMs) / 2
				}
				lock.Unlock()

				w.metricsWriter.WriteEvent(MetricEvent{
					TimestampMs: time.Now().UnixMilli(),
					ClientIndex: w.index,
					DeviceID:    w.DeviceId,
					Event:       "first_frame",
					LatencyMs:   diffMs,
				})
			}
		}
	}()

	if err := w.sendHello(); err != nil {
		return err
	}
	return w.sendAudioDataToServer()
}

func (w *WsClient) sendHello() error {
	helloMsg := ClientMessage{
		Type:      MessageTypeHello,
		DeviceID:  w.DeviceId,
		Transport: "websocket",
		Version:   1,
		AudioParams: &AudioFormat{
			SampleRate:    SampleRate,
			Channels:      Channels,
			FrameDuration: FrameDurationMs,
			Format:        "opus",
		},
	}
	if err := sendJSONMessage(w.Conn, helloMsg); err != nil {
		return fmt.Errorf("发送hello消息失败: %v", err)
	}
	return nil
}

func (w *WsClient) sendListenStart() error {
	listenStartMsg := ClientMessage{Type: MessageTypeListen, DeviceID: w.DeviceId, State: MessageStateStart, Mode: w.listenMode()}
	if err := sendJSONMessage(w.Conn, listenStartMsg); err != nil {
		return fmt.Errorf("发送listen start消息失败: %v", err)
	}
	return nil
}

func (w *WsClient) sendListenStop() error {
	listenStopMsg := ClientMessage{Type: MessageTypeListen, DeviceID: w.DeviceId, State: MessageStateStop, Mode: w.listenMode()}
	if err := sendJSONMessage(w.Conn, listenStopMsg); err != nil {
		return fmt.Errorf("发送listen stop消息失败: %v", err)
	}
	w.detectStartTs = time.Now().UnixMilli()
	w.firstRecvFrame = false
	return nil
}

func (w *WsClient) listenMode() string {
	if strings.TrimSpace(w.ListenMode) == "" {
		return "manual"
	}
	return w.ListenMode
}

func sendJSONMessage(conn *websocket.Conn, msg interface{}) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return conn.WriteMessage(websocket.TextMessage, data)
}

func genDeviceId() string {
	rand.Seed(time.Now().UnixNano())
	mac := make([]byte, 6)
	rand.Read(mac)
	return fmt.Sprintf("%02X:%02X:%02X:%02X:%02X:%02X", mac[0], mac[1], mac[2], mac[3], mac[4], mac[5])
}

func genClientId() string {
	return "e4b0c442-98fc-4e1b-8c3d-6a5b6a5b6a6e"
}

type AudioOpusData struct {
	OpusData [][]byte
	Duration int
}

func genAudioOpusDataList(textList []string, wavPaths string) ([]AudioOpusData, error) {
	if strings.TrimSpace(wavPaths) != "" {
		return genAudioOpusDataFromWav(wavPaths)
	}

	cosyVoiceConfig := map[string]interface{}{
		"api_url":        "https://tts.linkerai.cn/tts",
		"spk_id":         "OUeAo1mhq6IBExi",
		"frame_duration": FrameDurationMs,
		"target_sr":      SampleRate,
		"audio_format":   "mp3",
		"instruct_text":  "你好",
	}

	ttsProvider, err := tts.GetTTSProvider("cosyvoice", cosyVoiceConfig)
	if err != nil {
		return nil, fmt.Errorf("获取tts服务失败: %v", err)
	}

	ret := []AudioOpusData{}
	for _, text := range textList {
		audioData := AudioOpusData{}
		var audioChan chan []byte

		for i := 0; i < 3; i++ {
			audioChan, err = ttsProvider.TextToSpeechStream(context.Background(), text, SampleRate, 1, FrameDurationMs)
			if err != nil {
				fmt.Printf("生成语音失败: %v\n", err)
				continue
			}
			break
		}

		for perOpusData := range audioChan {
			audioData.OpusData = append(audioData.OpusData, perOpusData)
		}
		ret = append(ret, audioData)
	}

	return ret, nil
}

func genAudioOpusDataFromWav(wavPaths string) ([]AudioOpusData, error) {
	paths := strings.Split(wavPaths, ",")
	ret := make([]AudioOpusData, 0, len(paths))
	for _, p := range paths {
		path := strings.TrimSpace(p)
		if path == "" {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("读取wav失败(%s): %w", path, err)
		}
		opusData, err := wavToOpus(data, SampleRate, Channels, 64000)
		if err != nil {
			return nil, fmt.Errorf("wav转opus失败(%s): %w", path, err)
		}
		ret = append(ret, AudioOpusData{OpusData: opusData})
	}
	if len(ret) == 0 {
		return nil, fmt.Errorf("未加载到任何有效wav输入")
	}
	return ret, nil
}

func wavToOpus(wavData []byte, sampleRate int, channels int, bitRate int) ([][]byte, error) {
	wavReader := bytes.NewReader(wavData)
	wavDecoder := wav.NewDecoder(wavReader)
	if !wavDecoder.IsValidFile() {
		return nil, fmt.Errorf("无效WAV文件")
	}
	wavDecoder.ReadInfo()
	format := wavDecoder.Format()

	if sampleRate <= 0 {
		sampleRate = int(format.SampleRate)
	}
	if channels <= 0 {
		channels = int(format.NumChannels)
	}

	enc, err := opus.NewEncoder(sampleRate, channels, opus.AppAudio)
	if err != nil {
		return nil, fmt.Errorf("创建opus编码器失败: %w", err)
	}
	if bitRate > 0 {
		if err := enc.SetBitrate(bitRate); err != nil {
			return nil, fmt.Errorf("设置opus码率失败: %w", err)
		}
	}

	frameSize := sampleRate * FrameDurationMs / 1000
	pcmBuffer := make([]int16, frameSize*channels)
	opusBuffer := make([]byte, 2000)
	audioBuf := &audio.IntBuffer{Data: make([]int, frameSize*channels), Format: format}

	frames := make([][]byte, 0, 64)
	for {
		n, err := wavDecoder.PCMBuffer(audioBuf)
		if err == io.EOF || n == 0 {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("读取wav数据失败: %w", err)
		}
		for i := range pcmBuffer {
			if i < len(audioBuf.Data) {
				pcmBuffer[i] = int16(audioBuf.Data[i])
			} else {
				pcmBuffer[i] = 0
			}
		}
		encN, err := enc.Encode(pcmBuffer, opusBuffer)
		if err != nil {
			return nil, fmt.Errorf("opus编码失败: %w", err)
		}
		frame := make([]byte, encN)
		copy(frame, opusBuffer[:encN])
		frames = append(frames, frame)
	}
	return frames, nil
}

func (w *WsClient) sendAudioDataToServer() error {
	for {
		audioOpusData := <-w.audioOpusDataChan
		if err := w.sendListenStart(); err != nil {
			return err
		}
		for _, opusData := range audioOpusData.OpusData {
			if err := w.Conn.WriteMessage(websocket.BinaryMessage, opusData); err != nil {
				return fmt.Errorf("发送Opus帧失败: %v", err)
			}
			time.Sleep(FrameDurationMs * time.Millisecond)
		}
		if err := w.sendListenStop(); err != nil {
			return err
		}
	}
}
