package xiaozhi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	log "xiaozhi-esp32-server-golang/logger"

	"github.com/gorilla/websocket"
)

var deviceIdList = []string{
	"ba:8f:17:de:94:94",
	"f2:85:44:27:7b:51",
	"4f:57:fb:d4:69:fa",
	"b3:1e:1c:80:cc:78",
	"32:a5:cc:b7:c0:e4",
	"2b:60:6a:5a:72:10",
	"ca:a6:8b:20:f1:6f",
	"26:1a:d7:27:9f:f8",
	"03:02:26:58:2b:06",
	"5f:f3:85:8b:5d:da",
}

// tracks recently failed deviceIds and their block expiry
var (
	deviceIdBlocklist     = make(map[string]time.Time)
	deviceIdBlocklistLock sync.Mutex
	// how long a device ID stays blocked after an error
	deviceIdBlockDuration = 5 * time.Second
)

// XiaozhiProvider Xiaozhi TTS WebSocket Provider
// supports streaming text-to-speech
type XiaozhiProvider struct {
	ServerAddr  string
	DeviceID    string
	AudioFormat map[string]interface{}
	Header      http.Header
}

// periodically purge expired deviceId blocks
func init() {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			// purge expired deviceId blocks
			deviceIdBlocklistLock.Lock()
			now := time.Now()
			for id, expireTime := range deviceIdBlocklist {
				if now.After(expireTime) {
					delete(deviceIdBlocklist, id)
					log.Debugf("device ID disable expired, re-enabled: %s", id)
				}
			}
			deviceIdBlocklistLock.Unlock()
		}
	}()
}

// add deviceId to the block list
func blockDeviceId(deviceId string) {
	deviceIdBlocklistLock.Lock()
	defer deviceIdBlocklistLock.Unlock()

	deviceIdBlocklist[deviceId] = time.Now().Add(deviceIdBlockDuration)
	log.Warnf("device ID %s added to disable list, will re-enable after %v", deviceId, deviceIdBlockDuration)
}

// check whether deviceId is blocked
func isDeviceIdBlocked(deviceId string) bool {
	deviceIdBlocklistLock.Lock()
	defer deviceIdBlocklistLock.Unlock()

	expireTime, exists := deviceIdBlocklist[deviceId]
	if !exists {
		return false
	}

	// if the block has expired, remove it from the list
	if time.Now().After(expireTime) {
		delete(deviceIdBlocklist, deviceId)
		log.Debugf("device ID disable expired, re-enabled: %s", deviceId)
		return false
	}

	return true
}

// NewXiaozhiProvider creates a new Xiaozhi TTS Provider
func NewXiaozhiProvider(config map[string]interface{}) *XiaozhiProvider {
	serverAddr, _ := config["server_addr"].(string)
	deviceID, _ := config["device_id"].(string)
	clientID, _ := config["client_id"].(string)
	token, _ := config["token"].(string)
	format := map[string]interface{}{
		"sample_rate":    16000,
		"channels":       1,
		"frame_duration": 20,
		"format":         "opus",
	}

	header := http.Header{}
	header.Set("Device-Id", deviceID)
	header.Set("Content-Type", "application/json")
	header.Set("Authorization", "Bearer "+token)
	header.Set("Protocol-Version", "1")
	header.Set("Client-Id", clientID)

	return &XiaozhiProvider{
		ServerAddr:  serverAddr,
		DeviceID:    deviceID,
		AudioFormat: format,
		Header:      header,
	}
}

