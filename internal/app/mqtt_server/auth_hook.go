package mqtt_server

import (
	"bytes"
	"crypto/aes"
	"encoding/base64"
	"encoding/json"

	"xiaozhi-esp32-server-golang/internal/util"
	log "xiaozhi-esp32-server-golang/logger"

	mqttServer "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/packets"
	"github.com/spf13/viper"
)

// AuthHook implements custom auth logic
// Supports normal users and super admin
// Normal user: username is base64({"ip":"1.202.193.194"}), password is HMAC-SHA256 signature
// Super admin: username admin, password shijingbo!@#
type AuthHook struct {
	mqttServer.HookBase
}

func (h *AuthHook) ID() string {
	return "custom-auth-hook"
}

func (h *AuthHook) Provides(b byte) bool {
	return b == mqttServer.OnConnectAuthenticate
}

func (h *AuthHook) OnConnectAuthenticate(cl *mqttServer.Client, pk packets.Packet) bool {
	// Check whether auth is enabled
	enableAuth := viper.GetBool("mqtt_server.enable_auth")
	if !enableAuth {
		//log.Infof("MQTT auth disabled, allow all connections")
		return true
	}

	username := string(pk.Connect.Username)
	password := string(pk.Connect.Password)
	clientId := string(pk.Connect.ClientIdentifier)

	// Super-admin check
	adminUsername := configuredAdminUsername()
	adminPassword := configuredAdminPassword()
	if username == adminUsername && password == adminPassword {
		log.Infof("Super admin login succeeded: %s", username)
		return true
	}
	if username == adminUsername {
		log.Warnf("MQTT admin login failed: username=%s, clientId=%s, reason=bad password", username, clientId)
		return false
	}

	// Normal-user check - new signature verification
	signatureKey := viper.GetString("mqtt_server.signature_key")
	if signatureKey != "" {
		credentialInfo, err := util.ValidateMqttCredentials(clientId, username, password, signatureKey)
		//log.Infof("MQTT user auth start: clientId=%s, username=%s, password=%s, signatureKey=%s",
		//	clientId, username, password, signatureKey)
		//log.Infof("MQTT user auth start: credentialInfo=%+v", credentialInfo)

		if err != nil {
			log.Warnf("MQTT credential verification failed: username=%s, clientId=%s, err=%v", username, clientId, err)
			return false
		}

		log.Infof("MQTT user auth succeeded: groupId=%s, macAddress=%s, uuid=%s",
			credentialInfo.GroupId, credentialInfo.MacAddress, credentialInfo.UUID)
		return true
	}

	// If no signing key configured, fall back to legacy AES verification
	log.Warnf("OTA signature key config missing, using AES verification")
	return h.validateWithAes(username, password)
}

// validateWithAes verifies password with AES (backward compatible)
func (h *AuthHook) validateWithAes(username, password string) bool {
	// Normal-user check
	decoded, err := base64.StdEncoding.DecodeString(username)
	if err != nil {
		return false
	}
	var userInfo map[string]string
	if err := json.Unmarshal(decoded, &userInfo); err != nil {
		return false
	}
	if _, ok := userInfo["ip"]; !ok {
		return false
	}
	// Verify password is AES-encrypted username
	if !checkAesPassword(username, password) {
		return false
	}
	return true
}

// checkAesPassword verifies password is AES-ECB of base64(username)
func checkAesPassword(username, password string) bool {
	key := []byte("xiaozhi_aes_key_1") // 16-byte key; prefer configuring in practice
	ciphertext, err := aesEncryptECB([]byte(username), key)
	if err != nil {
		return false
	}
	cipherBase64 := base64.StdEncoding.EncodeToString(ciphertext)
	return cipherBase64 == password
}

// aesEncryptECB implements AES-ECB encrypt
func aesEncryptECB(src, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	blockSize := block.BlockSize()
	// PKCS7 padding
	padding := blockSize - len(src)%blockSize
	padtext := bytes.Repeat([]byte{byte(padding)}, padding)
	src = append(src, padtext...)
	encrypted := make([]byte, len(src))
	for bs, be := 0, blockSize; bs < len(src); bs, be = bs+blockSize, be+blockSize {
		block.Encrypt(encrypted[bs:be], src[bs:be])
	}
	return encrypted, nil
}
