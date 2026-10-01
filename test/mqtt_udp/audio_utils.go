package main

import (
	"bytes"
	"fmt"
	"io"
	"os"

	"github.com/go-audio/audio"
	"github.com/go-audio/wav"
	"gopkg.in/hraban/opus.v2"
)

// WavToOpus converts WAV audio data to standard Opus format
// Returns a slice of Opus frames; each element is one Opus-encoded frame
func WavToOpus(wavData []byte, sampleRate int, channels int, bitRate int) ([][]byte, error) {
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

	// If provided params differ from file params, use the file's params
	if sampleRate == 0 {
		sampleRate = wavSampleRate
	}
	if channels == 0 {
		channels = wavChannels
	}

	//Print wavDecoder info
	fmt.Println("WAV格式:", format)

	enc, err := opus.NewEncoder(sampleRate, channels, opus.AppAudio)
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
	opusBuffer := make([]byte, 1000) // Buffer large enough for encoded data

	// Read audio buffer
	audioBuf := &audio.IntBuffer{Data: make([]int, frameSize*channels), Format: format}

	fmt.Println("开始转换...")
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

		// Copy the current frame into a new slice and append to the frame list
		frameData := make([]byte, n)
		copy(frameData, opusBuffer[:n])
		opusFrames = append(opusFrames, frameData)
	}

	return opusFrames, nil
}

func OpusToWav(opusData [][]byte, sampleRate int, channels int, fileName string) ([][]int16, error) {
	opusDecoder, err := opus.NewDecoder(sampleRate, channels)
	if err != nil {
		return nil, fmt.Errorf("创建Opus解码器失败: %v", err)
	}

	wavOut, err := os.Create(fileName)
	if err != nil {
		return nil, fmt.Errorf("创建WAV文件失败: %v", err)
	}

	pcmDataList := make([][]int16, 0)
	pcmBuffer := make([]int16, 4096)

	wavEncoder := wav.NewEncoder(wavOut, sampleRate, 16, channels, 1)
	wavBuffer := audio.IntBuffer{
		Format: &audio.Format{
			NumChannels: channels, // Use the provided channel count
			SampleRate:  sampleRate,
		},
		SourceBitDepth: 16,
		Data:           make([]int, 4096),
	}

	for _, frame := range opusData {
		n, err := opusDecoder.Decode(frame, pcmBuffer)
		if err != nil {
			return nil, fmt.Errorf("解码失败: %v", err)
		}
		copyData := make([]int16, len(pcmBuffer[:n]))
		copy(copyData, pcmBuffer[:n])
		pcmDataList = append(pcmDataList, copyData)

		//fmt.Println("pcmData len: ", len(copyData))

		// Convert PCM to int
		for i := 0; i < len(copyData); i++ {
			wavBuffer.Data = append(wavBuffer.Data, int(copyData[i]))
		}
	}

	// Write WAV file
	err = wavEncoder.Write(&wavBuffer)
	if err != nil {
		return nil, fmt.Errorf("写入WAV文件失败: %v", err)
	}

	wavEncoder.Close()

	return pcmDataList, nil
}