// selectDeviceId picks an available device ID
func (p *XiaozhiProvider) selectDeviceId() string {
	// find non-blocked deviceIds in deviceIdList
	for _, deviceId := range deviceIdList {
		if !isDeviceIdBlocked(deviceId) {
			log.Debugf("selected non-disabled device ID: %s", deviceId)
			return deviceId
		}
	}

	// if every deviceId is blocked, round-robin across all deviceIds
	if len(deviceIdList) > 0 {
		// simple time-based round-robin
		selectedIndex := int(time.Now().Unix()) % len(deviceIdList)
		selectedDeviceId := deviceIdList[selectedIndex]
		log.Warnf("all deviceIds disabled, round-robin selected device ID: %s (index: %d)", selectedDeviceId, selectedIndex)
		return selectedDeviceId
	}

	// if deviceIdList is empty, use the passed-in deviceId
	if p.DeviceID != "" {
		log.Warnf("deviceIdList empty, using current device ID: %s", p.DeviceID)
		return p.DeviceID
	}

	// if neither, return the first device ID when present
	if len(deviceIdList) > 0 {
		return deviceIdList[0]
	}

	return ""
}

// createWSConnection creates a new WebSocket connection
func (p *XiaozhiProvider) createWSConnection(ctx context.Context) (*websocket.Conn, string, error) {
	// pick an available device ID
	selectedDeviceId := p.selectDeviceId()
	if selectedDeviceId == "" {
		return nil, "", fmt.Errorf("无法选择设备ID")
	}

	// update current p.DeviceID and Header
	p.DeviceID = selectedDeviceId
	p.Header.Set("Device-Id", selectedDeviceId)

	// create new connection
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, p.ServerAddr, p.Header)
	if err != nil {
		log.Errorf("failed to create WebSocket connection: %v, device ID: %s", err, selectedDeviceId)
		blockDeviceId(selectedDeviceId) // add the failed deviceId to the block list
		return nil, "", err
	}

	// enable keep-alive
	conn.SetPingHandler(func(appData string) error {
		return conn.WriteControl(websocket.PongMessage, []byte(appData), time.Now().Add(5*time.Second))
	})

	// send hello when a new connection is created
	helloMsg := map[string]interface{}{
		"type":         "hello",
		"device_id":    selectedDeviceId,
		"transport":    "websocket",
		"version":      1,
		"audio_params": p.AudioFormat,
	}
	log.Debugf("created new connection and sent hello, device ID: %s", selectedDeviceId)
	if err := conn.WriteJSON(helloMsg); err != nil {
		conn.Close()
		return nil, "", fmt.Errorf("发送hello消息失败: %v", err)
	}

	return conn, selectedDeviceId, nil
}

type RecvMsg struct {
	Type    string `json:"type"`
	State   string `json:"state"`
	Text    string `json:"text"`
	Version int    `json:"version"`
}

// sendStopMessage sends stop and closes the connection
func sendStopMessage(conn *websocket.Conn, deviceId string) {
	stopMsg := map[string]interface{}{
		"type":      "listen",
		"device_id": deviceId,
		"state":     "stop",
	}
	if err := conn.WriteJSON(stopMsg); err != nil {
		log.Warnf("failed to send stop message: %v, device ID: %s", err, deviceId)
	} else {
		log.Debugf("sent stop message ok, device ID: %s", deviceId)
	}
}

