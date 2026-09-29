package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"math"
	"os"

	"xiaozhi-esp32-server-golang/internal/domain/audio"
	"xiaozhi-esp32-server-golang/internal/domain/vad/webrtc_vad"
)

func genFloat32Empty(sampleRate int, durationMs int, channels int, count int) [][]float32 {
	// compute sample count
	numSamples := int(float64(sampleRate) * float64(durationMs) / 1000.0)
	// create a silence buffer
	var buf bytes.Buffer
	// 32-bit float silence value is 0.0
	for i := 0; i < numSamples*channels; i++ {
		binary.Write(&buf, binary.LittleEndian, float32(0.0))
	}
	//convert data to float32
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
	// compute sample count
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

	//convert Opus data to float32
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
	// check command-line args
	if len(os.Args) != 2 {
		log.Fatalf("usage: %s <wav_path>", os.Args[0])
	}

	wavFilePath := os.Args[1]

	// read the WAV file
	wavFile, err := os.Open(wavFilePath)
	if err != nil {
		log.Fatalf("failed to open WAV file: %v", err)
	}
	defer wavFile.Close()

	// read the entire file
	wavData, err := io.ReadAll(wavFile)
	if err != nil {
		log.Fatalf("failed to read WAV file: %v", err)
	}

	fmt.Printf("成功读取WAV文件: %s (%d 字节)\n", wavFilePath, len(wavData))

	// convert WAV to PCM via Wav2Pcm
	// use WebRTC VAD standard params: 16000Hz, mono
	sampleRate := 16000
	channels := 1

	pcmFloat32, pcmBytes, err := Wav2Pcm(wavData, sampleRate, channels)
	if err != nil {
		log.Fatalf("WAV to PCM failed: %v", err)
	}

	_ = pcmBytes

	fmt.Printf("成功转换为PCM数据，共 %d 帧（每帧20ms）\n", len(pcmFloat32))

	// create a WebRTC VAD instance
	vadImpl, err := webrtc_vad.NewWebRTCVADWithConfig(sampleRate, 2) // mode 2: medium sensitivity
	if err != nil {
		log.Fatalf("failed to create WebRTC VAD: %v", err)
	}
	defer vadImpl.Close()

	fmt.Println("WebRTC VAD创建成功，开始测试...")

	// test whether VAD works directly
	if len(pcmFloat32) == 0 {
		log.Fatalf("no PCM data to process")
	}

	// WebRTC VAD needs frames of 320 samples (20ms @ 16000Hz)
	// Wav2Pcm already frames at 20ms; each frame is exactly 320 samples
	frameSize := 320 // 20ms @ 16000Hz

	// merge all frames into continuous audio, then re-frame by frameSize
	// this ensures every frame is a full frameSize
	totalSamples := 0
	for _, frame := range pcmFloat32 {
		totalSamples += len(frame)
	}
	allPcmData := make([]float32, 0, totalSamples)
	for _, frame := range pcmFloat32 {
		allPcmData = append(allPcmData, frame...)
	}

	fmt.Printf("合并后的音频数据: %d 个样本 (%.2f 秒)\n", len(allPcmData), float64(len(allPcmData))/float64(sampleRate))

	// check audio value range (for debugging)
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
		// if values are outside [-1.0, 1.0], normalization may be needed
		if maxVal > 1.0 || minVal < -1.0 {
			fmt.Printf("警告: 音频数据超出 [-1.0, 1.0] 范围，可能需要归一化\n")
		}
	}

	fmt.Println("开始进行语音活动检测...")

	// detect using frameSize frames
	detectVoice := func(pcmData []float32) {
		speechFrames := 0
		totalFrames := 0

		// process in frameSize chunks
		for i := 0; i < len(pcmData); i += frameSize {
			end := i + frameSize
			if end > len(pcmData) {
				end = len(pcmData)
			}

			frame := pcmData[i:end]

			// if the frame is shorter than frameSize, pad with zeros
			if len(frame) < frameSize {
				// pad with zeros to frameSize
				paddedFrame := make([]float32, frameSize)
				copy(paddedFrame, frame)
				frame = paddedFrame
			}

			totalFrames++

			// run VAD detection
			isVoice, err := vadImpl.IsVADExt(frame, sampleRate, frameSize)
			if err != nil {
				log.Printf("VAD detect failed on frame %d: %v", totalFrames, err)
				// failure on the first frame means VAD was not initialized correctly
				if totalFrames == 1 {
					log.Fatalf("VAD init failed, check WebRTC VAD config and library")
				}
				continue
			}

			if isVoice {
				speechFrames++
				fmt.Printf("第%d帧: 检测到语音活动 (样本范围: %d-%d)\n", totalFrames, i, end-1)
			} else {
				fmt.Printf("第%d帧: 无语音活动 (样本范围: %d-%d)\n", totalFrames, i, end-1)
			}
		}

		// print stats
		speechPercentage := float64(speechFrames) / float64(totalFrames) * 100
		nonSpeechFrames := totalFrames - speechFrames
		fmt.Printf("\n=== WebRTC VAD检测结果统计 ===\n")
		fmt.Printf("总帧数: %d (每帧 %d 样本, %.2f ms)\n", totalFrames, frameSize, float64(frameSize)/float64(sampleRate)*1000)
		fmt.Printf("语音帧数: %d\n", speechFrames)
		fmt.Printf("非语音帧数: %d\n", nonSpeechFrames)
		fmt.Printf("语音活动比例: %.2f%%\n", speechPercentage)

		if speechFrames > 0 {
			fmt.Println("结论: 检测到语音活动")
		} else {
			fmt.Println("结论: 未检测到语音活动")
		}
	}

	// test with real WAV file data
	detectVoice(allPcmData)
}

func float32ToByte(pcmFrame []float32) []byte {
	byteData := make([]byte, len(pcmFrame)*4)
	for i, sample := range pcmFrame {
		binary.LittleEndian.PutUint32(byteData[i*4:], math.Float32bits(sample))
	}
	return byteData
}
