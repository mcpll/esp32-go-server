// Test domain/asr/doubao: after connecting wait 12s, send 10 packets, wait 3s more, then get results.
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
		log.Errorf("failed to create Doubao ASR: %v", err)
		os.Exit(1)
	}
	defer asr.Close()

	// 200ms audio chunks: 3200 samples each
	chunkSamples := sampleRate * chunkMs / 1000

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	audioStream := make(chan []float32, 4)

	// Establish connection
	log.Info("establishing connection...")
	resultChan, err := asr.StreamingRecognize(ctx, audioStream)
	if err != nil {
		log.Errorf("StreamingRecognize failed: %v", err)
		return
	}

	// Wait 12 seconds
	log.Info("waiting 12 seconds...")

	// Send 10 packets
	log.Infof("sending %d packets...", packetCount)
	go func() {
		defer close(audioStream)
		for j := 0; j < packetCount; j++ {
			chunk := make([]float32, chunkSamples)
			// Silence: zeros in range [-1, 1]
			select {
			case audioStream <- chunk:
				log.Infof("sent packet %d/%d", j+1, packetCount)
			case <-ctx.Done():
				return
			}
		}
		log.Info("all packets sent")
	}()

	// Wait 3 seconds
	log.Info("waiting 3 seconds...")
	time.Sleep(3 * time.Second)

	// Get results
	log.Info("getting results...")
	for r := range resultChan {
		if r.Error != nil {
			log.Errorf("recognition error: %v", r.Error)
			break
		}
		if r.Text != "" {
			log.Infof("recognition result: %s (IsFinal=%v)", r.Text, r.IsFinal)
		}
	}

	log.Info("test finished")
}
