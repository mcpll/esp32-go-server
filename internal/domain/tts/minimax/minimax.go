package minimax

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"xiaozhi-esp32-server-golang/internal/util"
	log "xiaozhi-esp32-server-golang/logger"

	"github.com/gorilla/websocket"
)

// Constants
const (
	wsURL = "wss://api.minimaxi.com/ws/v1/t2a_v2"
)

// Global WebSocket Dialer
var wsDialer = websocket.Dialer{
	ReadBufferSize:   16384, // 16KB read buffer
	WriteBufferSize:  16384, // 16KB write buffer
	HandshakeTimeout: 45 * time.Second,
}

// MinimaxTTSProvider Minimax TTS provider
type MinimaxTTSProvider struct {
	APIKey     string
	Model      string
	Voice      string
	Speed      float64
	Volume     float64
	Pitch      int
	SampleRate int
	Bitrate    int
	Format     string
	Channel    int

	// Connection management
	conn      *websocket.Conn
	connMutex sync.RWMutex
	// Send lock so only one request uses the connection at a time
	sendMutex sync.Mutex
}

// WebSocket message structs
type minimaxMessage struct {
	Event           string        `json:"event,omitempty"`
	Model           string        `json:"model,omitempty"`
	VoiceSetting    *voiceSetting `json:"voice_setting,omitempty"`
	AudioSetting    *audioSetting `json:"audio_setting,omitempty"`
	ContinuousSound bool          `json:"continuous_sound,omitempty"`
	Text            string        `json:"text,omitempty"`
}

type minimaxResp struct {
	SessionId string            `json:"session_id,omitempty"`
	Event     string            `json:"event,omitempty"`
	TraceId   string            `json:"trace_id,omitempty"`
	Data      *minimaxData      `json:"data,omitempty"`
	IsFinal   bool              `json:"is_final,omitempty"`
	BaseResp  *minimaxBaseResp  `json:"base_resp,omitempty"`
	ExtraInfo *minimaxExtraInfo `json:"extra_info,omitempty"`
}

type minimaxExtraInfo struct {
	AudioLength     int    `json:"audio_length"`
	AudioSampleRate int    `json:"audio_sample_rate"`
	AudioDuration   int    `json:"audio_duration"`
	AudioSize       int    `json:"audio_size"`
	Bitrate         int    `json:"bitrate"`
	AudioFormat     string `json:"audio_format"`
	AudioChannel    int    `json:"audio_channel"`

	UsageCharacters int `json:"usage_characters"`
	WordCount       int `json:"word_count"`
}

type minimaxBaseResp struct {
	StatusCode int    `json:"status_code"`
	StatusMsg  string `json:"status_msg"`
}

type voiceSetting struct {
	VoiceID              string  `json:"voice_id"`
	Speed                float64 `json:"speed"`
	Vol                  float64 `json:"vol"`
	Pitch                int     `json:"pitch"`
	EnglishNormalization bool    `json:"english_normalization"`
}

type audioSetting struct {
	SampleRate int    `json:"sample_rate"`
	Bitrate    int    `json:"bitrate"`
	Format     string `json:"format"`
	Channel    int    `json:"channel"`
}

type minimaxData struct {
	Audio string `json:"audio"`
}

// NewMinimaxTTSProvider creates a Minimax TTS provider
func NewMinimaxTTSProvider(config map[string]interface{}) *MinimaxTTSProvider {
	apiKey, _ := config["api_key"].(string)
	model, _ := config["model"].(string)
	voice, _ := config["voice"].(string)
	speed, _ := config["speed"].(float64)
	volume, _ := config["vol"].(float64)
	if volume == 0 {
		volume, _ = config["volume"].(float64)
	}
	pitch, _ := config["pitch"].(float64)
	sampleRate, _ := config["sample_rate"].(float64)
	bitrate, _ := config["bitrate"].(float64)
	format, _ := config["format"].(string)
	channel, _ := config["channel"].(float64)

	// Set defaults
	if model == "" {
		model = "speech-2.8-hd"
	}
	if voice == "" {
		voice = "male-qn-qingse"
	}
	if speed == 0 {
		speed = 1.0
	}
	if volume == 0 {
		volume = 1.0
	}
	if sampleRate == 0 {
		sampleRate = 32000
	}
	if bitrate == 0 {
		bitrate = 128000
	}
	if format == "" {
		format = "mp3"
	}
	if channel == 0 {
		channel = 1
	}

	return &MinimaxTTSProvider{
		APIKey:     apiKey,
		Model:      model,
		Voice:      voice,
		Speed:      speed,
		Volume:     volume,
		Pitch:      int(pitch),
		SampleRate: int(sampleRate),
		Bitrate:    int(bitrate),
		Format:     format,
		Channel:    int(channel),
	}
}

