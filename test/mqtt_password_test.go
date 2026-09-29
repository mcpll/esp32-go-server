package main

import (
	"fmt"
	"xiaozhi-esp32-server-golang/internal/util"
)

func main() {
	// Test parameters
	deviceId := "02:4A:7D:E3:89:BF"
	clientId := "e3b0c442-98fc-4e1a-8c3d-6a5b6a5b6a5b"
	ip := "1.202.193.194"
	signatureKey := "your_ota_signature_key_here"

	fmt.Println("=== MQTT凭据生成测试 ===")

	// Generate MQTT credentials
	credentials, err := util.GenerateMqttCredentials(deviceId, clientId, ip, signatureKey)
	if err != nil {
		fmt.Printf("生成MQTT凭据失败: %v\n", err)
		return
	}

	fmt.Printf("Device ID: %s\n", deviceId)
	fmt.Printf("Client ID: %s\n", clientId)
	fmt.Printf("IP: %s\n", ip)
	fmt.Printf("MQTT Client ID: %s\n", credentials.ClientId)
	fmt.Printf("Username (base64): %s\n", credentials.Username)
	fmt.Printf("Password: %s\n", credentials.Password)

	fmt.Println("\n=== MQTT凭据验证测试 ===")

	// Validate MQTT credentials
	credentialInfo, err := util.ValidateMqttCredentials(
		credentials.ClientId,
		credentials.Username,
		credentials.Password,
		signatureKey,
	)
	if err != nil {
		fmt.Printf("验证MQTT凭据失败: %v\n", err)
		return
	}

	fmt.Printf("验证成功!\n")
	fmt.Printf("Group ID: %s\n", credentialInfo.GroupId)
	fmt.Printf("MAC Address: %s\n", credentialInfo.MacAddress)
	fmt.Printf("UUID: %s\n", credentialInfo.UUID)
	fmt.Printf("User Data: %+v\n", credentialInfo.UserData)

	fmt.Println("\n=== 错误情况测试 ===")

	// Test wrong password
	_, err = util.ValidateMqttCredentials(
		credentials.ClientId,
		credentials.Username,
		"wrong_password",
		signatureKey,
	)
	if err != nil {
		fmt.Printf("错误密码验证失败（预期）: %v\n", err)
	}

	// Test wrong clientId format
	_, err = util.ValidateMqttCredentials(
		"invalid_client_id",
		credentials.Username,
		credentials.Password,
		signatureKey,
	)
	if err != nil {
		fmt.Printf("错误clientId格式验证失败（预期）: %v\n", err)
	}

	// Test wrong username format
	_, err = util.ValidateMqttCredentials(
		credentials.ClientId,
		"invalid_username",
		credentials.Password,
		signatureKey,
	)
	if err != nil {
		fmt.Printf("错误username格式验证失败（预期）: %v\n", err)
	}
}
