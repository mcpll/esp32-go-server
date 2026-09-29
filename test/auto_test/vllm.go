package main

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
)

func requestVllm(imagePath, question, url, deviceId string) (string, error) {

	if imagePath == "" || question == "" || url == "" || deviceId == "" {
		return "", fmt.Errorf("用法: main -image <图片路径> -question <问题> -url <接口地址> -device <Device-Id>")
	}

	// 打开图片文件
	file, err := os.Open(imagePath)
	if err != nil {
		fmt.Printf("打开图片失败: %v\n", err)
		return "", err
	}
	defer file.Close()

	// 创建 multipart writer
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// 写入图片字段
	fileWriter, err := writer.CreateFormFile("file", (imagePath))
	if err != nil {
		fmt.Printf("创建图片字段失败: %v\n", err)
		return "", err
	}
	_, err = io.Copy(fileWriter, file)
	if err != nil {
		fmt.Printf("写入图片内容失败: %v\n", err)
		return "", err
	}

	// 写入文本字段
	_ = writer.WriteField("question", question)

	writer.Close()

	// 创建自定义请求，添加Device-Id头
	req, err := http.NewRequest("POST", url, body)
	if err != nil {
		fmt.Printf("创建请求失败: %v\n", err)
		return "", err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Device-Id", deviceId)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("请求失败: %v\n", err)
		return "", err
	}
	defer resp.Body.Close()

	// 读取响应
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("读取响应失败: %v\n", err)
		return "", err
	}
	responseText := string(respBody)
	fmt.Println("响应:")
	fmt.Println(responseText)
	return responseText, nil
}
