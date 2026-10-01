package funasr

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"xiaozhi-esp32-server-golang/constants"
	log "xiaozhi-esp32-server-golang/logger"

	"github.com/gorilla/websocket"

	"xiaozhi-esp32-server-golang/internal/data/audio"
	"xiaozhi-esp32-server-golang/internal/domain/asr/types"
)

// FunasrConfig config struct
type FunasrConfig struct {
	Host          string // FunASR service host
	Port          string // FunASR service port
	Mode          string // recognition mode, e.g. "online"
	SampleRate    int    // sample rate
	ChunkSize     []int  // chunk size
	ChunkInterval int    // chunk interval
	Timeout       int    // connection timeout (seconds)
	AutoEnd       bool   // auto-end after xx ms timeout, without relying on isSpeaking being false
}

// DefaultConfig default config
var DefaultConfig = FunasrConfig{
	Host:          "localhost",
	Port:          "10095",
	Mode:          "online",
	SampleRate:    audio.SampleRate,
	ChunkInterval: 10,
	ChunkSize:     []int{5, 10, 5},
	Timeout:       30,
}

// Funasr implements the ASR interface
type Funasr struct {
	config FunasrConfig

	// connection management
	conn      *websocket.Conn
	connMutex sync.RWMutex
	// send lock so only one request uses the connection at a time
	sendMutex sync.Mutex
}

var funasrStreamSeq atomic.Uint64
var funasrStreamPrefix = uuid.NewString()

type streamDebugState struct {
	audioChunkCount  atomic.Uint64
	audioSampleCount atomic.Uint64
}

// FunasrRequest FunASR WebSocket request struct
type FunasrRequest struct {
	Mode          string `json:"mode,omitempty"`           // recognition mode, e.g. "online"
	ChunkSize     []int  `json:"chunk_size,omitempty"`     // chunk size
	ChunkInterval int    `json:"chunk_interval,omitempty"` // chunk interval
	AudioFs       int    `json:"audio_fs,omitempty"`       // sample rate
	WavName       string `json:"wav_name,omitempty"`       // audio name
	WavFormat     string `json:"wav_format,omitempty"`     // audio format
	IsSpeaking    bool   `json:"is_speaking"`              // whether speaking
	Hotwords      string `json:"hotwords,omitempty"`       // hotwords
	Itn           bool   `json:"itn,omitempty"`            // whether to apply ITN
}

// FunasrResponse FunASR WebSocket response struct
type FunasrResponse struct {
	Text       string  `json:"text"`       // recognized text
	IsFinal    bool    `json:"is_final"`   // whether this is the final result
	WavName    string  `json:"wav_name"`   // audio name
	TimeStamp  string  `json:"timestamp"`  // timestamp
	Mode       string  `json:"mode"`       // mode
	Confidence float64 `json:"confidence"` // confidence
}

// NewFunasr creates a new Funasr instance
func NewFunasr(config FunasrConfig) (*Funasr, error) {
	if config.Host == "" {
		config = DefaultConfig
	}

	return &Funasr{
		config: config,
	}, nil
}

// getConnection gets a connection, creating one if needed
func (f *Funasr) getConnection(ctx context.Context) (*websocket.Conn, error) {
	// try reading an existing connection first
	f.connMutex.RLock()
	conn := f.conn
	f.connMutex.RUnlock()

	if conn != nil {
		log.Debugf("FunASR WebSocket reusing connection: conn=%p", conn)
		return conn, nil
	}

	// need to create a new connection
	f.connMutex.Lock()
	defer f.connMutex.Unlock()

	// double-check; another goroutine may have created the connection
	if f.conn != nil {
		return f.conn, nil
	}

	// create a new connection
	url := fmt.Sprintf("ws://%s:%s/", f.config.Host, f.config.Port)
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, url, nil)
	if err != nil {
		return nil, fmt.Errorf("连接到FunASR服务失败: %v", err)
	}

	f.conn = conn
	log.Infof("FunASR WebSocket connection established: conn=%p", conn)
	return conn, nil
}

// clearConnection clears the connection (for reconnect)
func (f *Funasr) clearConnection() {
	f.connMutex.Lock()
	defer f.connMutex.Unlock()

	if f.conn != nil {
		log.Infof("FunASR WebSocket connection cleared: conn=%p", f.conn)
		f.conn.Close()
		f.conn = nil
	}
}

