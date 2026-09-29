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

// Build WAV file header
func createWAVHeader(dataSize uint32) []byte {
	// WAV header is 44 bytes total
	header := make([]byte, 44)

	// RIFF chunk descriptor
	copy(header[0:4], []byte("RIFF"))
	binary.LittleEndian.PutUint32(header[4:8], 36+dataSize) // File size minus 8 bytes
	copy(header[8:12], []byte("WAVE"))

	// fmt sub-chunk
	copy(header[12:16], []byte("fmt "))
	binary.LittleEndian.PutUint32(header[16:20], 16)      // fmt chunk size
	binary.LittleEndian.PutUint16(header[20:22], 1)       // Audio format (1 = PCM)
	binary.LittleEndian.PutUint16(header[22:24], 1)       // Channels (1 = mono)
	binary.LittleEndian.PutUint32(header[24:28], 24000)   // Sample rate (24kHz)
	binary.LittleEndian.PutUint32(header[28:32], 24000*2) // Byte rate (SampleRate * BlockAlign)
	binary.LittleEndian.PutUint16(header[32:34], 2)       // Block align (channels * bit depth / 8)
	binary.LittleEndian.PutUint16(header[34:36], 16)      // Bit depth (16 bits)

	// data sub-chunk
	copy(header[36:40], []byte("data"))
	binary.LittleEndian.PutUint32(header[40:44], dataSize) // Audio data size

	return header
}

func main() {
	// Parse CLI flags
	url := flag.String("url", "ws://192.168.208.214:8081", "WebSocket服务器地址")
	flag.Parse()

	// Connect to WebSocket server
	c, _, err := websocket.DefaultDialer.Dial(*url, nil)
	if err != nil {
		log.Fatal("connect failed:", err)
	}
	defer c.Close()

	// Channel for interrupt signals
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	// Channel to detect interrupt
	done := make(chan struct{})

	// Listen for interrupts in the background
	go func() {
		<-interrupt
		fmt.Println("\n收到中断信号，正在关闭连接...")

		// Gracefully close the connection
		err := c.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		if err != nil {
			log.Println("error while closing connection:", err)
		}
		close(done)
	}()

	// Create stdin reader
	reader := bufio.NewReader(os.Stdin)
	fileCount := 1

	fmt.Println("请输入要转换的文本（输入'exit'退出）：")
	for {
		select {
		case <-done:
			return
		default:
			// Read user input
			fmt.Print("> ")
			text, err := reader.ReadString('\n')
			if err != nil {
				log.Println("failed to read input:", err)
				continue
			}

			// Trim surrounding whitespace
			text = strings.TrimSpace(text)

			// Check for exit
			if text == "exit" {
				fmt.Println("程序退出...")
				return
			}

			// If empty input, continue
			if text == "" {
				continue
			}

			// Send message
			err = c.WriteMessage(websocket.TextMessage, []byte(text))
			if err != nil {
				log.Println("failed to send message:", err)
				continue
			}
			fmt.Printf("已发送消息: %s\n", text)

			// Receive message
			msgType, data, err := c.ReadMessage()
			if err != nil {
				log.Println("failed to receive message:", err)
				continue
			}
			fmt.Printf("接收到的数据长度: %d 字节,类型: %d\n", len(data), msgType)

			// Generate unique filename
			filename := fmt.Sprintf("voice_%d.wav", fileCount)
			fileCount++

			// Create WAV header
			wavHeader := createWAVHeader(uint32(len(data)))

			// Build full WAV file bytes
			fullData := make([]byte, len(wavHeader)+len(data))
			copy(fullData[0:], wavHeader)
			copy(fullData[len(wavHeader):], data)

			// Save the complete WAV file
			err = os.WriteFile(filename, fullData, 0644)
			if err != nil {
				log.Println("failed to save file:", err)
				continue
			}
			fmt.Printf("音频数据已保存到 %s\n", filename)
		}
	}
}
