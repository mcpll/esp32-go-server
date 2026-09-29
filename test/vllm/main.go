package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
)

func main() {
	// 命令行参数
	imagePath := flag.String("image", "", "图片文件路径")
	question := flag.String("question", "", "问题文本")
	url := flag.String("url", "", "HTTP接口地址")
	deviceId := flag.String("device", "", "Device-Id 头部")
	flag.Parse()

	if *imagePath == "" || *question == "" || *url == "" || *deviceId == "" {
		fmt.Println("用法: main -image <图片路径> -question <问题> -url <接口地址> -device <Device-Id>")
		os.Exit(1)
	}

	// 打开图片文件
	file, err := os.Open(*imagePath)
	if err != nil {
		fmt.Printf("打开图片失败: %v\n", err)
		os.Exit(1)
	}
	defer file.Close()

	// 创建 multipart writer
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// 写入图片字段
	fileWriter, err := writer.CreateFormFile("file", (*imagePath))
	if err != nil {
		fmt.Printf("创建图片字段失败: %v\n", err)
		os.Exit(1)
	}
	_, err = io.Copy(fileWriter, file)
	if err != nil {
		fmt.Printf("写入图片内容失败: %v\n", err)
		os.Exit(1)
	}

	// 写入文本字段
	_ = writer.WriteField("question", *question)

	writer.Close()

	// 创建自定义请求，添加Device-Id头
	req, err := http.NewRequest("POST", *url, body)
	if err != nil {
		fmt.Printf("创建请求失败: %v\n", err)
		os.Exit(1)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Device-Id", *deviceId)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("请求失败: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	// 读取响应
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("读取响应失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("响应:")
	fmt.Println(string(respBody))
}