// StreamingResult streaming recognition result
type StreamingResult struct {
	Text    string // recognized text
	IsFinal bool   // whether this is the final result
}

// isTimeoutError checks whether the error is a timeout
func isTimeoutError(err error) bool {
	if err == nil {
		return false
	}

	// check for network timeout errors
	if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
		return true
	}

	// check whether the error message contains timeout keywords
	errMsg := strings.ToLower(err.Error())
	return strings.Contains(errMsg, "timeout") || strings.Contains(errMsg, "i/o timeout")
}

// isConnectionClosedError checks whether the error is a connection-closed error
func isConnectionClosedError(err error) bool {
	if err == nil {
		return false
	}

	// check for WebSocket close errors
	if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway,
		websocket.CloseAbnormalClosure, websocket.CloseNoStatusReceived) {
		return true
	}

	// check whether the error message contains connection-closed keywords
	errMsg := strings.ToLower(err.Error())
	return strings.Contains(errMsg, "connection closed") ||
		strings.Contains(errMsg, "broken pipe") ||
		strings.Contains(errMsg, "connection reset") ||
		strings.Contains(errMsg, "use of closed network connection")
}

// writeMessage safely writes a message to the WebSocket connection
func (f *Funasr) writeMessage(conn *websocket.Conn, messageType int, data []byte) error {
	// use a read lock around writes to avoid concurrent write corruption
	f.connMutex.RLock()
	defer f.connMutex.RUnlock()

	// check whether the connection is valid
	if conn == nil {
		return fmt.Errorf("连接已关闭")
	}

	return conn.WriteMessage(messageType, data)
}

// StreamingRecognize implements streaming recognition
// receives audio from audioStream and returns results via resultChan
// cancellation and timeout are controlled via ctx
func (f *Funasr) StreamingRecognize(ctx context.Context, audioStream <-chan []float32) (chan types.StreamingResult, error) {
	// hold the send lock so only one request uses the connection at a time
	f.sendMutex.Lock()
	// note: unlock when the goroutine finishes, not when the function returns

	// get a connection (reuse or create)
	conn, err := f.getConnection(ctx)
	if err != nil {
		f.sendMutex.Unlock() // unlock immediately if getting the connection fails
		return nil, err
	}

	subCtx, cancelFunc := context.WithCancel(ctx)
	streamID := fmt.Sprintf("funasr-stream-%s-%d", funasrStreamPrefix, funasrStreamSeq.Add(1))
	wavName := streamID
	debugState := &streamDebugState{}

	// send the initial message
	firstMessage := FunasrRequest{
		Mode:          f.config.Mode,
		ChunkSize:     []int{5, 10, 5},
		ChunkInterval: f.config.ChunkInterval,
		AudioFs:       f.config.SampleRate,
		WavName:       wavName,
		WavFormat:     "pcm",
		IsSpeaking:    true,
		Hotwords:      "{\"阿里巴巴\":20,\"hello world\":40}",
		Itn:           true,
	}

	log.Debugf(
		"funasr StreamingRecognize started: stream_id=%s, conn=%p, mode=%s, chunk_interval=%d, chunk_size=%v, wav_name=%s",
		streamID,
		conn,
		f.config.Mode,
		f.config.ChunkInterval,
		firstMessage.ChunkSize,
		firstMessage.WavName,
	)

	messageBytes, err := json.Marshal(firstMessage)
	if err != nil {
		cancelFunc()
		f.sendMutex.Unlock() // unlock immediately if serialization fails
		return nil, fmt.Errorf("序列化初始消息失败: %v", err)
	}

	err = f.writeMessage(conn, websocket.TextMessage, messageBytes)
	if err != nil {
		// send failed; clear the connection so the next use reconnects
		log.Errorf("failed to send initial message: %v, clearing connection", err)
		f.clearConnection()
		cancelFunc()
		f.sendMutex.Unlock() // unlock immediately if send fails
		return nil, fmt.Errorf("发送初始消息失败: %v", err)
	}

	// create a buffered result channel to avoid blocking
	resultChan := make(chan types.StreamingResult, 20)

	// use WaitGroup to wait for both goroutines
	var wg sync.WaitGroup
	wg.Add(2)

	// start goroutines to receive and send data
	// release the lock when the goroutine finishes
	go func() {
		defer wg.Done()
		f.recvResult(subCtx, conn, streamID, wavName, debugState, resultChan)
	}()

	go func() {
		defer wg.Done()
		f.forwardStreamAudio(subCtx, cancelFunc, conn, streamID, wavName, debugState, audioStream)
	}()

	// wait in the background for goroutines to finish, then unlock
	go func() {
		wg.Wait()
		f.clearConnection()
		f.sendMutex.Unlock()
		log.Debugf(
			"funasr StreamingRecognize goroutine done, sendMutex released: stream_id=%s, wav_name=%s, chunks=%d, samples=%d",
			streamID,
			wavName,
			debugState.audioChunkCount.Load(),
			debugState.audioSampleCount.Load(),
		)
	}()

	return resultChan, nil
}

