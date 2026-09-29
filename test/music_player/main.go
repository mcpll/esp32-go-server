package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"xiaozhi-esp32-server-golang/internal/domain/play_music"
	log "xiaozhi-esp32-server-golang/logger"
)

func main() {
	// 检查命令行参数
	if len(os.Args) < 2 {
		fmt.Println("使用方法: go run main.go <音乐URL>")
		fmt.Println("示例: go run main.go https://example.com/music.mp3")
		os.Exit(1)
	}

	musicURL := os.Args[1]
	fmt.Printf("开始播放音乐: %s\n", musicURL)

	// 创建上下文，支持取消操作
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 创建音乐播放器配置
	config := play_music.DefaultMusicPlayerConfig()
	config.FrameDuration = 20 // 20ms 帧时长

	// 创建音乐播放器
	player := play_music.NewMusicPlayer(config.ToMap())

	// 显示播放器信息
	playerInfo := player.GetPlayerInfo()
	fmt.Printf("播放器信息: %+v\n", playerInfo)

	// 设置信号处理，支持优雅退出
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// 开始流式播放
	audioChan, err := player.PlayMusicStream(ctx, musicURL)
	if err != nil {
		log.Errorf("启动音乐播放失败: %v", err)
		return
	}

	fmt.Println("音乐播放已启动，正在流式传输音频数据...")
	fmt.Println("按 Ctrl+C 停止播放")

	// 统计信息
	stats := &play_music.StreamingStats{
		StartTime: time.Now().UnixMilli(),
	}

	// 启动统计goroutine
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				fmt.Printf("\n=== 播放统计 ===\n")
				fmt.Printf("已生成帧数: %d\n", stats.FramesGenerated)
				fmt.Printf("已解码字节: %d\n", stats.BytesDecoded)
				fmt.Printf("运行时间: %d 秒\n", (time.Now().UnixMilli()-stats.StartTime)/1000)
				fmt.Printf("首帧时间: %d ms\n", stats.FirstFrameTime-stats.StartTime)
				fmt.Printf("===============\n")
			}
		}
	}()

	// 处理音频流数据
	go func() {
		frameCount := 0
		totalBytes := 0
		firstFrame := true

		for {
			select {
			case <-ctx.Done():
				fmt.Println("停止处理音频流")
				return

			case audioFrame, ok := <-audioChan:
				if !ok {
					fmt.Println("音频流已结束")
					cancel() // 取消上下文，结束程序
					return
				}

				if firstFrame {
					firstFrame = false
					stats.FirstFrameTime = time.Now().UnixMilli()
					fmt.Printf("收到首个音频帧，大小: %d 字节\n", len(audioFrame))
				}

				frameCount++
				totalBytes += len(audioFrame)
				stats.FramesGenerated = int64(frameCount)
				stats.BytesDecoded = int64(totalBytes)

				// 这里可以将音频帧发送到音频输出设备或进行其他处理
				// 例如：通过WebSocket发送给客户端、写入音频文件等

				// 每100帧显示一次进度
				if frameCount%100 == 0 {
					fmt.Printf("已处理 %d 帧，总计 %d 字节\n", frameCount, totalBytes)
				}
			}
		}
	}()

	// 等待信号或上下文取消
	select {
	case sig := <-sigChan:
		fmt.Printf("\n收到信号: %v，正在停止播放...\n", sig)
		cancel()
	case <-ctx.Done():
		fmt.Println("播放完成")
	}

	// 停止播放器
	if err := player.Stop(); err != nil {
		log.Errorf("停止播放器失败: %v", err)
	}

	fmt.Println("播放器已停止")
	fmt.Printf("最终统计: 总帧数=%d, 总字节=%d\n", stats.FramesGenerated, stats.BytesDecoded)
}

// 示例：将音频帧写入文件（可选）
func saveAudioFramesToFile(frames <-chan []byte, filename string) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	frameCount := 0
	for frame := range frames {
		_, err := file.Write(frame)
		if err != nil {
			return err
		}
		frameCount++
	}

	fmt.Printf("已将 %d 个音频帧写入文件: %s\n", frameCount, filename)
	return nil
}

// 示例：通过WebSocket发送音频流（伪代码）
func streamAudioViaWebSocket(frames <-chan []byte, wsURL string) {
	fmt.Printf("模拟通过WebSocket发送音频流到: %s\n", wsURL)

	for frame := range frames {
		// 这里是伪代码，实际实现需要建立WebSocket连接
		fmt.Printf("发送音频帧: %d 字节\n", len(frame))

		// 模拟发送延迟
		time.Sleep(20 * time.Millisecond) // 对应20ms帧时长
	}
}
