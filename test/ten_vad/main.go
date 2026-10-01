package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"path/filepath"
	"strings"

	"xiaozhi-esp32-server-golang/internal/domain/audio"
	"xiaozhi-esp32-server-golang/internal/domain/vad/ten_vad"

	goaudio "github.com/go-audio/audio"
	"github.com/go-audio/wav"
)

func genFloat32Empty(sampleRate int, durationMs int, channels int, count int) [][]float32 {
	// Compute sample count
	numSamples := int(float64(sampleRate) * float64(durationMs) / 1000.0)
	// Create silence buffer
	var buf bytes.Buffer
	// 32-bit float silence value is 0.0
	for i := 0; i < numSamples*channels; i++ {
		binary.Write(&buf, binary.LittleEndian, float32(0.0))
	}
	// Convert data to float32
	float32Data := make([]float32, numSamples*channels)
	for i := 0; i < numSamples*channels; i++ {
		float32Data[i] = float32(buf.Bytes()[i])
	}
	result := make([][]float32, 0)
	for i := 0; i < count; i++ {
		result = append(result, float32Data)
	}
	return result
}

func genOpusFloat32Empty(sampleRate int, durationMs int, channels int, count int) [][]float32 {
	// Compute sample count
	numSamples := int(float64(sampleRate) * float64(durationMs) / 1000.0)

	audioProcesser, err := audio.GetAudioProcesser(sampleRate, channels, 20)
	if err != nil {
		fmt.Printf("获取解码器失败: %v", err)
		return nil
	}

	pcmFrame := make([]int16, numSamples)

	opusFrame := make([]byte, 1000)
	n, err := audioProcesser.Encoder(pcmFrame, opusFrame)
	if err != nil {
		fmt.Printf("解码失败: %v", err)
		return nil
	}

	// Convert Opus data to float32
	pcmFloat32 := make([]float32, n)
	for i := 0; i < n; i++ {
		pcmFloat32[i] = float32(opusFrame[i])
	}

	result := make([][]float32, 0)
	for i := 0; i < count; i++ {
		tmp := make([]float32, n)
		copy(tmp, pcmFloat32)
		result = append(result, tmp)
	}
	return result
}

func main() {
	// Check command-line args
	if len(os.Args) < 2 {
		log.Fatalf("usage: %s <wav_path> [hop_size] [threshold]\nexample: %s test.wav 512 0.3", os.Args[0], os.Args[0])
	}

	wavFilePath := os.Args[1]

	// Parse optional args
	hopSize := 512
	threshold := 0.3
	if len(os.Args) >= 3 {
		_, err := fmt.Sscanf(os.Args[2], "%d", &hopSize)
		if err != nil {
			log.Printf("invalid hop_size, using default 512")
			hopSize = 512
		}
	}
	if len(os.Args) >= 4 {
		_, err := fmt.Sscanf(os.Args[3], "%f", &threshold)
		if err != nil {
			log.Printf("invalid threshold, using default 0.3")
			threshold = 0.3
		}
	}

	// Read WAV file
	wavFile, err := os.Open(wavFilePath)
	if err != nil {
		log.Fatalf("failed to open WAV file: %v", err)
	}
	defer wavFile.Close()

	// Read entire file contents
	wavData, err := io.ReadAll(wavFile)
	if err != nil {
		log.Fatalf("failed to read WAV file: %v", err)
	}

	fmt.Printf("成功读取WAV文件: %s (%d 字节)\n", wavFilePath, len(wavData))

	// Call Wav2Pcm to convert WAV to PCM
	// Use TEN-VAD standard params: 16000Hz sample rate, mono
	sampleRate := 16000
	channels := 1

	pcmFloat32, pcmBytes, err := Wav2Pcm(wavData, sampleRate, channels)
	if err != nil {
		log.Fatalf("WAV to PCM failed: %v", err)
	}

	_ = pcmBytes

	fmt.Printf("成功转换为PCM数据，共 %d 帧（每帧20ms）\n", len(pcmFloat32))

	// Create TEN-VAD instance
	config := map[string]interface{}{
		"hop_size":  hopSize,
		"threshold": threshold,
	}
	vadImpl, err := ten_vad.NewTenVAD(config)
	if err != nil {
		log.Fatalf("failed to create TEN-VAD: %v", err)
	}
	defer vadImpl.Close()

	fmt.Printf("TEN-VAD创建成功 (hop_size=%d, threshold=%.2f)，开始测试...\n", hopSize, threshold)

	// Directly test whether VAD works
	if len(pcmFloat32) == 0 {
		log.Fatalf("no PCM data to process")
	}

	// Merge all frames into continuous audio
	// TEN-VAD frames by hopSize, not by 20ms
	totalSamples := 0
	for _, frame := range pcmFloat32 {
		totalSamples += len(frame)
	}
	allPcmData := make([]float32, 0, totalSamples)
	for _, frame := range pcmFloat32 {
		allPcmData = append(allPcmData, frame...)
	}

	fmt.Printf("合并后的音频数据: %d 个样本 (%.2f 秒)\n", len(allPcmData), float64(len(allPcmData))/float64(sampleRate))

	// Check audio data range (for debugging)
	if len(allPcmData) > 0 {
		minVal := allPcmData[0]
		maxVal := allPcmData[0]
		for _, v := range allPcmData {
			if v < minVal {
				minVal = v
			}
			if v > maxVal {
				maxVal = v
			}
		}
		fmt.Printf("音频数据范围: [%.6f, %.6f]\n", minVal, maxVal)
		// If data is outside [-1.0, 1.0], normalization may be needed
		if maxVal > 1.0 || minVal < -1.0 {
			fmt.Printf("警告: 音频数据超出 [-1.0, 1.0] 范围，可能需要归一化\n")
		}
	}

	fmt.Println("开始进行语音活动检测...")

	// Detect in hopSize frames
	detectVoice := func(pcmData []float32) {
		speechFrames := 0
		totalFrames := 0
		var speechFramesData []float32 // Collect all speech frames

		// Process in hopSize frames
		for i := 0; i < len(pcmData); i += hopSize {
			end := i + hopSize
			if end > len(pcmData) {
				end = len(pcmData)
			}

			frame := pcmData[i:end]

			// If frame is shorter than hopSize, zero-pad or skip
			if len(frame) < hopSize {
				// Zero-pad to hopSize
				paddedFrame := make([]float32, hopSize)
				copy(paddedFrame, frame)
				frame = paddedFrame
			}

			totalFrames++

			// Run VAD detection
			isVoice, err := vadImpl.IsVADExt(frame, sampleRate, hopSize)
			if err != nil {
				log.Printf("VAD detect failed on frame %d: %v", totalFrames, err)
				// Failure on the first frame means VAD was not initialized correctly
				if totalFrames == 1 {
					log.Fatalf("VAD init failed, check TEN-VAD config and library")
				}
				continue
			}

			if isVoice {
				speechFrames++
				// Collect speech frame data (original frames, no padding)
				originalFrame := pcmData[i:end]
				speechFramesData = append(speechFramesData, originalFrame...)
				fmt.Printf("第%d帧: 检测到语音活动 (样本范围: %d-%d)\n", totalFrames, i, end-1)
			} else {
				fmt.Printf("第%d帧: 无语音活动 (样本范围: %d-%d)\n", totalFrames, i, end-1)
			}
		}

		// Print stats
		speechPercentage := float64(speechFrames) / float64(totalFrames) * 100
		nonSpeechFrames := totalFrames - speechFrames
		fmt.Printf("\n=== TEN-VAD检测结果统计 ===\n")
		fmt.Printf("总帧数: %d (每帧 %d 样本, %.2f ms)\n", totalFrames, hopSize, float64(hopSize)/float64(sampleRate)*1000)
		fmt.Printf("语音帧数: %d\n", speechFrames)
		fmt.Printf("非语音帧数: %d\n", nonSpeechFrames)
		fmt.Printf("语音活动比例: %.2f%%\n", speechPercentage)

		if speechFrames > 0 {
			fmt.Println("结论: 检测到语音活动")

			// Save speech frames to a WAV file
			outputFileName := generateOutputFileName(wavFilePath)
			err := saveFloat32ToWav(speechFramesData, outputFileName, sampleRate, channels)
			if err != nil {
				log.Printf("failed to save voiced frames to WAV: %v", err)
			} else {
				fmt.Printf("成功将有声音的帧保存到: %s (共 %d 个样本, %.2f 秒)\n",
					outputFileName, len(speechFramesData), float64(len(speechFramesData))/float64(sampleRate))
			}
		} else {
			fmt.Println("结论: 未检测到语音活动")
		}
	}

	// Test with the merged full audio
	detectVoice(allPcmData)
}