func (f *Funasr) recvResult(ctx context.Context, conn *websocket.Conn, streamID string, wavName string, debugState *streamDebugState, resultChan chan types.StreamingResult) {
	defer func() {
		close(resultChan)
	}()

	for {
		select {
		case <-ctx.Done():
			// context canceled; exit the goroutine
			log.Debugf("funasr recvResult canceled: %v", ctx.Err())
			return
		default:
			// continue normal processing
		}

		_, message, err := conn.ReadMessage()
		if err != nil {
			log.Debugf("funasr recvResult failed to read result: stream_id=%s, conn=%p, err=%v, clearing connection", streamID, conn, err)
			// read failed; clear the connection so the next use reconnects
			f.clearConnection()
			return
		}
		log.Debugf(
			"funasr recvResult read result: stream_id=%s, conn=%p, chunks=%d, samples=%d, payload=%v",
			streamID,
			conn,
			debugState.audioChunkCount.Load(),
			debugState.audioSampleCount.Load(),
			string(message),
		)

		var response FunasrResponse
		err = json.Unmarshal(message, &response)
		if err != nil {
			log.Debugf("funasr recvResult failed to parse result: %v", err)
			continue
		}

		if response.WavName != "" && response.WavName != wavName {
			log.Warnf(
				"funasr recvResult ignoring non-current stream result: stream_id=%s, expected_wav=%s, actual_wav=%s, conn=%p, chunks=%d, samples=%d",
				streamID,
				wavName,
				response.WavName,
				conn,
				debugState.audioChunkCount.Load(),
				debugState.audioSampleCount.Load(),
			)
			continue
		}

		// send a result only when there is text
		/*if response.Text == "" {
			continue
		}*/

		streamingResult := f.toStreamingResult(response)

		// send the recognition result
		select {
		case <-ctx.Done():
			// context canceled; exit the goroutine
			log.Debugf("funasr recvResult canceled: %v", ctx.Err())
			return
		case resultChan <- streamingResult:
		}
		/*if f.config.AutoEnd {
			log.Debugf("funasr recvResult autoend")
			return
		}*/
		// result sent successfully
		// if this is the final result and input has ended, exit the loop
		if streamingResult.IsFinal {
			log.Debugf(
				"funasr recvResult isfinal: stream_id=%s, conn=%p, response_mode=%s, raw_is_final=%v, text_len=%d, wav_name=%s, chunks=%d, samples=%d",
				streamID,
				conn,
				response.Mode,
				response.IsFinal,
				len([]rune(response.Text)),
				response.WavName,
				debugState.audioChunkCount.Load(),
				debugState.audioSampleCount.Load(),
			)
			return
		}
	}
}

func (f *Funasr) toStreamingResult(response FunasrResponse) types.StreamingResult {
	result := types.StreamingResult{
		Text:    response.Text,
		IsFinal: response.IsFinal,
		AsrType: constants.AsrTypeFunAsr,
		Mode:    response.Mode,
	}

	if strings.EqualFold(strings.TrimSpace(f.config.Mode), "2pass") {
		switch strings.ToLower(strings.TrimSpace(response.Mode)) {
		case "2pass-online":
			result.IsFinal = false
		case "2pass-offline":
			result.IsFinal = true
		}
	}

	if result.IsFinal && strings.TrimSpace(result.Text) == "" {
		result.EmptyReason = types.EmptyReasonProviderEmptyFinal
	}

	return result
}

