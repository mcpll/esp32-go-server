package edge_offline

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"xiaozhi-esp32-server-golang/internal/util"
	log "xiaozhi-esp32-server-golang/logger"

	"github.com/gopxl/beep"
	"github.com/gorilla/websocket"
)

// EdgeOfflineTTSProvider WebSocket TTS provider
type EdgeOfflineTTSProvider struct {
	ServerURL        string
	Timeout          time.Duration
	HandshakeTimeout time.Duration

	// connection management
	conn      *websocket.Conn
	connMutex sync.RWMutex
	// send lock so only one request uses the connection at a time
	sendMutex sync.Mutex
}

// NewEdgeOfflineTTSProvider creates a new Edge Offline TTS provider
func NewEdgeOfflineTTSProvider(config map[string]interface{}) *EdgeOfflineTTSProvider {
	serverURL, _ := config["server_url"].(string)
	timeout, _ := config["timeout"].(float64)
	handshakeTimeout, _ := config["handshake_timeout"].(float64)

	// set defaults
	if serverURL == "" {
		serverURL = "ws://localhost:8080/tts"
	}
	if timeout == 0 {
		timeout = 30 // default 30s timeout
	}
	if handshakeTimeout == 0 {
		handshakeTimeout = 10 // default 10s handshake timeout
	}

	return &EdgeOfflineTTSProvider{
		ServerURL:        serverURL,
		Timeout:          time.Duration(timeout) * time.Second,
		HandshakeTimeout: time.Duration(handshakeTimeout) * time.Second,
	}
}

// getConnection returns an existing connection or creates one
func (p *EdgeOfflineTTSProvider) getConnection(ctx context.Context) (*websocket.Conn, error) {
	// try reading an existing connection first
	p.connMutex.RLock()
	conn := p.conn
	p.connMutex.RUnlock()

	if conn != nil {
		return conn, nil
	}

	// need to create a new connection
	p.connMutex.Lock()
	defer p.connMutex.Unlock()

	// double-check; another goroutine may have created the connection
	if p.conn != nil {
		return p.conn, nil
	}

	// create new connection
	dialer := &websocket.Dialer{
		HandshakeTimeout: p.HandshakeTimeout,
	}
	conn, _, err := dialer.DialContext(ctx, p.ServerURL, nil)
	if err != nil {
		return nil, fmt.Errorf("WebSocket连接失败: %v", err)
	}

	p.conn = conn
	log.Infof("WebSocket connection established")
	return conn, nil
}

// clearConnection clears the connection (for reconnect)
func (p *EdgeOfflineTTSProvider) clearConnection() {
	p.connMutex.Lock()
	defer p.connMutex.Unlock()

	if p.conn != nil {
		p.conn.Close()
		p.conn = nil
		log.Infof("WebSocket connection cleared, waiting for next reconnect")
	}
}

// writeMessage writes a message to the WebSocket connection safely
func (p *EdgeOfflineTTSProvider) writeMessage(conn *websocket.Conn, messageType int, data []byte) error {
	// protect connection writes with a read lock to avoid concurrent write corruption
	p.connMutex.RLock()
	defer p.connMutex.RUnlock()

	// check whether the connection is valid
	if conn == nil {
		return fmt.Errorf("连接已关闭")
	}

	return conn.WriteMessage(messageType, data)
}

// TextToSpeech converts text to speech and returns audio frames
func (p *EdgeOfflineTTSProvider) TextToSpeech(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) ([][]byte, error) {
	var frames [][]byte

	// hold the send lock so only one request uses the connection at a time
	p.sendMutex.Lock()
	// Note: do not unlock on function return; unlock when the goroutine finishes

	// get connection (reuse or create)
	conn, err := p.getConnection(ctx)
	if err != nil {
		p.sendMutex.Unlock() // unlock immediately if getting the connection fails
		return nil, err
	}

	// send text (via the protected write helper)
	err = p.writeMessage(conn, websocket.TextMessage, []byte(text))
	if err != nil {
		// send failed; clear connection so the next use reconnects
		log.Errorf("failed to send text: %v, clearing connection", err)
		p.clearConnection()
		p.sendMutex.Unlock() // unlock immediately on send failure
		return nil, fmt.Errorf("发送文本失败: %v", err)
	}

	// create a pipe for audio transfer
	pipeReader, pipeWriter := io.Pipe()
	outputChan := make(chan []byte, 1000)
	startTs := time.Now().UnixMilli()

	// create audio decoder
	audioDecoder, err := util.CreateAudioDecoder(ctx, pipeReader, outputChan, frameDuration, "mp3")
	if err != nil {
		pipeReader.Close()
		p.sendMutex.Unlock() // unlock immediately if decoder creation fails
		return nil, fmt.Errorf("创建音频解码器失败: %v", err)
	}

	decoderDone := make(chan struct{})
	go func() {
		defer close(decoderDone)
		if err := audioDecoder.Run(startTs); err != nil {
			log.Errorf("audio decode failed: %v", err)
		}
	}()

	// use WaitGroup to wait for the read goroutine to finish
	var wg sync.WaitGroup
	wg.Add(1)

	// receive WebSocket data and write to the pipe; the lock is released via defer in this goroutine so it always unlocks on success, error, or panic
	done := make(chan struct{})
	go func() {
		defer wg.Done()
		defer p.sendMutex.Unlock()
		defer close(done)
		defer pipeWriter.Close()

		for {
			messageType, data, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsCloseError(err, websocket.CloseNormalClosure) {
					return
				}
				log.Errorf("failed to read WebSocket message: %v, clearing connection", err)
				// connection dropped; clear it so the next use reconnects
				p.clearConnection()
				return
			}

			if messageType == websocket.BinaryMessage {
				if _, err := pipeWriter.Write(data); err != nil {
					log.Errorf("failed to write audio data: %v", err)
					return
				}
			}
		}
	}()

	// collect all Opus frames
	collectorDone := make(chan struct{})
	go func() {
		for frame := range outputChan {
			frames = append(frames, frame)
		}
		close(collectorDone)
	}()

	// wait for completion or timeout
	select {
	case <-ctx.Done():
		_ = pipeWriter.CloseWithError(ctx.Err())
		p.clearConnection()
		<-decoderDone
		<-collectorDone
		return nil, fmt.Errorf("TTS合成超时或被取消")
	case <-done:
		<-decoderDone
		<-collectorDone
		return frames, nil
	}
}