// handleTTSConnection wraps connect, send, and receive logic
func (p *XiaozhiProvider) handleTTSConnection(ctx context.Context, text string, outputChan chan []byte) error {
	// create new connection
	conn, deviceId, err := p.createWSConnection(ctx)
	if err != nil {
		return fmt.Errorf("创建小智TTS连接失败: %v", err)
	}
	defer func() {
		// send stop and close the connection
		sendStopMessage(conn, deviceId)
		conn.Close()
	}()

	// send listen detect message
	sendText := fmt.Sprintf("`%s`", text)
	listenMsg := map[string]interface{}{
		"type":      "listen",
		"device_id": deviceId,
		"state":     "detect",
		"text":      sendText,
	}
	log.Debugf("sending xiaozhi server message: %v", listenMsg)

	if err := conn.WriteJSON(listenMsg); err != nil {
		log.Errorf("failed to send listen message: %v, device ID: %s", err, deviceId)
		blockDeviceId(deviceId) // add the failed deviceId to the block list
		return fmt.Errorf("发送消息失败: %v", err)
	}

	// read and process messages
	startTs := time.Now().UnixMilli()
	var firstFrameTs bool
	i := 0
	receivedFrames := false

	for {
		select {
		case <-ctx.Done():
			log.Debugf("xiaozhi server message ctx.Done(), device ID: %s", deviceId)
			return nil
		default:
		}
		msgType, msg, err := conn.ReadMessage()
		if err != nil {
			// connection error
			log.Errorf("read message error: %v, device ID: %s", err, deviceId)

			// if no audio frames received yet, the connection may be bad; add deviceId to the block list
			if !receivedFrames {
				blockDeviceId(deviceId)
			}

			return fmt.Errorf("读取消息错误: %v", err)
		}
		if msgType == websocket.TextMessage {
			log.Debugf("received xiaozhi server message: %s", string(msg))
			var recvMsg RecvMsg
			err := json.Unmarshal(msg, &recvMsg)
			if err != nil {
				continue
			}
			if recvMsg.Type == "tts" {
				if recvMsg.State == "stop" {
					log.Debugf("xiaozhi server tts stop message")
					return nil
				}
			}
		} else if msgType == websocket.BinaryMessage {
			receivedFrames = true
			if !firstFrameTs {
				firstFrameTs = true
				log.Debugf("tts timing: xiaozhi first audio frame at: %d", time.Now().UnixMilli()-startTs)
			}
			outputChan <- msg
			if i%20 == 0 {
				log.Debugf("xiaozhi server audio message, received %d audio frames", i)
			}
			i++
		}
	}
}

// TextToSpeechStream streaming TTS; returns a chan of Opus audio frames
func (p *XiaozhiProvider) TextToSpeechStream(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) (chan []byte, error) {
	outputChan := make(chan []byte, 1000)

	// try the TTS connection with retries
	go func() {
		defer close(outputChan)

		retryCount := 0
		maxRetries := 2
		var lastError error

		// retry up to maxRetries times
		for retryCount <= maxRetries {
			if retryCount > 0 {
				log.Infof("retrying get connection, attempt %d/%d", retryCount, maxRetries)

				// check whether the context was canceled before retrying
				select {
				case <-ctx.Done():
					log.Debugf("context canceled, stopping retries")
					return
				default:
					// continue retrying
				}
			}

			// handle TTS connection
			err := p.handleTTSConnection(ctx, text, outputChan)

			if err == nil {
				// connection handling succeeded; no retry needed
				return
			}

			lastError = err
			log.Errorf("TTS connection handling failed: %v (retry: %d/%d)", err, retryCount, maxRetries)

			retryCount++
		}

		if retryCount > maxRetries {
			log.Warnf("hit max retries %d, giving up, last error: %v", maxRetries, lastError)
		}
	}()

	return outputChan, nil
}

// GetVoiceInfo returns TTS config info
func (p *XiaozhiProvider) GetVoiceInfo() map[string]interface{} {
	return map[string]interface{}{
		"type":         "xiaozhi_ws",
		"server_addr":  p.ServerAddr,
		"device_id":    p.DeviceID,
		"audio_format": p.AudioFormat,
	}
}

// SetVoice sets voice params (Xiaozhi Provider does not support dynamic voice)
func (p *XiaozhiProvider) SetVoice(voiceConfig map[string]interface{}) error {
	return fmt.Errorf("Xiaozhi TTS Provider 不支持动态设置音色")
}

// Close closes resources (stateless Provider; no-op)
func (p *XiaozhiProvider) Close() error {
	return nil
}

// IsValid checks whether the resource is valid
func (p *XiaozhiProvider) IsValid() bool {
	return p != nil
}

// TextToSpeech implements BaseTTSProvider by aggregating stream frames
func (p *XiaozhiProvider) TextToSpeech(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) ([][]byte, error) {
	ch, err := p.TextToSpeechStream(ctx, text, sampleRate, channels, frameDuration)
	if err != nil {
		return nil, err
	}
	var frames [][]byte
	for frame := range ch {
		frames = append(frames, frame)
	}
	return frames, nil
}
