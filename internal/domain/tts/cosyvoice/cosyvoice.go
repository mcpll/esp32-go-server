package cosyvoice

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"xiaozhi-esp32-server-golang/internal/data/audio"
	"xiaozhi-esp32-server-golang/internal/util"
	log "xiaozhi-esp32-server-golang/logger"
)

// Global HTTP client with connection pooling
var (
	httpClient     *http.Client
	httpClientOnce sync.Once
)

// Get HTTP client with connection pool
func getHTTPClient() *http.Client {
	httpClientOnce.Do(func() {
		transport := &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   10,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		}
		httpClient = &http.Client{
			Transport: transport,
			Timeout:   30 * time.Second,
		}
	})
	return httpClient
}

// CosyVoiceTTSProvider CosyVoice TTS provider
type CosyVoiceTTSProvider struct {
	APIURL        string
	SpeakerID     string
	FrameDuration int
	TargetSR      int
	AudioFormat   string
	InstructText  string
}

// Response struct
type cosyVoiceResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    []byte `json:"data"`
}

// NewCosyVoiceTTSProvider creates a CosyVoice TTS provider
func NewCosyVoiceTTSProvider(config map[string]interface{}) *CosyVoiceTTSProvider {
	apiURL, _ := config["api_url"].(string)
	speakerID, _ := config["spk_id"].(string)
	frameDuration, _ := config["frame_duration"].(float64)
	targetSR, _ := config["target_sr"].(float64)
	audioFormat, _ := config["audio_format"].(string)
	instructText, _ := config["instruct_text"].(string)

	// Set defaults
	if apiURL == "" {
		apiURL = "https://tts.linkerai.cn/tts"
	}
	if speakerID == "" {
		speakerID = "OUeAo1mhq6IBExi"
	}
	if frameDuration == 0 {
		frameDuration = audio.FrameDuration
	}
	if targetSR == 0 {
		targetSR = audio.SampleRate
	}
	if audioFormat == "" {
		audioFormat = "mp3"
	}

	return &CosyVoiceTTSProvider{
		APIURL:        apiURL,
		SpeakerID:     speakerID,
		FrameDuration: int(frameDuration),
		TargetSR:      int(targetSR),
		AudioFormat:   audioFormat,
		InstructText:  instructText,
	}
}

// TextToSpeech converts text to speech; returns audio frames and error
func (p *CosyVoiceTTSProvider) TextToSpeech(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) ([][]byte, error) {
	// Build query params
	params := url.Values{}
	params.Add("tts_text", text)
	params.Add("spk_id", p.SpeakerID)
	params.Add("frame_durition", fmt.Sprintf("%d", p.FrameDuration))
	params.Add("stream", "true") // Streaming request
	params.Add("target_sr", fmt.Sprintf("%d", p.TargetSR))
	params.Add("audio_format", p.AudioFormat)

	startTs := time.Now().UnixMilli()

	// Build full URL
	requestURL := fmt.Sprintf("%s?%s", p.APIURL, params.Encode())

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "GET", requestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %v", err)
	}

	req.Header.Set("Accept", "application/json")

	// Send request via connection pool
	client := getHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("发送请求失败: %v", err)
	}
	defer resp.Body.Close()

	// Read response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %v", err)
	}

	// Check response status
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API请求失败，状态码: %d, 响应: %s", resp.StatusCode, string(body))
	}

	// Check content type and content length
	// contentType := resp.Header.Get("Content-Type")
	contentLength := resp.ContentLength

	// Log response length
	log.Debugf("received TTS response, Content-Length: %d", contentLength)

	// Validate Content-Length
	if contentLength == 0 {
		log.Errorf("API returned empty response, Content-Length is 0")
		return nil, fmt.Errorf("API返回空响应，Content-Length为0")
	}

	// MP3 header needs at least 100 bytes to parse
	// -1 means unknown length (e.g. chunked transfer)
	if contentLength > 0 && contentLength < 100 {
		log.Errorf("API response too small to parse as MP3: %d bytes", contentLength)
		return nil, fmt.Errorf("API返回的响应太小无法解析为MP3: %d字节", contentLength)
	}

	// Convert to Opus frames
	if p.AudioFormat == "mp3" {
		// Create a pipe
		doneChan := make(chan struct{})
		outputChan := make(chan []byte, 1000)

		// Create MP3 decoder
		mp3Decoder, err := util.CreateAudioDecoder(ctx, io.NopCloser(bytes.NewReader(body)), outputChan, frameDuration, p.AudioFormat)
		if err != nil {
			close(doneChan)
			return nil, fmt.Errorf("创建MP3解码器失败: %v", err)
		}
		// Start decoding
		go func() {
			if err := mp3Decoder.Run(startTs); err != nil {
				log.Errorf("MP3 decode failed: %v", err)
			}
		}()

		// Collect all Opus frames
		var opusFrames [][]byte
		for frame := range outputChan {
			opusFrames = append(opusFrames, frame)
		}

		return opusFrames, nil
	}

	return nil, fmt.Errorf("不支持的音频格式: %s", p.AudioFormat)
}

