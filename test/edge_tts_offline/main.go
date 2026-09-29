package main

import (
	"bufio"
	"encoding/binary"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"

	"github.com/gorilla/websocket"
)

// 创建WAV文件头
func createWAVHeader(dataSize uint32) []byte {
	// WAV头部总共44字节
	header := make([]byte, 44)

	// RIFF chunk descriptor
	copy(header[0:4], []byte("RIFF"))
	binary.LittleEndian.PutUint32(header[4:8], 36+dataSize) // 文件总长度减去8字节
	copy(header[8:12], []byte("WAVE"))

	// fmt sub-chunk
	copy(header[12:16], []byte("fmt "))
	binary.LittleEndian.PutUint32(header[16:20], 16)      // fmt chunk大小
	binary.LittleEndian.PutUint16(header[20:22], 1)       // 音频格式 (1 = PCM)
	binary.LittleEndian.PutUint16(header[22:24], 1)       // 通道数 (1 = 单声道)
	binary.LittleEndian.PutUint32(header[24:28], 24000)   // 采样率 (24kHz)
	binary.LittleEndian.PutUint32(header[28:32], 24000*2) // 字节率 (SampleRate * BlockAlign)
	binary.LittleEndian.PutUint16(header[32:34], 2)       // 块对齐 (通道数 * 位深度 / 8)
	binary.LittleEndian.PutUint16(header[34:36], 16)      // 位深度 (16 bits)

	// data sub-chunk
	copy(header[36:40], []byte("data"))
	binary.LittleEndian.PutUint32(header[40:44], dataSize) // 音频数据大小

	return header
}

func main() {
	// 解析命令行参数
	url := flag.String("url", "ws://192.168.208.214:8081", "WebSocket服务器地址")
	flag.Parse()

	// 连接到 WebSocket 服务器
	c, _, err := websocket.DefaultDialer.Dial(*url, nil)
	if err != nil {
		log.Fatal("连接失败:", err)
	}
	defer c.Close()

	// 创建一个用于处理中断信号的通道
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	// 创建一个用于检测中断的通道
	done := make(chan struct{})

	// 在后台监听中断信号
	go func() {
		<-interrupt
		fmt.Println("\n收到中断信号，正在关闭连接...")

		// 优雅地关闭连接
		err := c.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		if err != nil {
			log.Println("关闭连接时发生错误:", err)
		}
		close(done)
	}()

	// 创建标准输入读取器
	reader := bufio.NewReader(os.Stdin)
	fileCount := 1

	fmt.Println("请输入要转换的文本（输入'exit'退出）：")
	for {
		select {
		case <-done:
			return
		default:
			// 读取用户输入
			fmt.Print("> ")
			text, err := reader.ReadString('\n')
			if err != nil {
				log.Println("读取输入失败:", err)
				continue
			}

			// 去除输入文本的首尾空白字符
			text = strings.TrimSpace(text)

			// 检查是否退出
			if text == "exit" {
				fmt.Println("程序退出...")
				return
			}

			// 如果输入为空，继续下一次循环
			if text == "" {
				continue
			}

			// 发送消息
			err = c.WriteMessage(websocket.TextMessage, []byte(text))
			if err != nil {
				log.Println("发送消息失败:", err)
				continue
			}
			fmt.Printf("已发送消息: %s\n", text)

			// 接收消息
			msgType, data, err := c.ReadMessage()
			if err != nil {
				log.Println("接收消息失败:", err)
				continue
			}
			fmt.Printf("接收到的数据长度: %d 字节,类型: %d\n", len(data), msgType)

			// 生成唯一的文件名
			filename := fmt.Sprintf("voice_%d.wav", fileCount)
			fileCount++

			// 创建WAV头部
			wavHeader := createWAVHeader(uint32(len(data)))

			// 创建完整的WAV文件数据
			fullData := make([]byte, len(wavHeader)+len(data))
			copy(fullData[0:], wavHeader)
			copy(fullData[len(wavHeader):], data)

			// 保存完整的WAV文件
			err = os.WriteFile(filename, fullData, 0644)
			if err != nil {
				log.Println("保存文件失败:", err)
				continue
			}
			fmt.Printf("音频数据已保存到 %s\n", filename)
		}
	}
}
