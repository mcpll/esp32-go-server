package play_music

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"bytes"

	"xiaozhi-esp32-server-golang/internal/util"
	log "xiaozhi-esp32-server-golang/logger"
)

// Global HTTP client with connection pooling
var (
	httpClient     *http.Client
	httpClientOnce sync.Once
)

// Get an HTTP client configured with a connection pool
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
			//Timeout:   30 * time.Second,
		}
	})
	return httpClient
}

// PlayMusicStream plays music from a URL and returns an audio stream channel
// frameDuration: frame duration in ms, default 20ms
// audioFormat: audio format, supports "mp3"
func PlayMusicStream(ctx context.Context, url string, sampleRate int, frameDuration int, audioFormat string) (outputChan chan []byte, err error) {
	// Validate args and apply defaults
	if frameDuration <= 0 {
		frameDuration = 20 // Default 20ms frame duration
	}
	if audioFormat == "" {
		audioFormat = "mp3" // Default MP3 format
	}

	startTs := time.Now().UnixMilli()

	// Create HTTP request
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %v", err)
	}

	req.Header.Set("Accept", "audio/*")
	req.Header.Set("User-Agent", "MusicPlayer/1.0")

	// Create client using the connection pool
	client := getHTTPClient()

	// Create output channel
	outputChan = make(chan []byte, 100)

	// Start goroutine to handle the streaming response
	go func() {
		// Send request
		resp, err := client.Do(req)
		if err != nil {
			log.Errorf("failed to send request: %v", err)
			close(outputChan)
			return
		}
		defer func() {
			resp.Body.Close()
		}()

		// Check response status code
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			log.Errorf("API request failed, status: %d, body: %s", resp.StatusCode, string(body))
			close(outputChan)
			return
		}

		// Check response content type and length
		contentLength := resp.ContentLength

		// Log response length
		log.Debugf("received music stream response, Content-Length: %d", contentLength)

		// Check whether Content-Length is reasonable
		if contentLength == 0 {
			log.Errorf("music stream returned empty response, Content-Length is 0")
			close(outputChan)
			return
		}

		// MP3 headers need at least 100 bytes to parse
		// -1 means unknown length (e.g. chunked transfer)
		if contentLength > 0 && contentLength < 100 {
			log.Errorf("music stream response too small to parse as MP3: %d bytes", contentLength)
			close(outputChan)
			return
		}

		log.Infof("starting music playback: %s", url)

		// Handle streaming response by audio format
		if audioFormat == "mp3" {
			// Create MP3 decoder with context instead of a done channel
			mp3Decoder, err := util.CreateAudioDecoderWithSampleRate(ctx, resp.Body, outputChan, frameDuration, audioFormat, sampleRate)
			if err != nil {
				log.Errorf("failed to create MP3 decoder: %v", err)
				close(outputChan)
				return
			}

			// Start decoding
			if err := mp3Decoder.Run(startTs); err != nil {
				log.Errorf("MP3 decode failed: %v", err)
				return
			}

			select {
			case <-ctx.Done():
				log.Debugf("music playback cancelled, URL: %s", url)
				return
			default:
				log.Infof("music playback finished in %d ms", time.Now().UnixMilli()-startTs)
			}
		} else {
			log.Errorf("only MP3 streaming playback is supported, got format: %s", audioFormat)
			close(outputChan)
		}
	}()

	return outputChan, nil
}

func PlayMusicFromAudioData(ctx context.Context, audioData []byte, sampleRate int, frameDuration int, audioFormat string) (outputChan chan []byte, err error) {
	// Validate args and apply defaults
	if frameDuration <= 0 {
		frameDuration = 20 // Default 20ms frame duration
	}
	if audioFormat == "" {
		audioFormat = "mp3" // Default MP3 format
	}

	// Add debug info
	log.Debugf("PlayMusicFromAudioData: audio_len=%d bytes, sample_rate=%d, frame_duration=%dms, format=%s",
		len(audioData), sampleRate, frameDuration, audioFormat)

	// Check whether audio data is empty
	if len(audioData) == 0 {
		log.Errorf("audio data is empty, cannot play")
		return nil, fmt.Errorf("音频数据为空")
	}

	startTs := time.Now().UnixMilli()

	// Create output channel
	outputChan = make(chan []byte, 100)

	// Start goroutine to handle the streaming response
	go func() {
		// Create an io.ReadCloser from audioData
		audioReader := io.NopCloser(bytes.NewReader(audioData))

		// Handle streaming response by audio format
		if audioFormat == "mp3" {
			// Create MP3 decoder with context instead of a done channel
			mp3Decoder, err := util.CreateAudioDecoderWithSampleRate(ctx, audioReader, outputChan, frameDuration, audioFormat, sampleRate)
			if err != nil {
				log.Errorf("failed to create MP3 decoder: %v", err)
				return
			}

			// Start decoding
			if err := mp3Decoder.Run(startTs); err != nil {
				log.Errorf("MP3 decode failed: %v", err)
				return
			}

			select {
			case <-ctx.Done():
				log.Debugf("music playback cancelled")
				return
			default:
				log.Infof("music playback finished in %d ms", time.Now().UnixMilli()-startTs)
			}
		} else {
			log.Errorf("only MP3 streaming playback is supported, got format: %s", audioFormat)
		}
	}()

	return outputChan, nil
}

func PlayMusicFromPipe(ctx context.Context, pipeReader *io.PipeReader, sampleRate int, frameDuration int, audioFormat string) (outputChan chan []byte, err error) {
	// Validate args and apply defaults
	if frameDuration <= 0 {
		frameDuration = 20 // Default 20ms frame duration
	}
	if audioFormat == "" {
		audioFormat = "mp3" // Default MP3 format
	}

	// Add debug info
	log.Debugf("PlayMusicFromPipe: sample_rate=%d, frame_duration=%dms, format=%s",
		sampleRate, frameDuration, audioFormat)

	startTs := time.Now().UnixMilli()

	// Create output channel
	outputChan = make(chan []byte, 100)

	// Start goroutine to handle the streaming response
	go func() {
		// Handle streaming response by audio format
		if audioFormat == "mp3" {
			// Create MP3 decoder with context instead of a done channel
			mp3Decoder, err := util.CreateAudioDecoderWithSampleRate(ctx, pipeReader, outputChan, frameDuration, audioFormat, sampleRate)
			if err != nil {
				log.Errorf("failed to create MP3 decoder: %v", err)
				return
			}

			// Start decoding
			if err := mp3Decoder.Run(startTs); err != nil {
				log.Errorf("MP3 decode failed: %v", err)
				return
			}

			select {
			case <-ctx.Done():
				log.Debugf("music playback cancelled")
				return
			default:
				log.Infof("music playback finished in %d ms", time.Now().UnixMilli()-startTs)
			}
		} else {
			log.Errorf("only MP3 streaming playback is supported, got format: %s", audioFormat)
		}
	}()

	return outputChan, nil
}