// TextToSpeechStream streaming speech synthesis
func (p *CosyVoiceTTSProvider) TextToSpeechStream(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) (outputChan chan []byte, err error) {
	// Build query params
	params := url.Values{}
	params.Add("tts_text", text)
	params.Add("spk_id", p.SpeakerID)
	params.Add("frame_durition", fmt.Sprintf("%d", frameDuration))
	params.Add("stream", "true") // Streaming request
	params.Add("target_sr", fmt.Sprintf("%d", sampleRate))
	params.Add("audio_format", p.AudioFormat)

	startTs := time.Now().UnixMilli()

	// Build full URL
	requestURL := fmt.Sprintf("%s?%s", p.APIURL, params.Encode())

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "GET", requestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %v", err)
	}

	req.Header.Set("Accept", "application/json")

	// Create client with connection pool
	client := getHTTPClient()

	// Create output channel
	outputChan = make(chan []byte, 100)
	// Start goroutine for streaming response
	go func() {
		decoderStarted := false
		defer func() {
			if !decoderStarted {
				close(outputChan)
			}
		}()

		// Send request
		resp, err := client.Do(req)
		if err != nil {
			log.Errorf("failed to send request: %v", err)
			return
		}
		defer func() {
			resp.Body.Close()
		}()

		// Check response status
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			log.Errorf("API request failed, status: %d, body: %s", resp.StatusCode, string(body))
			return
		}

		// Check content type and content length
		// contentType := resp.Header.Get("Content-Type")
		contentLength := resp.ContentLength

		// Log response length
		log.Debugf("received TTS response, Content-Length: %d", contentLength)

		// Validate Content-Length
		if contentLength == 0 {
			log.Errorf("API returned empty response, Content-Length is 0")
			return
		}

		// MP3 header needs at least 100 bytes to parse
		// -1 means unknown length (e.g. chunked transfer)
		if contentLength > 0 && contentLength < 100 {
			log.Errorf("API response too small to parse as MP3: %d bytes", contentLength)
			return
		}

		// Handle streaming response by audio format
		if p.AudioFormat == "mp3" {
			// Create MP3 decoder with context instead of done channel
			mp3Decoder, err := util.CreateAudioDecoder(ctx, resp.Body, outputChan, frameDuration, p.AudioFormat)
			if err != nil {
				log.Errorf("failed to create MP3 decoder: %v", err)
				return
			}

			// Start decoding
			decoderStarted = true
			if err := mp3Decoder.Run(startTs); err != nil {
				log.Errorf("MP3 decode failed: %v", err)
				return
			}

			select {
			case <-ctx.Done():
				log.Debugf("TTS streaming synthesis cancelled, text: %s", text)
				return
			default:
				log.Infof("tts latency: from input to end of MP3 data: %d ms", time.Now().UnixMilli()-startTs)

			}
		} else {
			log.Errorf("only MP3 streaming synthesis is supported")
		}
	}()

	return outputChan, nil
}

// SetVoice set voice params
func (p *CosyVoiceTTSProvider) SetVoice(voiceConfig map[string]interface{}) error {
	if spkID, ok := voiceConfig["spk_id"].(string); ok && spkID != "" {
		p.SpeakerID = spkID
		return nil
	}
	return fmt.Errorf("无效的音色配置: 缺少 spk_id")
}

// Close Close resources (stateless Provider; nothing to close)
func (p *CosyVoiceTTSProvider) Close() error {
	return nil
}

// IsValid check whether the resource is valid
func (p *CosyVoiceTTSProvider) IsValid() bool {
	return p != nil
}
