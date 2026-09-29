package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/go-audio/audio"
	"github.com/go-audio/wav"
	"github.com/hackers365/silero-vad-go/speech"
	"gopkg.in/hraban/opus.v2"
)

// readCloserWrapper adds Close to bytes.Reader to implement ReadCloser
type readCloserWrapper struct {
	*bytes.Reader
}

// Close implements io.Closer
func (r *readCloserWrapper) Close() error {
	return nil
}

// newReadCloserWrapper creates a new ReadCloser wrapper
func newReadCloserWrapper(data []byte) *readCloserWrapper {
	return &readCloserWrapper{bytes.NewReader(data)}
}

// WavToOpus converts WAV audio to standard Opus format
// Returns a slice of Opus frames; each element is one Opus-encoded frame
func WavToOpus(wavData []byte, sampleRate int, channels int, bitRate int) ([][]byte, error) {

	sd, err := speech.NewDetector(speech.DetectorConfig{
		ModelPath:            "silero_vad.onnx",
		SampleRate:           16000,
		Threshold:            0.5,
		MinSilenceDurationMs: 250,
		SpeechPadMs:          150,
	})
	if err != nil {
		log.Fatalf("failed to create speech detector: %s", err)
	}

	// Create WAV decoder
	wavReader := bytes.NewReader(wavData)
	wavDecoder := wav.NewDecoder(wavReader)
	if !wavDecoder.IsValidFile() {
		return nil, fmt.Errorf("无效的WAV文件")
	}

	// Read WAV file info
	wavDecoder.ReadInfo()
	format := wavDecoder.Format()
	wavSampleRate := int(format.SampleRate)
	wavChannels := int(format.NumChannels)

	// If args disagree with file params, use the file params
	if sampleRate == 0 {
		sampleRate = wavSampleRate
	}
	if channels == 0 {
		channels = wavChannels
	}

	// Print wavDecoder info
	fmt.Println("WAV格式:", format)

	enc, err := opus.NewEncoder(sampleRate, channels, opus.AppAudio)
	if err != nil {
		return nil, fmt.Errorf("创建Opus编码器失败: %v", err)
	}

	dec, err := opus.NewDecoder(sampleRate, channels)
	if err != nil {
		return nil, fmt.Errorf("创建Opus编码器失败: %v", err)
	}

	// Set bitrate
	if bitRate > 0 {
		if err := enc.SetBitrate(bitRate); err != nil {
			return nil, fmt.Errorf("设置比特率失败: %v", err)
		}
	}

	// Create output frame slice
	opusFrames := make([][]byte, 0)

	perFrameDuration := 60
	// PCM buffer - Opus frame size (60ms)
	frameSize := sampleRate * perFrameDuration / 1000
	pcmBuffer := make([]int16, frameSize*channels)
	pcmBufferFloat32 := make([]float32, frameSize*channels)
	opusBuffer := make([]byte, 1000) // Buffer large enough for encoded data

	// Read audio buffer
	audioBuf := &audio.IntBuffer{Data: make([]int, frameSize*channels), Format: format}

	fmt.Println("开始转换...")

	pcmAllData := make([]float32, 0)
	for {
		// Read WAV data
		n, err := wavDecoder.PCMBuffer(audioBuf)
		if err == io.EOF || n == 0 {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("读取WAV数据失败: %v", err)
		}

		// Convert int to int16
		for i := 0; i < len(audioBuf.Data); i++ {
			if i < len(pcmBuffer) {
				pcmBuffer[i] = int16(audioBuf.Data[i])
			}
		}

		// Encode as Opus
		n, err = enc.Encode(pcmBuffer, opusBuffer)
		if err != nil {
			return nil, fmt.Errorf("编码失败: %v", err)
		}

		// Copy the current frame into a new slice and append to frames
		frameData := make([]byte, n)
		copy(frameData, opusBuffer[:n])
		opusFrames = append(opusFrames, frameData)

		// Decode Opus to PCM
		n, err = dec.DecodeFloat32(frameData, pcmBufferFloat32)
		if err != nil {
			return nil, fmt.Errorf("解码失败: %v", err)
		}

		fmt.Printf("pcmBufferFloat32 len: %d\n", len(pcmBufferFloat32[:n]))

		segments, err := sd.Detect(pcmBufferFloat32[:n])
		if err != nil {
			//log.Fatalf("Detect failed: %s", err)
		}
		fmt.Printf("detect voice: %v\n", segments)

		pcmAllData = append(pcmAllData, pcmBufferFloat32[:n]...)
	}

	segments, err := sd.Detect(pcmAllData)
	if err != nil {
		log.Fatalf("Detect failed: %s", err)
	}
	fmt.Printf("detect voice: %v\n", segments)

	// Write frameData to test.opus
	opusFile, err := os.OpenFile("output.opus", os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatalf("failed to create opus file: %s", err)
	}
	opusFile.Write(opusFrames[0])
	opusFile.Close()

	/*
		// Write PCM data to test.pcm
		pcmFile, err := os.OpenFile("test.pcm", os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			log.Fatalf("failed to create pcm file: %s", err)
		}

		defer pcmFile.Close()
		dec, err := opus.NewDecoder(sampleRate, channels)
		if err != nil {
			return nil, fmt.Errorf("failed to create Opus decoder: %v", err)
		}

		pcmBuffer = make([]int16, 10240)
		for _, data := range opusFrames {
			// Decode Opus data to PCM
			n, err := dec.Decode(data, pcmBuffer)
			if err != nil {
				return nil, fmt.Errorf("decode failed: %v", err)
			}
			frameData := make([]int16, len(pcmBuffer)*2)
			copy(frameData, pcmBuffer[:n])
			_, err = pcmFile.Write(frameData)
			if err != nil {
				log.Fatalf("failed to write to pcm file: %s", err)
			}
		}*/

	return opusFrames, nil
}

func main() {
	if len(os.Args) != 2 {
		log.Fatalf("invalid arguments provided: expecting one file path")
	}

	f, err := os.Open(os.Args[1])
	if err != nil {
		log.Fatalf("failed to open sample audio file: %s", err)
	}
	defer f.Close()

	// Read entire file contents
	mp3Data, err := io.ReadAll(f)
	if err != nil {
		log.Fatalf("failed to read mp3 file: %s", err)
	}

	// Convert MP3 to Opus
	opusData, err := WavToOpus(mp3Data, 16000, 1, 0)
	if err != nil {
		log.Fatalf("failed to convert mp3 to opus: %s", err)
	}

	// Print Opus data
	fmt.Printf("opusData: %d\n", len(opusData))

	// Decode Opus data to PCM

	// Write all data to test.opus
	/*opusFile, err := os.OpenFile("test.opus", os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatalf("failed to create opus file: %s", err)
	}
	defer opusFile.Close()

	for _, data := range opusData {
		_, err := opusFile.Write(data)
		if err != nil {
			log.Fatalf("failed to write to opus file: %s", err)
		}
	}*/
}
