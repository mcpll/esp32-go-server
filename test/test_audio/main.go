package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	//读取文件，使用参数传入，输入输出文件
	inputFilePath := flag.String("input", "", "输入文件路径")
	outputFilePath := flag.String("output", "", "输出文件路径")
	sampleRate := flag.Int("sampleRate", 24000, "采样率")
	channels := flag.Int("channels", 1, "声道数")
	flag.Parse()

	if *inputFilePath == "" || *outputFilePath == "" {
		flag.Usage()
		return
	}

	//读取文件所有内容
	content, err := os.ReadFile(*inputFilePath)
	if err != nil {
		fmt.Println("读取文件失败:", err)
		return
	}

	fmt.Println("读取文件成功:", *inputFilePath)

	opusData := [][]byte{content}
	pcmData, err := OpusToWav(opusData, *sampleRate, *channels, *outputFilePath)
	if err != nil {
		fmt.Println("转换失败:", err)
		return
	}
	fmt.Println("pcmData len: ", len(pcmData[0]))

	fmt.Println("转换成功:", *outputFilePath)
}
