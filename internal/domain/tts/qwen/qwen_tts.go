package qwen

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
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

const (
	defaultAPIURLBeijing    = "https://dashscope.aliyuncs.com/api/v1/services/aigc/multimodal-generation/generation"
	defaultAPIURLSingapore  = "https://dashscope-intl.aliyuncs.com/api/v1/services/aigc/multimodal-generation/generation"
	defaultQwenModel        = "qwen3-tts-flash"
	defaultQwenVoice        = "Cherry"
	defaultQwenLanguageType = "Chinese"
)

// global HTTP client with connection pooling
var (
	httpClient     *http.Client
	httpClientOnce sync.Once
)

// returns the pooled HTTP client
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

// QwenTTSProvider Alibaba Cloud Qwen TTS provider
type QwenTTSProvider struct {
	APIKey        string
	APIURL        string
	Model         string
	Voice         string
	LanguageType  string
	Stream        bool
	FrameDuration int
}

// qwenRequest request struct
type qwenRequest struct {
	Model string           `json:"model"`
	Input qwenRequestInput `json:"input"`
}

type qwenRequestInput struct {
	Text         string `json:"text"`
	Voice        string `json:"voice"`
	LanguageType string `json:"language_type,omitempty"`
}

// qwenResponse unified response for non-streaming and streaming
type qwenResponse struct {
	StatusCode int        `json:"status_code"`
	RequestID  string     `json:"request_id"`
	Code       string     `json:"code"`
	Message    string     `json:"message"`
	Output     qwenOutput `json:"output"`
	Usage      qwenUsage  `json:"usage"`
}

type qwenOutput struct {
	Text         interface{}   `json:"text"`
	FinishReason string        `json:"finish_reason"`
	Choices      interface{}   `json:"choices"`
	Audio        qwenAudioInfo `json:"audio"`
}

type qwenAudioInfo struct {
	Data      string `json:"data"`       // Base64 audio for streaming (16-bit PCM)
	URL       string `json:"url"`        // WAV URL for non-streaming output
	ID        string `json:"id"`         // audio ID
	ExpiresAt int64  `json:"expires_at"` // URL expiry timestamp
}

type qwenUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	Characters   int `json:"characters"`
}

// NewQwenTTSProvider creates a new Alibaba Cloud Qwen TTS provider
func NewQwenTTSProvider(config map[string]interface{}) *QwenTTSProvider {
	apiKey, _ := config["api_key"].(string)
	apiURL, _ := config["api_url"].(string)
	model, _ := config["model"].(string)
	voice, _ := config["voice"].(string)
	languageType, _ := config["language_type"].(string)
	stream, _ := config["stream"].(bool)
	frameDuration, _ := config["frame_duration"].(float64)
	region, _ := config["region"].(string)

	// normalize API URL / region
	if apiURL == "" {
		if strings.EqualFold(region, "singapore") {
			apiURL = defaultAPIURLSingapore
		} else {
			apiURL = defaultAPIURLBeijing
		}
	}

	// defaults
	if model == "" {
		model = defaultQwenModel
	}
	if voice == "" {
		voice = defaultQwenVoice
	}
	if languageType == "" {
		languageType = defaultQwenLanguageType
	}
	if frameDuration == 0 {
		frameDuration = audio.FrameDuration
	}

	return &QwenTTSProvider{
		APIKey:        apiKey,
		APIURL:        apiURL,
		Model:         model,
		Voice:         voice,
		LanguageType:  languageType,
		Stream:        stream,
		FrameDuration: int(frameDuration),
	}
}

// TextToSpeech non-streaming TTS: call HTTP, download WAV, decode to frames
func (p *QwenTTSProvider) TextToSpeech(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) ([][]byte, error) {
	startTs := time.Now().UnixMilli()

	// build the request body
	reqBody := qwenRequest{
		Model: p.Model,
		Input: qwenRequestInput{
			Text:         text,
			Voice:        p.Voice,
			LanguageType: p.LanguageType,
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %v", err)
	}

	// create the HTTP request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.APIURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", p.APIKey))

	client := getHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("发送请求失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API请求失败，状态码: %d, 响应: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %v", err)
	}

	var ttsResp qwenResponse
	if err := json.Unmarshal(body, &ttsResp); err != nil {
		return nil, fmt.Errorf("解析响应失败: %v, 响应体: %s", err, string(body))
	}

	if ttsResp.StatusCode != 200 {
		return nil, fmt.Errorf("千问 TTS API 错误 [%s]: %s", ttsResp.Code, ttsResp.Message)
	}

	if ttsResp.Output.Audio.URL == "" {
		return nil, fmt.Errorf("响应中未包含音频 URL")
	}

	log.Debugf("Qwen TTS non-stream, downloading audio URL: %s", ttsResp.Output.Audio.URL)

	// download WAV and decode to frames via the shared decoder
	wavReq, err := http.NewRequestWithContext(ctx, http.MethodGet, ttsResp.Output.Audio.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("创建音频下载请求失败: %v", err)
	}

	wavResp, err := client.Do(wavReq)
	if err != nil {
		return nil, fmt.Errorf("下载音频失败: %v", err)
	}
	defer wavResp.Body.Close()

	if wavResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(wavResp.Body)
		return nil, fmt.Errorf("下载音频失败，状态码: %d, 响应: %s", wavResp.StatusCode, string(body))
	}

	outputChan := make(chan []byte, 1000)

	decoder, err := util.CreateAudioDecoderWithSampleRate(ctx, wavResp.Body, outputChan, frameDuration, "wav", sampleRate)
	if err != nil {
		return nil, fmt.Errorf("创建千问音频解码器失败: %v", err)
	}

	// start decoding
	go func() {
		if err := decoder.Run(startTs); err != nil {
			log.Errorf("Qwen TTS non-stream audio decode failed: %v", err)
		}
	}()

	var frames [][]byte
	for frame := range outputChan {
		frames = append(frames, frame)
	}

	log.Debugf("Qwen TTS non-stream done, elapsed from input to end of audio: %d ms", time.Now().UnixMilli()-startTs)
	return frames, nil
}

