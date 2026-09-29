package edge

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"xiaozhi-esp32-server-golang/internal/util"
	log "xiaozhi-esp32-server-golang/logger"

	"github.com/difyz9/edge-tts-go/pkg/communicate"
)

// EdgeTTSProvider Edge TTS provider
// supports one-shot and streaming TTS; outputs Opus frames
// config: voice, rate, volume, pitch, connectTimeout, receiveTimeout
type EdgeTTSProvider struct {
	Voice          string
	Rate           string
	Volume         string
	Pitch          string
	ConnectTimeout int
	ReceiveTimeout int
}

// NewEdgeTTSProvider creates an EdgeTTSProvider
func NewEdgeTTSProvider(config map[string]interface{}) *EdgeTTSProvider {
	voice, _ := config["voice"].(string)
	rate, _ := config["rate"].(string)
	volume, _ := config["volume"].(string)
	pitch, _ := config["pitch"].(string)
	connectTimeout, _ := config["connect_timeout"].(int)
	receiveTimeout, _ := config["receive_timeout"].(int)
	if rate == "" {
		rate = "+0%"
	}
	if volume == "" {
		volume = "+0%"
	}
	if pitch == "" {
		pitch = "+0Hz"
	}
	if connectTimeout == 0 {
		connectTimeout = 10
	}
	if receiveTimeout == 0 {
		receiveTimeout = 60
	}
	return &EdgeTTSProvider{
		Voice:          voice,
		Rate:           rate,
		Volume:         volume,
		Pitch:          pitch,
		ConnectTimeout: connectTimeout,
		ReceiveTimeout: receiveTimeout,
	}
}

// TextToSpeech one-shot synthesis; returns Opus frames
func (p *EdgeTTSProvider) TextToSpeech(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) ([][]byte, error) {
	startTs := time.Now().UnixMilli()
	// temporary MP3 file
	tmpFile := fmt.Sprintf("/tmp/edge-tts-%d.mp3", time.Now().UnixNano())
	defer os.Remove(tmpFile)

	comm, err := communicate.NewCommunicate(
		text,
		p.Voice,
		p.Rate,
		p.Volume,
		p.Pitch,
		"", // proxy
		p.ConnectTimeout,
		p.ReceiveTimeout,
	)
	if err != nil {
		log.Errorf("EdgeTTS Communicate create failed: %v", err)
		return nil, err
	}
	// save MP3
	err = comm.Save(ctx, tmpFile, "")
	if err != nil {
		log.Errorf("EdgeTTS save MP3 failed: %v", err)
		return nil, err
	}
	// MP3 to Opus
	f, err := os.Open(tmpFile)
	if err != nil {
		return nil, fmt.Errorf("打开MP3失败: %v", err)
	}
	defer f.Close()
	pipeReader, pipeWriter := io.Pipe()
	outputChan := make(chan []byte, 1000)
	// write MP3 data to the pipe
	go func() {
		_, _ = io.Copy(pipeWriter, f)
		pipeWriter.Close()
	}()
	mp3Decoder, err := util.CreateAudioDecoder(ctx, pipeReader, outputChan, frameDuration, "mp3")
	if err != nil {
		return nil, fmt.Errorf("创建MP3解码器失败: %v", err)
	}
	var opusFrames [][]byte
	done := make(chan struct{})
	go func() {
		for frame := range outputChan {
			opusFrames = append(opusFrames, frame)
		}
		done <- struct{}{}
	}()
	if err := mp3Decoder.Run(startTs); err != nil {
		return nil, fmt.Errorf("MP3解码失败: %v", err)
	}
	<-done
	return opusFrames, nil
}

// TextToSpeechStream streaming synthesis; returns an Opus frame channel
func (p *EdgeTTSProvider) TextToSpeechStream(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) (chan []byte, error) {
	startTs := time.Now().UnixMilli()
	comm, err := communicate.NewCommunicate(
		text,
		p.Voice,
		p.Rate,
		p.Volume,
		p.Pitch,
		"", // proxy
		p.ConnectTimeout,
		p.ReceiveTimeout,
	)
	if err != nil {
		log.Errorf("EdgeTTS Communicate create failed: %v", err)
		return nil, err
	}

	chunkChan, errChan := comm.Stream(ctx)
	outputChan := make(chan []byte, 100)
	pipeReader, pipeWriter := io.Pipe()
	// MP3-to-Opus decoder
	go func() {
		defer func() {
			pipeWriter.Close()
			log.Debugf("EdgeTTS streaming synthesis finished, took %d ms", time.Now().UnixMilli()-startTs)
			if err := <-errChan; err != nil {
				log.Errorf("EdgeTTS streaming synthesis error: %v", err)
			}
		}()
		for {
			select {
			case <-ctx.Done():
				log.Debugf("EdgeTTS Stream context done, exit")
				return
			default:
				select {
				case chunk, ok := <-chunkChan:
					if !ok {
						log.Debugf("EdgeTTS Stream channel closed, exit")
						return
					}
					if chunk.Type == "audio" {
						_, _ = pipeWriter.Write(chunk.Data)
					}
				}
			}
		}

	}()
	// start MP3→Opus decoding
	go func() {
		mp3Decoder, err := util.CreateAudioDecoder(ctx, pipeReader, outputChan, frameDuration, "mp3")
		if err != nil {
			log.Errorf("EdgeTTS MP3 decoder create failed: %v", err)
			return
		}
		if err := mp3Decoder.Run(startTs); err != nil {
			log.Errorf("EdgeTTS MP3 decode failed: %v", err)
		}
		log.Debugf("EdgeTTS MP3 decode finished, took %d ms", time.Now().UnixMilli()-startTs)
	}()
	return outputChan, nil
}

// SetVoice sets voice parameters
func (p *EdgeTTSProvider) SetVoice(voiceConfig map[string]interface{}) error {
	if voice, ok := voiceConfig["voice"].(string); ok && voice != "" {
		p.Voice = voice
		return nil
	}
	return fmt.Errorf("无效的音色配置: 缺少 voice")
}

// Close closes resources (stateless provider; nothing to close)
func (p *EdgeTTSProvider) Close() error {
	return nil
}

// IsValid checks whether the resource is valid
func (p *EdgeTTSProvider) IsValid() bool {
	return p != nil
}