func (f *Funasr) forwardStreamAudio(ctx context.Context, cancelFunc context.CancelFunc, conn *websocket.Conn, streamID string, wavName string, debugState *streamDebugState, audioStream <-chan []float32) {
	sendEndMsg := func() {
		// send the termination message
		endMessage := FunasrRequest{
			Mode:          f.config.Mode,
			ChunkInterval: f.config.ChunkInterval,
			ChunkSize:     []int{5, 10, 5},
			WavName:       wavName,
			IsSpeaking:    false,
		}
		endMessageBytes, _ := json.Marshal(endMessage)
		log.Debugf(
			"funasr forwardStreamAudio sending end message: stream_id=%s, conn=%p, chunks=%d, samples=%d, payload=%v",
			streamID,
			conn,
			debugState.audioChunkCount.Load(),
			debugState.audioSampleCount.Load(),
			string(endMessageBytes),
		)
		err := f.writeMessage(conn, websocket.TextMessage, endMessageBytes)
		if err != nil {
			log.Debugf("funasr forwardStreamAudio failed to send end message: stream_id=%s, conn=%p, err=%v, clearing connection", streamID, conn, err)
			f.clearConnection()
		}
	}
	// handle the input audio stream
	for {
		select {
		case <-ctx.Done():
			// context canceled; send end message and exit
			log.Debugf(
				"funasr forwardStreamAudio context canceled: stream_id=%s, conn=%p, chunks=%d, samples=%d, err=%v",
				streamID,
				conn,
				debugState.audioChunkCount.Load(),
				debugState.audioSampleCount.Load(),
				ctx.Err(),
			)
			// note: no need to call cancelFunc() here; ctx.Done() already means the context is canceled
			sendEndMsg()
			return
		case pcmChunk, ok := <-audioStream:
			if !ok {
				// channel closed; end input and notify the receive goroutine to stop
				log.Debugf(
					"funasr forwardStreamAudio audio channel closed: stream_id=%s, conn=%p, chunks=%d, samples=%d",
					streamID,
					conn,
					debugState.audioChunkCount.Load(),
					debugState.audioSampleCount.Load(),
				)
				sendEndMsg()
				return
			}

			// convert PCM data to bytes
			audioBytes := Float32SliceToBytes(pcmChunk)

			//log.Debugf("funasr forwardStreamAudio send audio data, pcmChunk len: %v, audioBytes len: %v", len(pcmChunk), len(audioBytes))

			// send audio data
			err := f.writeMessage(conn, websocket.BinaryMessage, audioBytes)
			if err != nil {
				log.Debugf("funasr forwardStreamAudio failed to send audio: stream_id=%s, conn=%p, err=%v, clearing connection", streamID, conn, err)
				f.clearConnection()
				cancelFunc() // cancel context on send failure so the recvResult goroutine stops
				return
			}
			chunkCount := debugState.audioChunkCount.Add(1)
			sampleCount := debugState.audioSampleCount.Add(uint64(len(pcmChunk)))
			if chunkCount <= 3 || chunkCount%10 == 0 {
				log.Debugf(
					"funasr forwardStreamAudio sent audio chunk: stream_id=%s, conn=%p, chunk=%d, chunk_samples=%d, total_samples=%d, bytes=%d",
					streamID,
					conn,
					chunkCount,
					len(pcmChunk),
					sampleCount,
					len(audioBytes),
				)
			}
		}
	}
}

