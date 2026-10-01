package manager

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"xiaozhi-esp32-server-golang/internal/components/http"
	"xiaozhi-esp32-server-golang/internal/domain/config/types"
	log "xiaozhi-esp32-server-golang/logger"
)

// HTTP API response structs

// CheckActivationResponse check-activation response
type CheckActivationResponse struct {
	Activated bool   `json:"activated"`
	Message   string `json:"message"`
}

// GetActivationInfoResponse get-activation-info response
type GetActivationInfoResponse struct {
	Activated bool   `json:"activated"`
	Code      string `json:"code,omitempty"` // Use string to match backend API
	Challenge string `json:"challenge,omitempty"`
	Message   string `json:"message,omitempty"`
}

// ActivateDeviceRequest device activation request
type ActivateDeviceRequest struct {
	DeviceId     string `json:"device_id"`
	ClientId     string `json:"client_id"`
	Code         string `json:"code"`
	Challenge    string `json:"challenge"`
	Algorithm    string `json:"algorithm"`
	SerialNumber string `json:"serial_number"`
	Hmac         string `json:"hmac"`
}

// ActivateDeviceResponse device activation response
type ActivateDeviceResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message"`
	Error   string      `json:"error,omitempty"`
	Data    interface{} `json:"data,omitempty"`
}

// IsDeviceActivated checks whether the device is activated
func (am *ConfigManager) IsDeviceActivated(ctx context.Context, deviceId string, clientId string) (bool, error) {
	// Call backend management HTTP API directly
	activated, err := am.callCheckActivationAPI(ctx, deviceId, clientId)
	if err != nil {
		log.Log().Errorf("failed to check activation status for device %s: %v", deviceId, err)
		return false, err
	}

	log.Log().Debugf("device %s activation status: %v", deviceId, activated)
	return activated, nil
}

// GetActivationInfo returns device activation info
func (am *ConfigManager) GetActivationInfo(ctx context.Context, deviceId string, clientId string) (string, string, string, int) {
	// Call backend management HTTP API directly
	activated, codeStr, challenge, message, err := am.callGetActivationInfoAPI(ctx, deviceId, clientId)
	if err != nil {
		log.Log().Errorf("failed to get activation info for device %s: %v", deviceId, err)
		return "", "", "", 0
	}

	// If already activated, return immediately
	if activated {
		log.Log().Debugf("device %s already activated", deviceId)
		return "", "", message, 0
	}

	// Check whether Challenge is empty
	if challenge == "" {
		log.Log().Errorf("device %s Challenge field is empty", deviceId)
		return "", "", "Challenge字段为空，请联系管理员", 0
	}

	// Device not activated; return activation info
	timeoutMs := 300 // Default 5-minute timeout
	log.Log().Debugf("got activation info for device %s: code=%s, challenge=%s", deviceId, codeStr, challenge)
	if codeStr == "" {
		log.Log().Warnf("device %s activation code is empty", deviceId)
	}

	return codeStr, challenge, message, timeoutMs
}

// VerifyChallenge verifies challenge and HMAC
func (am *ConfigManager) VerifyChallenge(ctx context.Context, deviceId string, clientId string, activationPayload types.ActivationPayload) (bool, error) {
	// Verify HMAC when provided
	if activationPayload.HMAC != "" {
		if !am.verifyHMAC(activationPayload.Challenge, activationPayload.HMAC) {
			log.Log().Warnf("device %s HMAC verification failed", deviceId)
			return false, fmt.Errorf("HMAC验证失败")
		}
	}

	// Call backend management activation API directly
	verified, err := am.callActivateDeviceAPI(ctx, deviceId, clientId, activationPayload)
	if err != nil {
		log.Log().Errorf("device activation failed: %v", err)
		return false, err
	}

	if verified {
		log.Log().Infof("device %s activation verified successfully", deviceId)
	}

	return verified, nil
}

// verifyHMAC verifies the HMAC signature
func (am *ConfigManager) verifyHMAC(challenge, providedHmac string) bool {
	// Configure the key as needed
	// Empty key for now; production should load from config
	secretKey := ""

	if secretKey == "" {
		// If no key configured, pass verification
		return true
	}

	mac := hmac.New(sha256.New, []byte(secretKey))
	mac.Write([]byte(challenge))
	expectedHmac := hex.EncodeToString(mac.Sum(nil))

	return expectedHmac == providedHmac
}

// HTTP API call helpers

// callCheckActivationAPI calls the check-activation API
func (am *ConfigManager) callCheckActivationAPI(ctx context.Context, deviceId, clientId string) (bool, error) {
	var response CheckActivationResponse

	// Send HTTP request
	err := am.client.DoRequest(ctx, http.RequestOptions{
		Method: "GET",
		Path:   "/api/internal/device/check-activation",
		QueryParams: map[string]string{
			"device_id": deviceId,
			"client_id": clientId,
		},
		Response: &response,
	})
	if err != nil {
		return false, fmt.Errorf("请求失败: %w", err)
	}

	log.Log().Debugf("check activation status response: %+v", response)
	return response.Activated, nil
}

// callGetActivationInfoAPI calls the get-activation-info API
func (am *ConfigManager) callGetActivationInfoAPI(ctx context.Context, deviceId, clientId string) (bool, string, string, string, error) {
	var response GetActivationInfoResponse

	// Send HTTP request
	err := am.client.DoRequest(ctx, http.RequestOptions{
		Method: "GET",
		Path:   "/api/internal/device/activation-info",
		QueryParams: map[string]string{
			"device_id": deviceId,
			"client_id": clientId,
		},
		Response: &response,
	})
	if err != nil {
		return false, "", "", "", fmt.Errorf("请求失败: %w", err)
	}

	log.Log().Debugf("get activation info response: %+v", response)

	if response.Activated {
		return true, "", "", response.Message, nil
	}

	return false, response.Code, response.Challenge, response.Message, nil
}

// callActivateDeviceAPI calls the device activation API
func (am *ConfigManager) callActivateDeviceAPI(ctx context.Context, deviceId, clientId string, activationPayload types.ActivationPayload) (bool, error) {
	// Build request body
	request := ActivateDeviceRequest{
		DeviceId:     deviceId,
		ClientId:     clientId,
		Challenge:    activationPayload.Challenge,
		Algorithm:    activationPayload.Algorithm,
		SerialNumber: activationPayload.SerialNumber,
		Hmac:         activationPayload.HMAC,
	}

	var response ActivateDeviceResponse

	// Send HTTP request
	err := am.client.DoRequest(ctx, http.RequestOptions{
		Method:   "POST",
		Path:     "/api/internal/device/activate",
		Body:     request,
		Response: &response,
	})
	if err != nil {
		return false, fmt.Errorf("请求失败: %w", err)
	}

	log.Log().Debugf("device activation response: %+v", response)

	if !response.Success {
		return false, nil
	}

	return response.Success, nil
}