// TextToSpeech one-shot synthesis (unsupported; use streaming)
func (p *MinimaxTTSProvider) TextToSpeech(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) ([][]byte, error) {
	// Minimax is mainly streaming; collect stream data then return
	outputChan, err := p.TextToSpeechStream(ctx, text, sampleRate, channels, frameDuration)
	if err != nil {
		return nil, err
	}

	var frames [][]byte
	for frame := range outputChan {
		frames = append(frames, frame)
	}

	return frames, nil
}

// TextToSpeechStream streaming speech synthesis
func (p *MinimaxTTSProvider) TextToSpeechStream(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) (outputChan chan []byte, err error) {
	startTs := time.Now().UnixMilli()

	// Hold send lock so only one request uses the connection
	p.sendMutex.Lock()
	// Note: release the lock when the goroutine finishes, not on function return

	// Get connection (reuse or create)
	conn, err := p.getConnection(ctx)
	if err != nil {
		p.sendMutex.Unlock()
		return nil, fmt.Errorf("获取WebSocket连接失败: %v", err)
	}

	// Create output channel
	outputChan = make(chan []byte, 100)

	// Create pipe for audio decode
	pipeReader, pipeWriter := io.Pipe()

	// Start audio decoder goroutine
	go func() {
		decoder, err := util.CreateAudioDecoderWithSampleRate(ctx, pipeReader, outputChan, frameDuration, p.Format, sampleRate)
		if err != nil {
			log.Errorf("failed to create audio decoder: %v", err)
			pipeReader.Close()
			close(outputChan)
			return
		}

		if err := decoder.Run(startTs); err != nil {
			log.Errorf("audio decode failed: %v", err)
		}
	}()

	// Use WaitGroup to wait for the read goroutine
	var wg sync.WaitGroup
	wg.Add(1)

	// Start read/process goroutine; release lock via defer in that goroutine on normal end, error, or panic
	go func() {
		defer wg.Done()
		defer p.sendMutex.Unlock()
		defer func() {
			pipeWriter.Close()
			pipeReader.Close()
		}()

		p.processStreamTTS(ctx, conn, text, pipeWriter)
	}()

	// Wait in background for goroutine completion and lock release
	go func() {
		wg.Wait()
		log.Debugf("Minimax TTS stream synthesis done, elapsed: %d ms", time.Now().UnixMilli()-startTs)
	}()

	return outputChan, nil
}

