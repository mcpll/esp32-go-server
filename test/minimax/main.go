package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"time"

	"xiaozhi-esp32-server-golang/internal/domain/tts/minimax"
)

func main() {
	// 解析命令行参数
	text := flag.String("text", "真正的危险不是计算机开始像人一样思考，而是人开始像计算机一样思考。计算机只是可以帮我们处理一些简单事务。", "要合成的文本")
	outputFile := flag.String("output", "output.mp3", "输出音频文件名")
	apiKey := flag.String("api_key", "", "Minimax API Key（如果未提供，将从环境变量 MINIMAX_API_KEY 读取）")
	model := flag.String("model", "speech-2.8-hd", "模型名称")
	voiceID := flag.String("voice", "male-qn-qingse", "音色ID")
	format := flag.String("format", "mp3", "音频格式 (mp3/wav/pcm)")
	flag.Parse()

	// 获取 API Key
	if *apiKey == "" {
		*apiKey = os.Getenv("MINIMAX_API_KEY")
		if *apiKey == "" {
			fmt.Fprintf(os.Stderr, "错误: 请提供 API Key（通过 -api_key 参数或 MINIMAX_API_KEY 环境变量）\n")
			os.Exit(1)
		}
	}

	// 创建配置
	config := map[string]interface{}{
		"api_key":     *apiKey,
		"model":       *model,
		"voice_id":    *voiceID,
		"speed":       1.0,
		"vol":         1.0,
		"pitch":       0,
		"sample_rate": 32000,
		"bitrate":     128000,
		"format":      *format,
		"channel":     1,
	}

	// 创建 Minimax TTS Provider
	provider := minimax.NewMinimaxTTSProvider(config)

	// 创建上下文，支持取消操作
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 设置信号处理，支持 Ctrl+C 中断
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)
	go func() {
		<-interrupt
		fmt.Println("\n收到中断信号，正在取消...")
		cancel()
	}()

	fmt.Printf("开始合成文本: %s\n", *text)
	fmt.Printf("使用配置: 模型=%s, 音色=%s, 格式=%s\n", *model, *voiceID, *format)
	fmt.Println("正在连接并合成音频...")

	// 调用流式 TTS
	startTime := time.Now()
	outputChan, err := provider.TextToSpeechStream(ctx, *text, 16000, 1, 60)
	if err != nil {
		fmt.Fprintf(os.Stderr, "TTS 合成失败: %v\n", err)
		os.Exit(1)
	}

	// 收集音频数据
	var audioFrames [][]byte
	chunkCount := 0

	fmt.Println("开始接收音频数据...")
	for frame := range outputChan {
		chunkCount++
		audioFrames = append(audioFrames, frame)
		if chunkCount%10 == 0 {
			fmt.Printf("已接收 %d 个音频帧...\n", chunkCount)
		}
	}

	elapsed := time.Since(startTime)
	fmt.Printf("音频合成完成！共接收 %d 个音频帧，耗时: %v\n", chunkCount, elapsed)

	// 合并所有音频帧
	if len(audioFrames) == 0 {
		fmt.Fprintf(os.Stderr, "错误: 未接收到任何音频数据\n")
		os.Exit(1)
	}

	// 计算总大小
	totalSize := 0
	for _, frame := range audioFrames {
		totalSize += len(frame)
	}

	// 合并音频数据
	audioData := make([]byte, 0, totalSize)
	for _, frame := range audioFrames {
		audioData = append(audioData, frame...)
	}

	fmt.Printf("音频数据总大小: %d 字节 (%.2f KB)\n", totalSize, float64(totalSize)/1024)

	// 保存到文件
	// 注意：系统内部使用 Opus 编码，所以接收到的音频帧是 Opus 格式
	// 如果需要其他格式（如 WAV/MP3），需要额外的解码和编码步骤
	// 这里我们直接保存 Opus 帧，可以使用支持 Opus 的播放器播放（如 VLC、ffplay 等）

	fmt.Printf("正在保存音频到文件: %s\n", *outputFile)
	fmt.Println("注意: 保存的是 Opus 编码的音频帧，可以使用支持 Opus 的播放器播放")
	if err := os.WriteFile(*outputFile, audioData, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "保存文件失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("音频已成功保存到: %s\n", *outputFile)
	fmt.Printf("文件大小: %d 字节 (%.2f KB)\n", len(audioData), float64(len(audioData))/1024)

	// 清理资源
	if err := provider.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "关闭 Provider 失败: %v\n", err)
	}

	fmt.Println("测试完成！")
}
