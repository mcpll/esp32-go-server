package groq_whisper

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"xiaozhi-esp32-server-golang/constants"
	"xiaozhi-esp32-server-golang/internal/domain/asr/types"
	log "xiaozhi-esp32-server-golang/logger"
)

// ASR is a batch Groq Whisper client. StreamingRecognize buffers until the
// audio channel closes, then posts one transcription request.
type ASR struct {
	config Config
	client *http.Client
}

// NewASR creates a Groq Whisper ASR engine.
func NewASR(config Config) (*ASR, error) {
	if strings.TrimSpace(config.APIKey) == "" {
		return nil, fmt.Errorf("groq_whisper: api_key is empty")
	}
	if strings.TrimSpace(config.APIURL) == "" {
		config.APIURL = defaultAPIURL
	}
	if config.SampleRate <= 0 {
		config.SampleRate = defaultSampleRate
	}
	if config.Timeout <= 0 {
		config.Timeout = time.Duration(defaultTimeoutSeconds) * time.Second
	}
	return &ASR{
		config: config,
		client: &http.Client{Timeout: config.Timeout},
	}, nil
}

// Process transcribes a full PCM float32 buffer.
func (a *ASR) Process(pcmData []float32) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), a.config.Timeout)
	defer cancel()
	ch := make(chan []float32, 1)
	ch <- pcmData
	close(ch)
	resultChan, err := a.StreamingRecognize(ctx, ch)
	if err != nil {
		return "", err
	}
	var text string
	for r := range resultChan {
		if r.Error != nil {
			return "", r.Error
		}
		if r.IsFinal {
			text = r.Text
		}
	}
	return text, nil
}

// StreamingRecognize buffers the stream, then calls Groq once.
func (a *ASR) StreamingRecognize(ctx context.Context, audioStream <-chan []float32) (chan types.StreamingResult, error) {
	out := make(chan types.StreamingResult, 2)
	go func() {
		defer close(out)
		samples := make([]float32, 0, 16000)
		for {
			select {
			case <-ctx.Done():
				out <- types.StreamingResult{Error: ctx.Err(), IsFinal: true, AsrType: constants.AsrTypeGroqWhisper}
				return
			case chunk, ok := <-audioStream:
				if !ok {
					text, err := a.transcribe(ctx, samples)
					if err != nil {
						out <- types.StreamingResult{Error: err, IsFinal: true, AsrType: constants.AsrTypeGroqWhisper}
						return
					}
					out <- types.StreamingResult{Text: text, IsFinal: true, AsrType: constants.AsrTypeGroqWhisper}
					return
				}
				samples = append(samples, chunk...)
			}
		}
	}()
	return out, nil
}

func (a *ASR) transcribe(ctx context.Context, samples []float32) (string, error) {
	if len(samples) == 0 {
		return "", nil
	}
	wav, err := float32ToWAV(samples, a.config.SampleRate)
	if err != nil {
		return "", err
	}

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	fw, err := w.CreateFormFile("file", "audio.wav")
	if err != nil {
		return "", err
	}
	if _, err := fw.Write(wav); err != nil {
		return "", err
	}
	_ = w.WriteField("model", a.config.Model)
	if a.config.Language != "" {
		_ = w.WriteField("language", a.config.Language)
	}
	if a.config.ResponseFormat != "" {
		_ = w.WriteField("response_format", a.config.ResponseFormat)
	}
	_ = w.WriteField("temperature", fmt.Sprintf("%g", a.config.Temperature))
	if err := w.Close(); err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.config.APIURL, &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+a.config.APIKey)
	req.Header.Set("Content-Type", w.FormDataContentType())

	log.Debugf("[groq_whisper] POST %s model=%s language=%s samples=%d", a.config.APIURL, a.config.Model, a.config.Language, len(samples))
	resp, err := a.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("groq_whisper request failed: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("groq_whisper HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var parsed struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", fmt.Errorf("groq_whisper decode: %w", err)
	}
	return strings.TrimSpace(parsed.Text), nil
}

// Close is a no-op for the stateless HTTP client.
func (a *ASR) Close() error { return nil }

// IsValid reports whether the engine is usable.
func (a *ASR) IsValid() bool { return a != nil && a.config.APIKey != "" }

func float32ToWAV(samples []float32, sampleRate int) ([]byte, error) {
	pcm := make([]byte, len(samples)*2)
	for i, s := range samples {
		var v int16
		switch {
		case s > 1:
			v = 32767
		case s < -1:
			v = -32768
		default:
			v = int16(s * 32767)
		}
		binary.LittleEndian.PutUint16(pcm[i*2:], uint16(v))
	}
	var buf bytes.Buffer
	dataSize := uint32(len(pcm))
	// RIFF header
	buf.WriteString("RIFF")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(36+dataSize))
	buf.WriteString("WAVE")
	buf.WriteString("fmt ")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(16))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1)) // PCM
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1)) // mono
	_ = binary.Write(&buf, binary.LittleEndian, uint32(sampleRate))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(sampleRate*2)) // byte rate
	_ = binary.Write(&buf, binary.LittleEndian, uint16(2))            // block align
	_ = binary.Write(&buf, binary.LittleEndian, uint16(16))           // bits
	buf.WriteString("data")
	_ = binary.Write(&buf, binary.LittleEndian, dataSize)
	buf.Write(pcm)
	return buf.Bytes(), nil
}
