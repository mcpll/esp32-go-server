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
	// check command-line args
	if len(os.Args) < 2 {
		fmt.Println("使用方法: go run main.go <音乐URL>")
		fmt.Println("示例: go run main.go https://example.com/music.mp3")
		os.Exit(1)
	}

	musicURL := os.Args[1]
	fmt.Printf("开始播放音乐: %s\n", musicURL)

	// create a cancelable context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// create music player config
	config := play_music.DefaultMusicPlayerConfig()
	config.FrameDuration = 20 // 20ms frame duration

	// create the music player
	player := play_music.NewMusicPlayer(config.ToMap())

	// print player info
	playerInfo := player.GetPlayerInfo()
	fmt.Printf("播放器信息: %+v\n", playerInfo)

	// set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// start streaming playback
	audioChan, err := player.PlayMusicStream(ctx, musicURL)
	if err != nil {
		log.Errorf("failed to start music playback: %v", err)
		return
	}

	fmt.Println("音乐播放已启动，正在流式传输音频数据...")
	fmt.Println("按 Ctrl+C 停止播放")

	// stats
	stats := &play_music.StreamingStats{
		StartTime: time.Now().UnixMilli(),
	}

	// start the stats goroutine
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

	// handle the audio stream
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
					cancel() // cancel the context and exit
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

				// frames can be sent to an audio output device or processed further
				// e.g. send to a client over WebSocket, or write to an audio file

				// print progress every 100 frames
				if frameCount%100 == 0 {
					fmt.Printf("已处理 %d 帧，总计 %d 字节\n", frameCount, totalBytes)
				}
			}
		}
	}()

	// wait for a signal or context cancel
	select {
	case sig := <-sigChan:
		fmt.Printf("\n收到信号: %v，正在停止播放...\n", sig)
		cancel()
	case <-ctx.Done():
		fmt.Println("播放完成")
	}

	// stop the player
	if err := player.Stop(); err != nil {
		log.Errorf("failed to stop player: %v", err)
	}

	fmt.Println("播放器已停止")
	fmt.Printf("最终统计: 总帧数=%d, 总字节=%d\n", stats.FramesGenerated, stats.BytesDecoded)
}

// example: write audio frames to a file (optional)
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

// example: send the audio stream over WebSocket (pseudocode)
func streamAudioViaWebSocket(frames <-chan []byte, wsURL string) {
	fmt.Printf("模拟通过WebSocket发送音频流到: %s\n", wsURL)

	for frame := range frames {
		// pseudocode only; a real implementation needs a WebSocket connection
		fmt.Printf("发送音频帧: %d 字节\n", len(frame))

		// simulate send latency
		time.Sleep(20 * time.Millisecond) // matches 20ms frame duration
	}
}