// TextToSpeechStream streaming TTS implementation
func (p *QwenTTSProvider) TextToSpeechStream(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) (outputChan chan []byte, err error) {

	startTs := time.Now().UnixMilli()

	// build the request body
	reqBody := qwenRequest{
		Model: p.Model,
		Input: qwenRequestInput{
			Text:         text,
			Voice:        p.Voice,
			LanguageType: p.LanguageType,
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %v", err)
	}

	// create the HTTP request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.APIURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", p.APIKey))
	req.Header.Set("X-DashScope-SSE", "enable") // enable streaming output

	client := getHTTPClient()

	outputChan = make(chan []byte, 100)

	go func() {

		resp, err := client.Do(req)
		if err != nil {
			log.Errorf("failed to send Qwen stream request: %v", err)
			close(outputChan)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			log.Errorf("Qwen stream API request failed, status=%d, body: %s", resp.StatusCode, string(body))
			close(outputChan)
			return
		}

		contentType := resp.Header.Get("Content-Type")
		if !strings.Contains(contentType, "text/event-stream") {
			log.Warnf("Qwen stream API Content-Type is not text/event-stream: %s", contentType)
			close(outputChan)
			return
		}

		// pipeline: parse SSE -> PCM -> decode to frames
		pipeReader, pipeWriter := io.Pipe()

		// parse SSE and write raw PCM data.
		// Qwen streaming audio.data may include a one-time WAV header in practice; strip it before treating as PCM.
		go func() {
			defer func() {
				if err := pipeWriter.Close(); err != nil {
					log.Debugf("failed to close Qwen pipe writer: %v", err)
				}
			}()

			if err := p.parseEventStream(ctx, resp.Body, pipeWriter, text); err != nil {
				log.Errorf("failed to parse Qwen Event Stream: %v", err)
			}
		}()

		// create an audio decoder reading PCM from the pipe and emitting Opus frames
		decoder, err := util.CreateAudioDecoderWithSampleRate(
			ctx,
			pipeReader,
			outputChan,
			frameDuration,
			"pcm", // parseEventStream strips the WAV header when needed and outputs raw 16-bit PCM
			sampleRate,
		)
		if err != nil {
			log.Errorf("failed to create Qwen stream audio decoder: %v", err)
			close(outputChan)
			pipeReader.Close()
			return
		}

		// tell the decoder the PCM sample rate / channel layout
		decoder.WithFormat(beep.Format{
			SampleRate:  beep.SampleRate(24000),
			NumChannels: 1,
		})

		// decoder.Run() closes outputChan internally
		// use sync.Once so defer does not double-close if decoder.Run() already closed the channel
		if err := decoder.Run(startTs); err != nil {
			log.Errorf("Qwen stream audio decode failed: %v", err)
			return
		}

		// if decoder.Run() completes successfully it closes the channel
		// so skip the deferred close (handled via sync.Once)

		select {
		case <-ctx.Done():
			log.Debugf("Qwen TTS stream synthesis canceled, text: %s", text)
			return
		default:
			log.Debugf("Qwen TTS stream elapsed from input to end of audio: %d ms", time.Now().UnixMilli()-startTs)
		}
	}()

	return outputChan, nil
}

// parseEventStream parses Alibaba Cloud Qwen SSE with go-sse, decodes Base64 PCM, and writes to the pipe
func (p *QwenTTSProvider) parseEventStream(ctx context.Context, reader io.Reader, writer *io.PipeWriter, text string) error {
	var leadingAudio bytes.Buffer
	wroteLeadingAudio := false

	for ev, evErr := range sse.Read(reader, nil) {
		if evErr != nil {
			return fmt.Errorf("读取千问 SSE 事件失败: %w", evErr)
		}

		select {
		case <-ctx.Done():
			log.Debugf("Qwen TTS stream synthesis canceled, text: %s", text)
			return ctx.Err()
		default:
		}

		dataValue := strings.TrimSpace(ev.Data)
		if dataValue == "" {
			continue
		}

		var eventResp qwenResponse
		if err := json.Unmarshal([]byte(dataValue), &eventResp); err != nil {
			log.Warnf("failed to parse Qwen Event Stream JSON: %v, data: %s", err, previewString(dataValue, 200))
			continue
		}

		// check business status (streaming data may omit status_code; 0 means success)
		if eventResp.StatusCode != 0 && eventResp.StatusCode != 200 {
			return fmt.Errorf("千问流式 API 错误 [%s]: %s", eventResp.Code, eventResp.Message)
		}

		// decode Base64 PCM data
		if eventResp.Output.Audio.Data != "" {
			encoded := cleanBase64(eventResp.Output.Audio.Data)
			audioBytes, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil {
				log.Errorf("failed to decode Qwen Base64 PCM: %v", err)
				continue
			}

			if len(audioBytes) > 0 {
				if !wroteLeadingAudio {
					leadingAudio.Write(audioBytes)
					normalized, needMore, detectedWAV, err := normalizeLeadingQwenAudio(leadingAudio.Bytes())
					if err != nil {
						return fmt.Errorf("解析千问流式音频头失败: %w", err)
					}
					if needMore {
						continue
					}
					wroteLeadingAudio = true
					if detectedWAV {
						log.Infof("Qwen stream audio has WAV header; stripped and treating as PCM")
					}
					if len(normalized) == 0 {
						continue
					}
					if _, err := writer.Write(normalized); err != nil {
						return fmt.Errorf("写入 PCM 到管道失败: %v", err)
					}
					continue
				}

				if _, err := writer.Write(audioBytes); err != nil {
					return fmt.Errorf("写入 PCM 到管道失败: %v", err)
				}
			}
		}

		// check whether finished
		if eventResp.Output.FinishReason == "stop" {
			log.Debugf("Qwen stream got finish_reason=stop, request ID: %s", eventResp.RequestID)
			return nil
		}
	}

	return nil
}

func normalizeLeadingQwenAudio(data []byte) (normalized []byte, needMore bool, detectedWAV bool, err error) {
	if len(data) < 12 {
		return nil, true, false, nil
	}

	if !bytes.HasPrefix(data, []byte("RIFF")) || !bytes.Equal(data[8:12], []byte("WAVE")) {
		return data, false, false, nil
	}

	offset, needMore, err := qwenWAVDataOffset(data)
	if err != nil {
		return nil, false, true, err
	}
	if needMore {
		return nil, true, true, nil
	}
	if offset > len(data) {
		return nil, false, true, fmt.Errorf("WAV data offset 越界: %d > %d", offset, len(data))
	}
	return data[offset:], false, true, nil
}

func qwenWAVDataOffset(data []byte) (offset int, needMore bool, err error) {
	if len(data) < 12 {
		return 0, true, nil
	}
	if !bytes.HasPrefix(data, []byte("RIFF")) || !bytes.Equal(data[8:12], []byte("WAVE")) {
		return 0, false, fmt.Errorf("不是有效的 WAV 头")
	}

	offset = 12
	for {
		if len(data) < offset+8 {
			return 0, true, nil
		}

		chunkID := string(data[offset : offset+4])
		chunkSize := int(binary.LittleEndian.Uint32(data[offset+4 : offset+8]))
		if chunkSize < 0 {
			return 0, false, fmt.Errorf("非法 WAV chunk size: %d", chunkSize)
		}
		offset += 8

		if chunkID == "data" {
			return offset, false, nil
		}

		nextOffset := offset + chunkSize
		if chunkSize%2 == 1 {
			nextOffset++
		}
		if len(data) < nextOffset {
			return 0, true, nil
		}
		offset = nextOffset
	}
}

// SetVoice sets the voice
func (p *QwenTTSProvider) SetVoice(voiceConfig map[string]interface{}) error {
	if voice, ok := voiceConfig["voice"].(string); ok && voice != "" {
		p.Voice = voice
		return nil
	}
	return fmt.Errorf("无效的音色配置: 缺少 voice")
}

// Close closes resources (stateless provider; nothing to close)
func (p *QwenTTSProvider) Close() error {
	return nil
}

// IsValid checks whether the resource is valid
func (p *QwenTTSProvider) IsValid() bool {
	return p != nil
}

// cleanBase64 removes all whitespace from a Base64 string
func cleanBase64(s string) string {
	if s == "" {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch == ' ' || ch == '\n' || ch == '\r' || ch == '\t' {
			continue
		}
		b.WriteByte(ch)
	}
	return b.String()
}

// previewString returns the first n characters of a string for logging
func previewString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
