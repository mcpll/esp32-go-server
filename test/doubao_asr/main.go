// 测试 domain/asr/doubao：建立连接后等待12秒，发送10个数据包，再等待3秒，获取结果。
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"xiaozhi-esp32-server-golang/internal/domain/asr/doubao"
	log "xiaozhi-esp32-server-golang/logger"
)

const (
	sampleRate  = 16000
	chunkMs     = 200
	packetCount = 10
)

func main() {
	log.UseStdout()

	appID := flag.String("appid", os.Getenv("DOUBAO_ASR_APPID"), "豆包 ASR AppID")
	token := flag.String("token", os.Getenv("DOUBAO_ASR_ACCESS_TOKEN"), "豆包 ASR AccessToken")
	flag.Parse()

	if *appID == "" || *token == "" {
		fmt.Fprintln(os.Stderr, "请设置 -appid 和 -token，或环境变量 DOUBAO_ASR_APPID、DOUBAO_ASR_ACCESS_TOKEN")
		os.Exit(1)
	}

	cfg := doubao.DoubaoV2Config{
		AppID:       *appID,
		AccessToken: *token,
	}
	asr, err := doubao.NewDoubaoV2ASR(cfg)
	if err != nil {
		log.Errorf("创建豆包 ASR 失败: %v", err)
		os.Exit(1)
	}
	defer asr.Close()

	// 200ms 音频块：每块 3200 样本
	chunkSamples := sampleRate * chunkMs / 1000

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	audioStream := make(chan []float32, 4)

	// 建立连接
	log.Info("建立连接...")
	resultChan, err := asr.StreamingRecognize(ctx, audioStream)
	if err != nil {
		log.Errorf("StreamingRecognize 失败: %v", err)
		return
	}

	// 等待12秒
	log.Info("等待12秒...")

	// 发送10个数据包
	log.Infof("发送 %d 个数据包...", packetCount)
	go func() {
		defer close(audioStream)
		for j := 0; j < packetCount; j++ {
			chunk := make([]float32, chunkSamples)
			// 静音，范围 [-1, 1] 的 0
			select {
			case audioStream <- chunk:
				log.Infof("已发送第 %d/%d 个数据包", j+1, packetCount)
			case <-ctx.Done():
				return
			}
		}
		log.Info("所有数据包发送完成")
	}()

	// 等待3秒
	log.Info("等待3秒...")
	time.Sleep(3 * time.Second)

	// 获取结果
	log.Info("获取结果...")
	for r := range resultChan {
		if r.Error != nil {
			log.Errorf("识别错误: %v", r.Error)
			break
		}
		if r.Text != "" {
			log.Infof("识别结果: %s (IsFinal=%v)", r.Text, r.IsFinal)
		}
	}

	log.Info("测试结束")
}