// processStreamTTS runs the streaming TTS flow
func (p *MinimaxTTSProvider) processStreamTTS(ctx context.Context, conn *websocket.Conn, text string, pipeWriter *io.PipeWriter) {
	// Send task-start message
	startMsg := minimaxMessage{
		Event: "task_start",
		Model: p.Model,
		VoiceSetting: &voiceSetting{
			VoiceID:              p.Voice,
			Speed:                p.Speed,
			Vol:                  p.Volume,
			Pitch:                p.Pitch,
			EnglishNormalization: false,
		},
		AudioSetting: &audioSetting{
			SampleRate: p.SampleRate,
			Bitrate:    p.Bitrate,
			Format:     p.Format,
			Channel:    p.Channel,
		},
		ContinuousSound: false,
	}

	log.Debugf("minimax sending task_start: model=%s, voice=%s, format=%s", p.Model, p.Voice, p.Format)
	if err := p.sendMessage(conn, startMsg); err != nil {
		log.Errorf("failed to send task_start: %v", err)
		p.clearConnection()
		return
	}

	// Wait for task-start ack
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	msg, err := p.readMessage(conn)
	if err != nil {
		// Check for timeout error
		if netErr, ok := err.(interface{ Timeout() bool }); ok && netErr.Timeout() {
			log.Errorf("timed out waiting for task_start ack (no response within 10s)")
		} else {
			log.Errorf("failed to read task_start ack: %v", err)
		}
		p.clearConnection()
		return
	}

	log.Debugf("received task_start ack: %+v", msg)

	if msg.Event != "task_started" {
		log.Errorf("task_start failed, expected 'task_started', got: event=%s, message=%+v", msg.Event, msg)
		if msg.BaseResp != nil && msg.BaseResp.StatusCode != 0 {
			log.Errorf("error detail: status_code=%d, status_msg=%s", msg.BaseResp.StatusCode, msg.BaseResp.StatusMsg)
		}
		p.clearConnection()
		return
	}
	// Reset read timeout
	conn.SetReadDeadline(time.Time{})

	log.Debugf("task_start ack ok")

	// Send text message
	continueMsg := minimaxMessage{
		Event: "task_continue",
		Text:  text,
	}

	if err := p.sendMessage(conn, continueMsg); err != nil {
		log.Errorf("failed to send text message: %v", err)
		p.clearConnection()
		return
	}

	// Read audio data
	chunkCount := 0
	for {
		select {
		case <-ctx.Done():
			log.Debugf("Minimax TTS stream synthesis canceled, text: %s", text)
			// Send task-finish message
			finishMsg := minimaxMessage{Event: "task_finish"}
			p.sendMessage(conn, finishMsg)

			// Per docs, server closes the WebSocket after receiving task_finish
			// Try reading task_finished if the server sends it
			conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			if finishResp, err := p.readMessage(conn); err == nil {
				log.Debugf("received task_finish ack: event=%s, message=%+v", finishResp.Event, finishResp)
			} else {
				// Connection may already be closed; that is normal
				if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					log.Debugf("server closed the connection (expected)")
					if closeErr, ok := err.(*websocket.CloseError); ok {
						log.Debugf("close frame detail: code=%d, text=%s", closeErr.Code, closeErr.Text)
					}
				} else {
					log.Debugf("failed to read task_finish ack: %v", err)
					if closeErr, ok := err.(*websocket.CloseError); ok {
						log.Debugf("close frame detail: code=%d, text=%s", closeErr.Code, closeErr.Text)
					}
				}
			}

			// Clear connection state because the server closed it
			p.clearConnection()
			return
		default:
		}

		// Set read timeout
		conn.SetReadDeadline(time.Now().Add(30 * time.Second))

		msg, err := p.readMessage(conn)
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Errorf("failed to read WebSocket message: %v", err)
				// Try to get close-frame info
				if closeErr, ok := err.(*websocket.CloseError); ok {
					log.Errorf("WebSocket close frame detail: code=%d, text=%s", closeErr.Code, closeErr.Text)
				}
				p.clearConnection()
				return
			}
			// Normal close or read error
			log.Debugf("WebSocket connection closed or read error: %v", err)
			if closeErr, ok := err.(*websocket.CloseError); ok {
				log.Debugf("WebSocket close frame detail: code=%d, text=%s", closeErr.Code, closeErr.Text)
			}
			return
		}

		if msg.BaseResp != nil && msg.BaseResp.StatusCode != 0 {
			log.Errorf("BaseResp: status_code=%d, status_msg=%s", msg.BaseResp.StatusCode, msg.BaseResp.StatusMsg)
		}

		// Check for error message
		if msg.Event == "error" || msg.Event == "task_error" {
			log.Errorf("received error message: %+v", msg)
			if msg.BaseResp != nil && msg.BaseResp.StatusCode != 0 {
				log.Errorf("error detail: status_code=%d, status_msg=%s", msg.BaseResp.StatusCode, msg.BaseResp.StatusMsg)
			}
			p.clearConnection()
			return
		}

		// Process audio data
		if msg.Data != nil && msg.Data.Audio != "" {
			chunkCount++

			// Convert hex-encoded audio to binary
			audioBytes, err := hex.DecodeString(msg.Data.Audio)
			if err != nil {
				log.Errorf("failed to decode audio data: %v", err)
				continue
			}

			// Write to pipe for the decoder
			if _, err := pipeWriter.Write(audioBytes); err != nil {
				log.Errorf("failed to write audio data to pipe: %v", err)
				p.clearConnection()
				return
			}
		}

		// Check whether finished
		if msg.IsFinal {
			log.Debugf("received last audio chunk, total chunks=%d", chunkCount)
			// Send task-finish message
			finishMsg := minimaxMessage{Event: "task_finish"}
			p.sendMessage(conn, finishMsg)

			// Clear connection state because the server closed it
			// Next use must create a new connection
			p.clearConnection()
			return
		}
	}
}

