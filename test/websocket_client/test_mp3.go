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
	// HTTP接口URL
	mp3URL := "http://home.hackers365.com:55555/apk/test.mp3"
	// 指定输出的PCM文件路径
	pcmFilePath := "output.pcm"

	// 创建PCM文件
	pcmFile, err := os.Create(pcmFilePath)
	if err != nil {
		fmt.Printf("无法创建PCM文件: %v\n", err)
		return
	}
	defer pcmFile.Close()

	// 从HTTP接口获取MP3数据并处理
	err = processMP3FromHTTP(mp3URL, pcmFile)
	if err != nil {
		fmt.Printf("处理HTTP MP3数据失败: %v\n", err)
		return
	}

	fmt.Printf("HTTP MP3数据已成功解码为PCM格式，保存至: %s\n", pcmFilePath)

	// 导出WAV格式
	exportHTTPToWav(mp3URL, "output.wav")
}

type readCloserWrapper struct {
	io.Reader
}

func (r readCloserWrapper) Close() error {
	return nil
}

// 从HTTP接口获取并处理MP3数据
func processMP3FromHTTP(url string, pcmFile *os.File) error {
	// 发起HTTP请求
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("HTTP请求失败: %v", err)
	}
	defer resp.Body.Close()

	// 检查HTTP响应状态
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP请求返回非200状态码: %d", resp.StatusCode)
	}

	// 创建一个pipe用于处理数据流
	pipeReader, pipeWriter := io.Pipe()
	defer pipeReader.Close()

	// 创建一个读取缓冲区和采样缓冲区
	bufferSize := 10 * 1024            // 10KB
	buffer := make([]byte, bufferSize) // HTTP 读取缓冲区

	opusBuffer := make([]byte, 1000) // Opus 编码输出缓冲区

	// 创建一个错误通道和完成通道
	errChan := make(chan error, 1)
	doneChan := make(chan struct{}, 1)

	// 启动goroutine解码MP3和处理PCM
	go func() {
		// 尝试初始化解码器
		streamer, format, err := mp3.Decode(pipeReader)
		if err != nil {
			errChan <- fmt.Errorf("MP3解码器初始化失败: %v", err)
			return
		}
		defer streamer.Close()

		fmt.Printf("MP3解码器初始化成功，采样率: %d Hz, 声道数: %d\n",
			format.SampleRate, format.NumChannels)

		//原mp3格式信息
		sampleRate := int(format.SampleRate)
		channels := int(format.NumChannels)

		// PCM缓冲区 及 Opus帧大小(例如60ms)
		perFrameDuration := 60 // 毫秒
		frameSize := sampleRate * perFrameDuration / 1000
		pcmBuffer := make([]int16, frameSize*channels)
		opusFrames := make([][]byte, 0) // 存储编码后的Opus帧

		enc, err := opus.NewEncoder(sampleRate, channels, opus.AppAudio)
		if err != nil {
			fmt.Printf("创建Opus编码器失败: %v\n", err)
			errChan <- fmt.Errorf("创建Opus编码器失败: %v", err)
			return
		}

		beepSampleBuf := make([][2]float64, 1024) // Beep 解码缓冲区
		// 处理解码后的音频流
		currentFramePos := 0 // 当前填充到pcmBuffer的位置
		for {
			// 从流中读取采样到sampleBuf
			numSamplesRead, ok := streamer.Stream(beepSampleBuf)
			if !ok {
				// 处理剩余不足一帧的数据
				if currentFramePos > 0 {
					// 创建一个完整的帧缓冲区，用0填充剩余部分
					paddedFrame := make([]int16, len(pcmBuffer))
					copy(paddedFrame, pcmBuffer[:currentFramePos]) // 将有效数据复制到开头，剩余部分默认为0

					// 编码补齐后的完整帧
					n, err := enc.Encode(paddedFrame, opusBuffer)
					if err != nil {
						fmt.Printf("编码剩余数据失败: %v\n", err)
						// 可能需要通过 errChan 发送错误
					} else {
						frameData := make([]byte, n)
						copy(frameData, opusBuffer[:n])
						opusFrames = append(opusFrames, frameData)
						// 注意：这里编码的是一个完整的帧，即使原始数据不足
						fmt.Printf("已编码最后补齐的 %d 个PCM样本 (原始 %d)\n", len(paddedFrame), currentFramePos)
					}
				}
				// 解码完成
				doneChan <- struct{}{}
				return
			}

			// 将读取到的float64样本转换为int16并填充到pcmBuffer
			for i := 0; i < numSamplesRead; i++ {
				// 直接进行转换
				leftSample := int16(beepSampleBuf[i][0] * 32767.0)
				rightSample := int16(beepSampleBuf[i][1] * 32767.0)

				// 写入PCM数据
				pcmBuffer[currentFramePos] = leftSample
				if channels > 1 {
					pcmBuffer[currentFramePos+1] = rightSample
				}
				currentFramePos += channels

				// 如果pcmBuffer已满一帧，则进行编码
				if currentFramePos == len(pcmBuffer) {
					n, err := enc.Encode(pcmBuffer, opusBuffer)
					if err != nil {
						fmt.Printf("编码失败: %v\n", err)
						errChan <- fmt.Errorf("编码失败: %v", err)
						return
					}

					// 将当前帧复制到新的切片中并添加到帧数组
					frameData := make([]byte, n)
					copy(frameData, opusBuffer[:n])
					opusFrames = append(opusFrames, frameData)

					fmt.Printf("已编码一帧 (%d PCM样本)\n", len(pcmBuffer))
					currentFramePos = 0 // 重置帧位置
				}
			}
		}
	}()

	// 创建定时器，每100ms发送一次数据
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	// 开始循环读取HTTP数据并写入pipe
	for {
		select {
		case <-ticker.C:
			// 从HTTP响应读取数据
			n, err := resp.Body.Read(buffer)

			// 如果读取到数据，写入pipe
			if n > 0 {
				_, writeErr := pipeWriter.Write(buffer[:n])
				if writeErr != nil {
					return fmt.Errorf("写入pipe失败: %v", writeErr)
				}
				fmt.Printf("已读取并写入 %d 字节MP3数据\n", n)
			}

			// 处理EOF或错误
			if err != nil {
				if err == io.EOF {
					fmt.Println("HTTP数据流已读取完毕")
					pipeWriter.Close() // 关闭pipe写入端

					// 等待解码完成或出错
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
	// 发起HTTP请求
	resp, err := http.Get(url)
	if err != nil {
		fmt.Printf("HTTP请求失败: %v\n", err)
		return
	}
	defer resp.Body.Close()

	// 检查HTTP响应状态
	if resp.StatusCode != http.StatusOK {
		fmt.Printf("HTTP请求返回非200状态码: %d\n", resp.StatusCode)
		return
	}

	// 解码MP3
	streamer, format, err := mp3.Decode(resp.Body)
	if err != nil {
		fmt.Printf("无法解码MP3数据: %v\n", err)
		return
	}
	defer streamer.Close()

	// 创建WAV文件
	wavFile, err := os.Create(wavFilePath)
	if err != nil {
		fmt.Printf("无法创建WAV文件: %v\n", err)
		return
	}
	defer wavFile.Close()

	// 使用beep/wav包将流编码为WAV
	err = wav.Encode(wavFile, streamer, format)
	if err != nil {
		fmt.Printf("WAV编码失败: %v\n", err)
		return
	}

	fmt.Printf("已从HTTP导出WAV文件: %s\n", wavFilePath)
}
