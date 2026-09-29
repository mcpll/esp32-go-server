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
	// 计算样本数
	numSamples := int(float64(sampleRate) * float64(durationMs) / 1000.0)
	// 创建静音缓冲区
	var buf bytes.Buffer
	// 32位浮点静音值为0.0
	for i := 0; i < numSamples*channels; i++ {
		binary.Write(&buf, binary.LittleEndian, float32(0.0))
	}
	//将数据转换为float32
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
	// 计算样本数
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

	//将opus数据转换为float32
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
	// 检查命令行参数
	if len(os.Args) != 2 {
		log.Fatalf("用法: %s <wav文件路径>", os.Args[0])
	}

	wavFilePath := os.Args[1]

	// 读取WAV文件
	wavFile, err := os.Open(wavFilePath)
	if err != nil {
		log.Fatalf("无法打开WAV文件: %v", err)
	}
	defer wavFile.Close()

	// 读取整个文件内容
	wavData, err := io.ReadAll(wavFile)
	if err != nil {
		log.Fatalf("无法读取WAV文件: %v", err)
	}

	fmt.Printf("成功读取WAV文件: %s (%d 字节)\n", wavFilePath, len(wavData))

	// 调用 Wav2Pcm 函数转换WAV数据为PCM数据
	// 使用WebRTC VAD支持的标准参数：16000Hz采样率，单声道
	sampleRate := 16000
	channels := 1

	pcmFloat32, pcmBytes, err := Wav2Pcm(wavData, sampleRate, channels)
	if err != nil {
		log.Fatalf("WAV转PCM失败: %v", err)
	}

	_ = pcmBytes

	fmt.Printf("成功转换为PCM数据，共 %d 帧（每帧20ms）\n", len(pcmFloat32))

	// 创建WebRTC VAD实例
	vadImpl, err := webrtc_vad.NewWebRTCVADWithConfig(sampleRate, 2) // 模式2：中等敏感度
	if err != nil {
		log.Fatalf("创建WebRTC VAD失败: %v", err)
	}
	defer vadImpl.Close()

	fmt.Println("WebRTC VAD创建成功，开始测试...")

	// 直接测试VAD是否能正常工作
	if len(pcmFloat32) == 0 {
		log.Fatalf("没有PCM数据可供处理")
	}

	// WebRTC VAD 需要 320 样本（20ms @ 16000Hz）的帧
	// Wav2Pcm 已经按 20ms 分帧，每帧正好是 320 样本
	frameSize := 320 // 20ms @ 16000Hz

	// 将所有帧合并成连续的音频数据，然后按 frameSize 重新分帧
	// 这样可以确保每帧都是完整的 frameSize
	totalSamples := 0
	for _, frame := range pcmFloat32 {
		totalSamples += len(frame)
	}
	allPcmData := make([]float32, 0, totalSamples)
	for _, frame := range pcmFloat32 {
		allPcmData = append(allPcmData, frame...)
	}

	fmt.Printf("合并后的音频数据: %d 个样本 (%.2f 秒)\n", len(allPcmData), float64(len(allPcmData))/float64(sampleRate))

	// 检查音频数据范围（用于调试）
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
		// 如果数据不在 [-1.0, 1.0] 范围内，可能需要归一化
		if maxVal > 1.0 || minVal < -1.0 {
			fmt.Printf("警告: 音频数据超出 [-1.0, 1.0] 范围，可能需要归一化\n")
		}
	}

	fmt.Println("开始进行语音活动检测...")

	// 按 frameSize 分帧进行检测
	detectVoice := func(pcmData []float32) {
		speechFrames := 0
		totalFrames := 0

		// 按 frameSize 分帧处理
		for i := 0; i < len(pcmData); i += frameSize {
			end := i + frameSize
			if end > len(pcmData) {
				end = len(pcmData)
			}

			frame := pcmData[i:end]

			// 如果帧长度不足 frameSize，填充零
			if len(frame) < frameSize {
				// 填充零到 frameSize 长度
				paddedFrame := make([]float32, frameSize)
				copy(paddedFrame, frame)
				frame = paddedFrame
			}

			totalFrames++

			// 进行VAD检测
			isVoice, err := vadImpl.IsVADExt(frame, sampleRate, frameSize)
			if err != nil {
				log.Printf("第%d帧VAD检测失败: %v", totalFrames, err)
				// 如果是第一帧就失败，说明VAD未正确初始化
				if totalFrames == 1 {
					log.Fatalf("VAD初始化失败，请检查WebRTC VAD配置和库文件")
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

		// 输出统计结果
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

	// 使用实际的WAV文件数据进行测试
	detectVoice(allPcmData)
}

func float32ToByte(pcmFrame []float32) []byte {
	byteData := make([]byte, len(pcmFrame)*4)
	for i, sample := range pcmFrame {
		binary.LittleEndian.PutUint32(byteData[i*4:], math.Float32bits(sample))
	}
	return byteData
}