// getConnection returns a connection, creating one if needed
func (p *MinimaxTTSProvider) getConnection(ctx context.Context) (*websocket.Conn, error) {
	// Try reading the existing connection first
	p.connMutex.RLock()
	conn := p.conn
	p.connMutex.RUnlock()

	if conn != nil {
		return conn, nil
	}

	// Need to create a new connection
	p.connMutex.Lock()
	defer p.connMutex.Unlock()

	// Double-check; another goroutine may have created it
	if p.conn != nil {
		return p.conn, nil
	}

	// Create HTTP headers
	header := http.Header{}
	header.Set("Authorization", fmt.Sprintf("Bearer %s", p.APIKey))

	// Create new connection
	conn, resp, err := wsDialer.DialContext(ctx, wsURL, header)
	if err != nil {
		if resp != nil {
			log.Errorf("WebSocket connect failed, status code: %d", resp.StatusCode)
		}
		return nil, fmt.Errorf("WebSocket连接失败: %v", err)
	}

	// Set message read limit
	conn.SetReadLimit(1024 * 1024) // 1MB max message size

	// Enable keep-alive
	conn.SetPingHandler(func(appData string) error {
		return conn.WriteControl(websocket.PongMessage, []byte(appData), time.Now().Add(1*time.Second))
	})

	// Wait for connection-success message
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	_, message, err := conn.ReadMessage()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("读取连接确认消息失败: %v", err)
	}

	log.Debugf("received connection ack (raw): %s", string(message))

	var connectMsg minimaxResp
	if err := json.Unmarshal(message, &connectMsg); err != nil {
		conn.Close()
		log.Errorf("failed to parse connection ack, raw: %s, error: %v", string(message), err)
		return nil, fmt.Errorf("解析连接确认消息失败: %v", err)
	}

	log.Debugf("received connection ack (parsed): %+v", connectMsg)

	if connectMsg.Event != "connected_success" {
		conn.Close()
		log.Errorf("connect failed, expected 'connected_success', got: %+v", connectMsg)
		return nil, fmt.Errorf("连接失败，收到: %+v", connectMsg)
	}

	p.conn = conn
	log.Infof("Minimax WebSocket connection established")
	return conn, nil
}

// clearConnection clears the connection (for reconnect)
func (p *MinimaxTTSProvider) clearConnection() {
	p.connMutex.Lock()
	defer p.connMutex.Unlock()

	if p.conn != nil {
		p.conn.Close()
		p.conn = nil
		log.Infof("Minimax WebSocket connection cleared, will reconnect next time")
	}
}

// sendMessage sends a JSON message
func (p *MinimaxTTSProvider) sendMessage(conn *websocket.Conn, msg minimaxMessage) error {
	p.connMutex.RLock()
	defer p.connMutex.RUnlock()

	if conn == nil {
		return fmt.Errorf("连接已关闭")
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("序列化消息失败: %v", err)
	}

	log.Debugf("minimax sending message: %s", string(data))

	return conn.WriteMessage(websocket.TextMessage, data)
}

// readMessage reads a JSON message
func (p *MinimaxTTSProvider) readMessage(conn *websocket.Conn) (*minimaxResp, error) {
	messageType, message, err := conn.ReadMessage()
	if err != nil {
		return nil, err
	}
	_ = messageType
	//log.Debugf("minimax read WebSocket message: type=%d, raw_len=%d, content=%s", messageType, len(message), string(message))

	var msg minimaxResp
	if err := json.Unmarshal(message, &msg); err != nil {
		log.Errorf("failed to parse message, raw: %s, error: %v", string(message), err)
		return nil, fmt.Errorf("解析消息失败: %v", err)
	}

	return &msg, nil
}

// SetVoice sets voice parameters
func (p *MinimaxTTSProvider) SetVoice(voiceConfig map[string]interface{}) error {
	return nil
}

// Close releases resources and the connection
func (p *MinimaxTTSProvider) Close() error {
	p.clearConnection()
	return nil
}

// IsValid checks whether the resource is valid
func (p *MinimaxTTSProvider) IsValid() bool {
	p.connMutex.RLock()
	conn := p.conn
	p.connMutex.RUnlock()

	return conn != nil
}
