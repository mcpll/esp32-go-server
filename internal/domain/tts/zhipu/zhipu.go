package zhipu

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"xiaozhi-esp32-server-golang/internal/data/audio"
	"xiaozhi-esp32-server-golang/internal/util"
	log "xiaozhi-esp32-server-golang/logger"

	"github.com/gopxl/beep"
	sse "github.com/tmaxmax/go-sse"
)

// Global HTTP client with connection pooling
var (
	httpClient     *http.Client
	httpClientOnce sync.Once
)

const (
	zhipuDefaultSampleRate = 24000
	zhipuLeadingFadeInMs   = 5
)

// Get HTTP client configured with connection pool
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
			Timeout:   60 * time.Second,
		}
	})
	return httpClient
}

// ZhipuTTSProvider Zhipu TTS provider
type ZhipuTTSProvider struct {
	APIKey         string
	APIURL         string
	Model          string
	Voice          string
	ResponseFormat string
	Speed          float64
	Volume         float64
	Stream         bool
	EncodeFormat   string // Streaming only: base64 or hex
	FrameDuration  int
}

// Request struct (per Zhipu API docs)
type zhipuRequest struct {
	Model          string  `json:"model"`
	Input          string  `json:"input"`
	Voice          string  `json:"voice"`
	ResponseFormat string  `json:"response_format,omitempty"`
	Speed          float64 `json:"speed,omitempty"`
	Volume         float64 `json:"volume,omitempty"`
	Stream         bool    `json:"stream,omitempty"`
	EncodeFormat   string  `json:"encode_format,omitempty"` // Streaming only: base64 or hex
}

