package util

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

// GeneratePasswordSignature generates a password signature
// HMAC-SHA256 from clientId + '|' + username and the signing key
func GeneratePasswordSignature(data, key string) string {
	// generate signature with HMAC-SHA256
	h := hmac.New(sha256.New, []byte(key))
	h.Write([]byte(data))
	signature := h.Sum(nil)

	// return the base64-encoded signature
	return base64.StdEncoding.EncodeToString(signature)
}

// ValidateMqttCredentials validates MQTT credentials
// implements the provided JavaScript validation logic
func ValidateMqttCredentials(clientId, username, password, signatureKey string) (*MqttCredentialInfo, error) {
	// validate signing key
	if signatureKey == "" {
		return nil, fmt.Errorf("缺少签名密钥配置")
	}

	// validate clientId
	if clientId == "" {
		return nil, fmt.Errorf("clientId必须是非空字符串")
	}

	// validate clientId format (must contain @@@)
	clientIdParts := strings.Split(clientId, "@@@")
	if len(clientIdParts) != 3 {
		return nil, fmt.Errorf("clientId格式错误，必须包含@@@分隔符")
	}

	// validate username
	if username == "" {
		return nil, fmt.Errorf("username必须是非空字符串")
	}

	// try decoding username (should be base64-encoded JSON)
	var userData map[string]interface{}
	decodedUsername, err := base64.StdEncoding.DecodeString(username)
	if err != nil {
		return nil, fmt.Errorf("username不是有效的base64编码: %v", err)
	}

	if err := json.Unmarshal(decodedUsername, &userData); err != nil {
		return nil, fmt.Errorf("username不是有效的base64编码JSON: %v", err)
	}

	// validate password signature
	signatureData := clientId + "|" + username
	expectedSignature := GeneratePasswordSignature(signatureData, signatureKey)
	if password != expectedSignature {
		return nil, fmt.Errorf("密码签名验证失败")
	}

	// parse fields from clientId
	groupId := clientIdParts[0]
	macAddress := strings.ReplaceAll(clientIdParts[1], "_", ":")
	uuid := clientIdParts[2]

	// on success, return the parsed useful fields
	return &MqttCredentialInfo{
		GroupId:    groupId,
		MacAddress: macAddress,
		UUID:       uuid,
		UserData:   userData,
	}, nil
}

// MqttCredentialInfo MQTT credential info
type MqttCredentialInfo struct {
	GroupId    string                 `json:"groupId"`
	MacAddress string                 `json:"macAddress"`
	UUID       string                 `json:"uuid"`
	UserData   map[string]interface{} `json:"userData"`
}

// GenerateMqttCredentials generates MQTT credentials
// used by the OTA API to build MQTT connection info
func GenerateMqttCredentials(deviceId, clientId, ip, signatureKey string) (*MqttCredentials, error) {
	// normalize deviceId (replace colons with underscores)
	deviceId = strings.ReplaceAll(deviceId, ":", "_")

	// build username payload (includes IP info)
	userName := struct {
		Ip string `json:"ip"`
	}{
		Ip: ip,
	}
	userNameJson, err := json.Marshal(userName)
	if err != nil {
		return nil, fmt.Errorf("用户名序列化失败: %v", err)
	}
	base64UserName := base64.StdEncoding.EncodeToString(userNameJson)

	// build clientId as GID_test@@@deviceId@@@clientId
	mqttClientId := fmt.Sprintf("GID_test@@@%s@@@%s", deviceId, clientId)

	// generate password signature
	var pwd string
	if signatureKey != "" {
		// generate password with the signing key
		signatureData := mqttClientId + "|" + base64UserName
		pwd = GeneratePasswordSignature(signatureData, signatureKey)
	} else {
		// if no signing key is configured, fall back to the previous logic
		pwd = Sha256Digest([]byte(mqttClientId))
	}

	return &MqttCredentials{
		ClientId: mqttClientId,
		Username: base64UserName,
		Password: pwd,
	}, nil
}

// MqttCredentials MQTT credentials
type MqttCredentials struct {
	ClientId string `json:"client_id"`
	Username string `json:"username"`
	Password string `json:"password"`
}
