package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/gopxl/beep/mp3"
	"github.com/gopxl/beep/wav"
	"gopkg.in/hraban/opus.v2"
)

func main1() {
	// HTTP API URL
	mp3URL := "http://home.hackers365.com:55555/apk/test.mp3"
	// Output PCM file path
	pcmFilePath := "output.pcm"

	// Create PCM file
	pcmFile, err := os.Create(pcmFilePath)
	if err != nil {
		fmt.Printf("无法创建PCM文件: %v\n", err)
		return
	}
	defer pcmFile.Close()

	// Fetch and process MP3 from HTTP API
	err = processMP3FromHTTP(mp3URL, pcmFile)
	if err != nil {
		fmt.Printf("处理HTTP MP3数据失败: %v\n", err)
		return
	}

	fmt.Printf("HTTP MP3数据已成功解码为PCM格式，保存至: %s\n", pcmFilePath)

	// Export WAV
	exportHTTPToWav(mp3URL, "output.wav")
}

type readCloserWrapper struct {
	io.Reader
}

func (r readCloserWrapper) Close() error {
	return nil
}

// Fetch and process MP3 from HTTP API
func processMP3FromHTTP(url string, pcmFile *os.File) error {
	// Send HTTP request
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("HTTP请求失败: %v", err)
	}
	defer resp.Body.Close()

	// Check HTTP response status
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP请求返回非200状态码: %d", resp.StatusCode)
	}

	// Create a pipe for the data stream
	pipeReader, pipeWriter := io.Pipe()
	defer pipeReader.Close()

	// Create read and sample buffers
	bufferSize := 10 * 1024            // 10KB
	buffer := make([]byte, bufferSize) // HTTP read buffer

	opusBuffer := make([]byte, 1000) // Opus encode output buffer

	// Create error and done channels
	errChan := make(chan error, 1)
	doneChan := make(chan struct{}, 1)

	// Goroutine to decode MP3 and process PCM
	go func() {
		// Try initializing the decoder
		streamer, format, err := mp3.Decode(pipeReader)
		if err != nil {
			errChan <- fmt.Errorf("MP3解码器初始化失败: %v", err)
			return
		}
		defer streamer.Close()

		fmt.Printf("MP3解码器初始化成功，采样率: %d Hz, 声道数: %d\n",
			format.SampleRate, format.NumChannels)

		// Original MP3 format info
		sampleRate := int(format.SampleRate)
		channels := int(format.NumChannels)

		// PCM buffer and Opus frame size (e.g. 60ms)
		perFrameDuration := 60 // milliseconds
		frameSize := sampleRate * perFrameDuration / 1000
		pcmBuffer := make([]int16, frameSize*channels)
		opusFrames := make([][]byte, 0) // Store encoded Opus frames

		enc, err := opus.NewEncoder(sampleRate, channels, opus.AppAudio)
		if err != nil {
			fmt.Printf("创建Opus编码器失败: %v\n", err)
			errChan <- fmt.Errorf("创建Opus编码器失败: %v", err)
			return
		}

		beepSampleBuf := make([][2]float64, 1024) // Beep decode buffer
		// Process decoded audio stream
		currentFramePos := 0 // Current fill position in pcmBuffer
		for {
			// Read samples from stream into sampleBuf
			numSamplesRead, ok := streamer.Stream(beepSampleBuf)
			if !ok {
				// Handle leftover data shorter than one frame
				if currentFramePos > 0 {
					// Full frame buffer; pad remainder with zeros
					paddedFrame := make([]int16, len(pcmBuffer))
					copy(paddedFrame, pcmBuffer[:currentFramePos]) // Copy valid data to the start; rest stays zero

					// Encode the padded full frame
					n, err := enc.Encode(paddedFrame, opusBuffer)
					if err != nil {
						fmt.Printf("编码剩余数据失败: %v\n", err)
						// May need to send error on errChan
					} else {
						frameData := make([]byte, n)
						copy(frameData, opusBuffer[:n])
						opusFrames = append(opusFrames, frameData)
						// Note: encodes a full frame even if original data was short
						fmt.Printf("已编码最后补齐的 %d 个PCM样本 (原始 %d)\n", len(paddedFrame), currentFramePos)
					}
				}
				// Decode finished
				doneChan <- struct{}{}
				return
			}

			// Convert float64 samples to int16 into pcmBuffer
			for i := 0; i < numSamplesRead; i++ {
				// Convert directly
				leftSample := int16(beepSampleBuf[i][0] * 32767.0)
				rightSample := int16(beepSampleBuf[i][1] * 32767.0)

				// Write PCM data
				pcmBuffer[currentFramePos] = leftSample
				if channels > 1 {
					pcmBuffer[currentFramePos+1] = rightSample
				}
				currentFramePos += channels

				// Encode when pcmBuffer holds a full frame
				if currentFramePos == len(pcmBuffer) {
					n, err := enc.Encode(pcmBuffer, opusBuffer)
					if err != nil {
						fmt.Printf("编码失败: %v\n", err)
						errChan <- fmt.Errorf("编码失败: %v", err)
						return
					}

					// Copy current frame into a new slice and append
					frameData := make([]byte, n)
					copy(frameData, opusBuffer[:n])
					opusFrames = append(opusFrames, frameData)

					fmt.Printf("已编码一帧 (%d PCM样本)\n", len(pcmBuffer))
					currentFramePos = 0 // Reset frame position
				}
			}
		}
	}()

	// Ticker every 100ms to send data
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	// Loop reading HTTP data into the pipe
	for {
		select {
		case <-ticker.C:
			// Read from HTTP response
			n, err := resp.Body.Read(buffer)

			// If data read, write to pipe
			if n > 0 {
				_, writeErr := pipeWriter.Write(buffer[:n])
				if writeErr != nil {
					return fmt.Errorf("写入pipe失败: %v", writeErr)
				}
				fmt.Printf("已读取并写入 %d 字节MP3数据\n", n)
			}

			// Handle EOF or error
			if err != nil {
				if err == io.EOF {
					fmt.Println("HTTP数据流已读取完毕")
					pipeWriter.Close() // Close pipe writer

					// Wait for decode done or error
					select {
					case <-doneChan:
						return nil
					case err := <-errChan:
						return err
					}
				} else {
					return fmt.Errorf("读取HTTP数据出错: %v", err)
				}
			}

		case err := <-errChan:
			return err

		case <-doneChan:
			return nil
		}
	}
}

func exportHTTPToWav(url string, wavFilePath string) {
	// Send HTTP request
	resp, err := http.Get(url)
	if err != nil {
		fmt.Printf("HTTP请求失败: %v\n", err)
		return
	}
	defer resp.Body.Close()

	// Check HTTP response status
	if resp.StatusCode != http.StatusOK {
		fmt.Printf("HTTP请求返回非200状态码: %d\n", resp.StatusCode)
		return
	}

	// Decode MP3
	streamer, format, err := mp3.Decode(resp.Body)
	if err != nil {
		fmt.Printf("无法解码MP3数据: %v\n", err)
		return
	}
	defer streamer.Close()

	// Create WAV file
	wavFile, err := os.Create(wavFilePath)
	if err != nil {
		fmt.Printf("无法创建WAV文件: %v\n", err)
		return
	}
	defer wavFile.Close()

	// Encode stream to WAV via beep/wav
	err = wav.Encode(wavFile, streamer, format)
	if err != nil {
		fmt.Printf("WAV编码失败: %v\n", err)
		return
	}

	fmt.Printf("已从HTTP导出WAV文件: %s\n", wavFilePath)
}
