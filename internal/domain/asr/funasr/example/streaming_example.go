package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/go-audio/audio"
	"github.com/go-audio/wav"

	"xiaozhi-esp32-server-golang/internal/domain/asr/funasr"
)

// readWavFile reads a WAV file and converts it to PCM []float32
func readWavFile(filePath string) ([]float32, error) {
	// Open WAV file
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("打开WAV文件失败: %v", err)
	}
	defer file.Close()

	// Create WAV decoder
	wavDecoder := wav.NewDecoder(file)
	if !wavDecoder.IsValidFile() {
		return nil, fmt.Errorf("无效的WAV文件")
	}

	// Read WAV file info
	wavDecoder.ReadInfo()
	format := wavDecoder.Format()

	fmt.Printf("WAV格式: 采样率=%dHz, 通道数=%d\n",
		int(format.SampleRate), format.NumChannels)

	// Read all PCM data
	var allPcmData []float32

	// Use a 20ms frame size as the buffer
	perFrameDuration := 20
	frameSize := int(format.SampleRate) * perFrameDuration / 1000
	audioBuf := &audio.IntBuffer{
		Format:         format,
		SourceBitDepth: 16,
		Data:           make([]int, frameSize*format.NumChannels),
	}

	fmt.Printf("使用帧大小: %d 采样点 (%.1fms)\n", frameSize, float64(perFrameDuration))
	fmt.Println("开始读取WAV数据...")

	for {
		// Read WAV data
		n, err := wavDecoder.PCMBuffer(audioBuf)
		if err == io.EOF || n == 0 {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("读取WAV数据失败: %v", err)
		}

		// Convert int data to float32 (range -1.0 to 1.0)
		for i := 0; i < n; i++ {
			// Convert int to float32, mapping [-32768, 32767] to [-1.0, 1.0]
			floatSample := float32(audioBuf.Data[i]) / 32767.0
			allPcmData = append(allPcmData, floatSample)
		}
	}

	fmt.Printf("成功读取WAV文件，总采样点数: %d, 时长: %.2f秒\n",
		len(allPcmData), float64(len(allPcmData))/float64(format.SampleRate))

	return allPcmData, nil
}

func main() {
	// Define command-line flags
	var (
		host = flag.String("host", "192.168.208.214", "FunASR服务器IP地址")
		port = flag.String("port", "10096", "FunASR服务器端口")
		mode = flag.String("mode", "offline", "识别模式 (online/offline)")
		file = flag.String("file", "test.wav", "要识别的WAV文件路径")
	)

	// Parse command-line flags
	flag.Parse()

	// Show usage
	if len(os.Args) < 2 {
		fmt.Println("用法: ./streaming_example [选项]")
		fmt.Println("选项:")
		flag.PrintDefaults()
		fmt.Println("\n示例:")
		fmt.Println("  ./streaming_example -host=192.168.1.100 -port=10095 -file=audio.wav")
		fmt.Println("  ./streaming_example -mode=online -file=test.wav")
		return
	}

	config := funasr.FunasrConfig{
		Host:          *host,
		Port:          *port,
		Mode:          *mode,
		SampleRate:    16000,
		ChunkSize:     []int{5, 10, 5},
		ChunkInterval: 10,
		Timeout:       30,
		AutoEnd:       false,
	}

	// Create ASR instance from config
	asr, err := funasr.NewFunasr(config)
	if err != nil {
		fmt.Printf("创建ASR实例失败: %v\n", err)
		return
	}

	fmt.Printf("目标服务器: %s:%s, 模式: %s\n", config.Host, config.Port, config.Mode)

	// Use the audio file path from the command line
	audioFilePath := *file

	// Check that the audio file exists
	if _, err := os.Stat(audioFilePath); os.IsNotExist(err) {
		fmt.Printf("音频文件 %s 不存在\n", audioFilePath)
		fmt.Println("请提供有效的音频文件路径")
		return
	}

	// Read WAV file and convert to PCM
	pcmData, err := readWavFile(audioFilePath)
	if err != nil {
		fmt.Printf("读取WAV文件失败: %v\n", err)
		return
	}

	// Run recognition
	result, err := asr.Process(pcmData)
	if err != nil {
		fmt.Printf("识别失败: %v\n", err)
		return
	}

	// Format and print results
	fmt.Println("识别结果:")
	fmt.Println(strings.Repeat("-", 40))
	fmt.Println(result)
	fmt.Println(strings.Repeat("-", 40))
}