// TextToSpeechStream streaming speech synthesis
func (p *EdgeOfflineTTSProvider) TextToSpeechStream(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) (chan []byte, error) {
	outputChan := make(chan []byte, 100)

	go func() {
		// hold the send lock so only one request uses the connection at a time
		p.sendMutex.Lock()

		// get connection (reuse or create)
		conn, err := p.getConnection(ctx)
		if err != nil {
			p.sendMutex.Unlock()
			close(outputChan)
			log.Errorf("failed to get WebSocket connection: %v", err)
			return
		}

		// send text (via the protected write helper)
		err = p.writeMessage(conn, websocket.TextMessage, []byte(text))
		if err != nil {
			p.sendMutex.Unlock()
			close(outputChan)
			log.Errorf("failed to send text: %v, clearing connection", err)
			// send failed; clear connection so the next use reconnects
			p.clearConnection()
			return
		}

		// create a pipe for audio transfer
		pipeReader, pipeWriter := io.Pipe()
		startTs := time.Now().UnixMilli()
		audioDecoder, err := util.CreateAudioDecoderWithSampleRate(ctx, pipeReader, outputChan, frameDuration, "pcm", sampleRate)
		if err != nil {
			p.sendMutex.Unlock()
			_ = pipeReader.Close()
			_ = pipeWriter.Close()
			close(outputChan)
			log.Errorf("failed to create audio decoder: %v", err)
			return
		}
		audioDecoder.WithFormat(beep.Format{
			SampleRate:  beep.SampleRate(24000),
			NumChannels: channels,
			Precision:   2,
		})

		decoderDone := make(chan struct{})
		go func() {
			defer close(decoderDone)
			if err := audioDecoder.Run(startTs); err != nil {
				log.Errorf("audio decode failed: %v", err)
			}
		}()

		defer func() {
			_ = pipeWriter.Close()
			<-decoderDone
			// unlock after reading finishes
			log.Debugf("TextToSpeechStream read completed, release sendMutex")
			p.sendMutex.Unlock()
		}()

		// receive WebSocket data into the pipe (hold the lock while reading for serialization)
		for {
			select {
			case <-ctx.Done():
				log.Debugf("TextToSpeechStream context done, exit")
				// close pipeWriter so the decoder ends naturally and closes the channel
				return
			default:
				messageType, data, err := conn.ReadMessage()
				if err != nil {
					// close pipeWriter so the decoder ends naturally and closes the channel
					pipeWriter.Close()
					if websocket.IsCloseError(err, websocket.CloseNormalClosure) {
						return
					}
					log.Errorf("failed to read WebSocket message: %v, clearing connection", err)
					// connection dropped; clear it so the next use reconnects
					p.clearConnection()
					return
				}

				if messageType == websocket.BinaryMessage {
					if _, err := pipeWriter.Write(data); err != nil {
						log.Errorf("failed to write audio data: %v", err)
						return
					}
					return
				}
			}
		}
	}()

	return outputChan, nil
}

// SetVoice sets voice params (EdgeOffline ignores dynamic voice changes but does not error)
func (p *EdgeOfflineTTSProvider) SetVoice(voiceConfig map[string]interface{}) error {
	// EdgeOffline uses WebSocket; voice is controlled server-side and cannot be set dynamically by the client
	// returning nil means success (even though nothing is done)
	return nil
}

// Close releases resources and the connection
func (p *EdgeOfflineTTSProvider) Close() error {
	p.clearConnection()
	return nil
}

// IsValid checks whether the resource is valid
func (p *EdgeOfflineTTSProvider) IsValid() bool {
	p.connMutex.RLock()
	conn := p.conn
	p.connMutex.RUnlock()

	// check whether the connection exists
	return conn != nil
}