// Event Stream response struct (OpenAI-like)
type zhipuEventStreamResponse struct {
	ID      string `json:"id"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index        int    `json:"index"`
		FinishReason string `json:"finish_reason,omitempty"`
		Delta        struct {
			Role             string `json:"role,omitempty"`
			Content          string `json:"content,omitempty"` // base64-encoded audio data
			ReturnSampleRate int    `json:"return_sample_rate,omitempty"`
			ReturnFormat     string `json:"return_format,omitempty"`
		} `json:"delta"`
	} `json:"choices"`
}

// NewZhipuTTSProvider creates a Zhipu TTS provider
func NewZhipuTTSProvider(config map[string]interface{}) *ZhipuTTSProvider {
	apiKey, _ := config["api_key"].(string)
	apiURL, _ := config["api_url"].(string)
	model, _ := config["model"].(string)
	voice, _ := config["voice"].(string)
	responseFormat, _ := config["response_format"].(string)
	speed, _ := config["speed"].(float64)
	volume, _ := config["volume"].(float64)
	stream, _ := config["stream"].(bool)
	encodeFormat, _ := config["encode_format"].(string)
	frameDuration, _ := config["frame_duration"].(float64)

	// Set defaults
	if apiURL == "" {
		apiURL = "https://open.bigmodel.cn/api/paas/v4/audio/speech"
	}
	if model == "" {
		model = "glm-tts"
	}
	if voice == "" {
		voice = "tongtong" // Default voice
	}
	if responseFormat == "" {
		responseFormat = "pcm" // Zhipu default pcm; also supports wav
	}
	if speed == 0 {
		speed = 1.0 // 0.5 to 2.0
	}
	if volume == 0 {
		volume = 1.0 // 0 to 10
	}
	if encodeFormat == "" {
		encodeFormat = "base64" // Default base64; also supports hex
	}
	if frameDuration == 0 {
		frameDuration = audio.FrameDuration
	}

	return &ZhipuTTSProvider{
		APIKey:         apiKey,
		APIURL:         apiURL,
		Model:          model,
		Voice:          voice,
		ResponseFormat: responseFormat,
		Stream:         stream,
		Speed:          speed,
		Volume:         volume,
		EncodeFormat:   encodeFormat,
		FrameDuration:  int(frameDuration),
	}
}

// TextToSpeech converts text to speech; returns audio frames and error
func (p *ZhipuTTSProvider) TextToSpeech(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) ([][]byte, error) {
	startTs := time.Now().UnixMilli()

	// Cap text length (Zhipu API max 1024 chars)
	if len(text) > 1024 {
		text = text[:1024]
		log.Warnf("text longer than 1024 characters, truncated")
	}

	// Build request body
	reqBody := zhipuRequest{
		Model:          p.Model,
		Input:          text,
		Voice:          p.Voice,
		ResponseFormat: p.ResponseFormat,
		Speed:          p.Speed,
		Volume:         p.Volume,
		Stream:         false, // Non-streaming
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %v", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", p.APIURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %v", err)
	}

	// Set request headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", p.APIKey))

	// Send request via connection pool
	client := getHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("发送请求失败: %v", err)
	}
	defer resp.Body.Close()

	// Check response status code
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API请求失败，状态码: %d, 响应: %s", resp.StatusCode, string(body))
	}

	// Check response content length
	contentLength := resp.ContentLength
	log.Debugf("received Zhipu TTS response, Content-Length: %d", contentLength)

	// Check whether Content-Length is reasonable
	if contentLength == 0 {
		log.Errorf("API returned empty response, Content-Length is 0")
		return nil, fmt.Errorf("API返回空响应，Content-Length为0")
	}

	// Handle response by audio format (Zhipu: wav and pcm only)
	if p.ResponseFormat == "wav" || p.ResponseFormat == "pcm" {
		audioReader := io.ReadCloser(resp.Body)
		if strings.EqualFold(p.ResponseFormat, "pcm") {
			pcmData, err := io.ReadAll(resp.Body)
			if err != nil {
				return nil, fmt.Errorf("读取智谱 PCM 数据失败: %v", err)
			}
			audioReader = io.NopCloser(bytes.NewReader(
				applyPCM16MonoLeadingFadeIn(pcmData, leadingFadeInSampleCount(zhipuDefaultSampleRate, zhipuLeadingFadeInMs)),
			))
		}

		// Channel to collect audio frames
		outputChan := make(chan []byte, 1000)

		// Create audio decoder
		decoder, err := util.CreateAudioDecoderWithSampleRate(ctx, audioReader, outputChan, frameDuration, p.ResponseFormat, sampleRate)
		if err != nil {
			return nil, fmt.Errorf("创建音频解码器失败: %v", err)
		}
		if strings.EqualFold(p.ResponseFormat, "pcm") {
			decoder.WithFormat(beep.Format{
				SampleRate:  beep.SampleRate(zhipuDefaultSampleRate),
				NumChannels: 1,
			})
		}

		// Start decoding
		go func() {
			if err := decoder.Run(startTs); err != nil {
				log.Errorf("audio decode failed: %v", err)
			}
		}()

		// Collect all audio frames
		var audioFrames [][]byte
		for frame := range outputChan {
			audioFrames = append(audioFrames, frame)
		}

		log.Debugf("Zhipu TTS done, elapsed from input to end of audio: %d ms", time.Now().UnixMilli()-startTs)
		return audioFrames, nil
	}

	return nil, fmt.Errorf("不支持的音频格式: %s，智谱仅支持 wav 和 pcm", p.ResponseFormat)
}

// TextToSpeechStream streaming speech synthesis
func (p *ZhipuTTSProvider) TextToSpeechStream(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) (outputChan chan []byte, err error) {
	startTs := time.Now().UnixMilli()

	// Cap text length (Zhipu API max 1024 chars)
	if len(text) > 1024 {
		text = text[:1024]
		log.Warnf("text longer than 1024 characters, truncated")
	}

	// Streaming supports pcm and wav only
	responseFormat := p.ResponseFormat

	// Build request body
	reqBody := zhipuRequest{
		Model:          p.Model,
		Input:          text,
		Voice:          p.Voice,
		ResponseFormat: responseFormat,
		Speed:          p.Speed,
		Volume:         p.Volume,
		Stream:         true,           // Streaming
		EncodeFormat:   p.EncodeFormat, // Use configured encode format
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %v", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", p.APIURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %v", err)
	}

	// Set request headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", p.APIKey))

	// Create client with connection pool
	client := getHTTPClient()

	// Create output channel
	outputChan = make(chan []byte, 100)

	// Goroutine to handle streaming response
	go func() {
		// Send request
		resp, err := client.Do(req)
		if err != nil {
			log.Errorf("failed to send Zhipu request: %v", err)
			close(outputChan)
			return
		}
		defer resp.Body.Close()

		// Check response status code
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			log.Errorf("Zhipu API request failed, status=%d, body: %s", resp.StatusCode, string(body))
			close(outputChan)
			return
		}

		// Check Content-Type is Event Stream
		contentType := resp.Header.Get("Content-Type")
		if !strings.Contains(contentType, "text/event-stream") {
			log.Warnf("Zhipu API Content-Type is not text/event-stream: %s", contentType)
		}

		// Streaming supports pcm and wav only
		//log.Debugf("Zhipu TTS stream responseFormat(request): %s", responseFormat)
		if responseFormat == "pcm" || responseFormat == "wav" {
			// Pipe decoded binary data to the audio decoder
			pipeReader, pipeWriter := io.Pipe()

			// Goroutine to parse Event Stream and decode
			go func() {
				defer func() {
					if err := pipeWriter.Close(); err != nil {
						log.Debugf("failed to close pipe writer: %v", err)
					}
				}()

				// Call standalone parse method
				if err := p.parseEventStream(ctx, resp.Body, pipeWriter, text); err != nil {
					log.Errorf("failed to parse Event Stream: %v", err)
				}
			}()

			// Audio decoder reading decoded binary from the pipe
			decoder, err := util.CreateAudioDecoderWithSampleRate(ctx, pipeReader, outputChan, frameDuration, responseFormat, sampleRate)
			if err != nil {
				log.Errorf("failed to create Zhipu audio decoder: %v", err)
				pipeReader.Close()
				close(outputChan)
				return
			}
			if strings.EqualFold(responseFormat, "pcm") {
				decoder.WithFormat(beep.Format{
					SampleRate:  beep.SampleRate(zhipuDefaultSampleRate),
					NumChannels: 1,
				})
			}

			// Start decoding
			if err := decoder.Run(startTs); err != nil {
				log.Errorf("Zhipu audio decode failed: %v", err)
				return
			}

			select {
			case <-ctx.Done():
				log.Debugf("Zhipu TTS stream synthesis canceled, text: %s", text)
				return
			default:
				log.Debugf("Zhipu TTS elapsed from input to end of audio: %d ms", time.Now().UnixMilli()-startTs)
			}
		} else {
			log.Errorf("Zhipu stream output supports pcm only")
			close(outputChan)
		}
	}()

	return outputChan, nil
}

// parseEventStream parses Zhipu Event Stream via go-sse; decode and write to pipe
// ctx: context for cancellation
// reader: response body reader
// writer: pipe write end for decoded binary
// text: original text for logging
func (p *ZhipuTTSProvider) parseEventStream(ctx context.Context, reader io.Reader, writer *io.PipeWriter, text string) error {
	// Configure go-sse ReadConfig with larger MaxEventSize for long tokens
	// Zhipu TTS base64 audio may exceed the default 64KB limit
	readConfig := &sse.ReadConfig{
		MaxEventSize: 4 * 1024 * 1024, // 4MB; enough for large base64 audio
	}
	fadeTotalSamples := 0
	fadeSamplesRemaining := -1

	for ev, evErr := range sse.Read(reader, readConfig) {
		if evErr != nil {
			return fmt.Errorf("读取智谱 SSE 事件失败: %w", evErr)
		}

		select {
		case <-ctx.Done():
			log.Debugf("Zhipu TTS stream synthesis canceled, text: %s", text)
			return ctx.Err()
		default:
		}

		// Event Stream format:
		// data: {"id":"...","choices":[{"delta":{"content":"base64_data"}}]}
		// data: {"choices":[{"finish_reason":"stop"}]}

		dataValue := strings.TrimSpace(ev.Data)
		if dataValue == "" {
			continue
		}

		// Parse JSON
		var eventResp zhipuEventStreamResponse
		if err := json.Unmarshal([]byte(dataValue), &eventResp); err != nil {
			log.Warnf("failed to parse Zhipu Event Stream JSON: %v, data: %s", err, previewString(dataValue, 200))
			continue
		}

		// Check finish_reason for end of stream
		for _, choice := range eventResp.Choices {
			if choice.FinishReason == "stop" {
				log.Debugf("received finish_reason: stop, Event Stream ended")
				return nil
			}
		}

		// Extract and handle each choice content field independently
		for _, choice := range eventResp.Choices {
			if choice.Delta.Content != "" {
				decodedData, err := p.decodeAudioContent(choice.Delta.Content)
				if err != nil {
					return fmt.Errorf("处理 content 失败: %v", err)
				}

				returnFormat := strings.TrimSpace(choice.Delta.ReturnFormat)
				if returnFormat == "" {
					returnFormat = p.ResponseFormat
				}
				if strings.EqualFold(returnFormat, "pcm") {
					if fadeSamplesRemaining < 0 {
						sampleRate := choice.Delta.ReturnSampleRate
						if sampleRate < 1 {
							sampleRate = zhipuDefaultSampleRate
						}
						fadeTotalSamples = leadingFadeInSampleCount(sampleRate, zhipuLeadingFadeInMs)
						fadeSamplesRemaining = fadeTotalSamples
					}
					applyPCM16MonoLeadingFadeInInPlace(decodedData, fadeTotalSamples, &fadeSamplesRemaining)
				}

				if len(decodedData) > 0 {
					if _, err := writer.Write(decodedData); err != nil {
						return fmt.Errorf("写入管道失败: %v", err)
					}
				}
			}
		}
	}

	return nil
}

// previewString returns the first n chars for logging
func previewString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// decodeAudioContent decodes a single content field
// content: base64- or hex-encoded audio string
func (p *ZhipuTTSProvider) decodeAudioContent(content string) ([]byte, error) {
	if content == "" {
		return nil, nil
	}

	// Decode per encode_format
	var decodedData []byte
	var decodeErr error

	switch p.EncodeFormat {
	case "base64":
		decodedData, decodeErr = base64.StdEncoding.DecodeString(content)
	case "hex":
		decodedData, decodeErr = hex.DecodeString(content)
	default:
		log.Warnf("unknown encoding format: %s, using base64", p.EncodeFormat)
		decodedData, decodeErr = base64.StdEncoding.DecodeString(content)
	}

	if decodeErr != nil {
		return nil, fmt.Errorf("解码音频数据失败: %v, 数据长度: %d", decodeErr, len(content))
	}

	return decodedData, nil
}

func leadingFadeInSampleCount(sampleRate int, fadeMs int) int {
	if sampleRate < 1 {
		sampleRate = zhipuDefaultSampleRate
	}
	if fadeMs < 1 {
		return 0
	}
	samples := sampleRate * fadeMs / 1000
	if samples < 1 {
		return 1
	}
	return samples
}

func applyPCM16MonoLeadingFadeIn(data []byte, remainingSamples int) []byte {
	if len(data) == 0 || remainingSamples <= 0 {
		return data
	}
	cloned := make([]byte, len(data))
	copy(cloned, data)
	applyPCM16MonoLeadingFadeInInPlace(cloned, remainingSamples, &remainingSamples)
	return cloned
}

func applyPCM16MonoLeadingFadeInInPlace(data []byte, totalSamples int, remainingSamples *int) {
	if len(data) < 2 || totalSamples <= 0 || remainingSamples == nil || *remainingSamples <= 0 {
		return
	}

	samplePairs := len(data) / 2
	for i := 0; i < samplePairs && *remainingSamples > 0; i++ {
		offset := i * 2
		sample := int16(uint16(data[offset]) | uint16(data[offset+1])<<8)
		appliedIndex := totalSamples - *remainingSamples
		scaled := int32(sample) * int32(appliedIndex) / int32(totalSamples)
		binarySample := uint16(int16(scaled))
		data[offset] = byte(binarySample)
		data[offset+1] = byte(binarySample >> 8)
		*remainingSamples = *remainingSamples - 1
	}
}

// SetVoice sets voice parameters
func (p *ZhipuTTSProvider) SetVoice(voiceConfig map[string]interface{}) error {
	if voice, ok := voiceConfig["voice"].(string); ok && voice != "" {
		p.Voice = voice
		return nil
	}
	return fmt.Errorf("无效的音色配置: 缺少 voice")
}

// Close releases resources (stateless Provider; no-op)
func (p *ZhipuTTSProvider) Close() error {
	return nil
}

// IsValid checks whether the resource is valid
func (p *ZhipuTTSProvider) IsValid() bool {
	return p != nil
}