func float32ToByte(pcmFrame []float32) []byte {
	byteData := make([]byte, len(pcmFrame)*4)
	for i, sample := range pcmFrame {
		binary.LittleEndian.PutUint32(byteData[i*4:], math.Float32bits(sample))
	}
	return byteData
}

// generateOutputFileName builds the output name by adding a "_speech" suffix
func generateOutputFileName(inputPath string) string {
	dir := filepath.Dir(inputPath)
	baseName := filepath.Base(inputPath)
	ext := filepath.Ext(baseName)
	nameWithoutExt := strings.TrimSuffix(baseName, ext)
	outputName := nameWithoutExt + "_speech" + ext
	return filepath.Join(dir, outputName)
}

// saveFloat32ToWav saves float32 PCM data as a WAV file
func saveFloat32ToWav(pcmData []float32, fileName string, sampleRate int, channels int) error {
	// Create output file
	wavFile, err := os.Create(fileName)
	if err != nil {
		return fmt.Errorf("创建WAV文件失败: %v", err)
	}
	defer wavFile.Close()

	// Create WAV encoder
	wavEncoder := wav.NewEncoder(wavFile, sampleRate, 16, channels, 1)

	// Convert float32 to int16
	// float32 is usually [-1.0, 1.0]; scale to int16 [-32768, 32767]
	intData := make([]int, len(pcmData))
	for i, sample := range pcmData {
		// Clamp to [-1.0, 1.0]
		if sample > 1.0 {
			sample = 1.0
		}
		if sample < -1.0 {
			sample = -1.0
		}
		// Convert to int16 range
		intSample := int(sample * 32767.0)
		if intSample > 32767 {
			intSample = 32767
		}
		if intSample < -32768 {
			intSample = -32768
		}
		intData[i] = intSample
	}

	// Create audio buffer
	audioBuf := &goaudio.IntBuffer{
		Format: &goaudio.Format{
			NumChannels: channels,
			SampleRate:  sampleRate,
		},
		SourceBitDepth: 16,
		Data:           intData,
	}

	// Write WAV file
	err = wavEncoder.Write(audioBuf)
	if err != nil {
		return fmt.Errorf("写入WAV文件失败: %v", err)
	}

	// Close encoder
	err = wavEncoder.Close()
	if err != nil {
		return fmt.Errorf("关闭WAV编码器失败: %v", err)
	}

	return nil
}