// Process processes audio and returns the recognition result
func (f *Funasr) Process(pcmData []float32) (string, error) {
	ctx := context.Background()

	// hold the send lock so only one request uses the connection at a time
	f.sendMutex.Lock()
	defer f.sendMutex.Unlock()

	// get a connection (reuse or create)
	conn, err := f.getConnection(ctx)
	if err != nil {
		return "", err
	}

	audioBytes := Float32SliceToBytes(pcmData)

	// send the initial message
	firstMessage := FunasrRequest{
		Mode:          f.config.Mode,
		ChunkSize:     []int{5, 10, 5},
		ChunkInterval: f.config.ChunkInterval,
		AudioFs:       f.config.SampleRate,
		WavName:       "stream",
		WavFormat:     "pcm",
		IsSpeaking:    true,
		Hotwords:      "",
		Itn:           true,
	}

	messageBytes, err := json.Marshal(firstMessage)
	if err != nil {
		return "", fmt.Errorf("序列化初始消息失败: %v", err)
	}

	err = f.writeMessage(conn, websocket.TextMessage, messageBytes)
	if err != nil {
		// send failed; clear the connection so the next use reconnects
		log.Errorf("failed to send initial message: %v, clearing connection", err)
		f.clearConnection()
		return "", fmt.Errorf("发送初始消息失败: %v", err)
	}

	// send audio data in chunks
	chunkSize := int(audio.SampleRate * 0.1) // each chunk is about 100ms of audio (16000 * 0.1)
	for i := 0; i < len(audioBytes); i += chunkSize {
		end := i + chunkSize
		if end > len(audioBytes) {
			end = len(audioBytes)
		}
		chunk := audioBytes[i:end]

		err = f.writeMessage(conn, websocket.BinaryMessage, chunk)
		if err != nil {
			// send failed; clear the connection so the next use reconnects
			log.Errorf("failed to send audio data: %v, clearing connection", err)
			f.clearConnection()
			return "", fmt.Errorf("发送音频数据失败: %v", err)
		}
	}

	// send the termination message
	endMessage := FunasrRequest{
		IsSpeaking: false,
	}
	endMessageBytes, _ := json.Marshal(endMessage)
	err = f.writeMessage(conn, websocket.TextMessage, endMessageBytes)
	if err != nil {
		// send failed; clear the connection so the next use reconnects
		log.Errorf("failed to send terminate message: %v, clearing connection", err)
		f.clearConnection()
		return "", fmt.Errorf("发送终止消息失败: %v", err)
	}

	// set read timeout
	conn.SetReadDeadline(time.Now().Add(time.Duration(f.config.Timeout) * time.Second))

	// read results
	var result string
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if isTimeoutError(err) {
				log.Debugf("funasr Process read result timeout: %v", err)
				f.clearConnection() // read timeout; clear the connection
				return "", fmt.Errorf("读取结果超时: %v", err)
			}
			if isConnectionClosedError(err) {
				log.Debugf("funasr Process read result connection closed: %v", err)
				f.clearConnection() // connection closed; clear the connection
				return "", fmt.Errorf("连接已关闭: %v", err)
			}
			// read failed; clear the connection so the next use reconnects
			log.Errorf("funasr Process failed to read result: %v, clearing connection", err)
			f.clearConnection()
			return "", fmt.Errorf("读取结果失败: %v", err)
		}

		var response FunasrResponse
		err = json.Unmarshal(message, &response)
		if err != nil {
			continue
		}

		// check whether this is the final result
		if response.IsFinal {
			result = response.Text
			break
		}
	}

	return result, nil
}

func Float32ToInt16(sample float32) int16 {
	// clamp to [-1, 1] to avoid overflow
	if sample > 1.0 {
		sample = 1.0
	} else if sample < -1.0 {
		sample = -1.0
	}
	return int16(sample * 32767)
}

func Float32SliceToBytes(samples []float32) []byte {
	data := make([]byte, len(samples)*2)
	for i, s := range samples {
		i16 := Float32ToInt16(s)
		data[2*i] = byte(i16)
		data[2*i+1] = byte(i16 >> 8)
	}
	return data
}

// Close closes resources and releases the connection
func (f *Funasr) Close() error {
	f.clearConnection()
	return nil
}

// IsValid checks whether the resource is valid
func (f *Funasr) IsValid() bool {
	f.connMutex.RLock()
	conn := f.conn
	f.connMutex.RUnlock()
	return conn != nil
}

/*
Example of error-type checks:

1. Timeout error:
   if isTimeoutError(err) {
       // handle timeout; may retry or adjust the timeout
       log.Warnf("operation timeout: %v", err)
   }

2. Connection-closed error:
   if isConnectionClosedError(err) {
       // handle closed connection; may need to reconnect
       log.Warnf("connection closed: %v", err)
   }

3. Combined error handling:
   _, message, err := conn.ReadMessage()
   if err != nil {
       if isTimeoutError(err) {
           // timeout: possible network delay or slow server
           // suggestion: adjust timeout or retry
       } else if isConnectionClosedError(err) {
           // connection closed: server disconnect or network drop
           // suggestion: re-establish the connection
       } else {
           // other: possible protocol or data-format error
           // suggestion: check data format or protocol implementation
       }
   }

Common error types:
- timeout: i/o timeout, context deadline exceeded
- connection closed: connection closed, broken pipe, connection reset
- WebSocket close: close 1000 (normal), close 1001 (going away)
*/
