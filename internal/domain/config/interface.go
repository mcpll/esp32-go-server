package user_config

import (
	"context"
	"xiaozhi-esp32-server-golang/internal/domain/config/types"
)

// UserConfigProvider user config provider interface
type UserConfigProvider interface {
	//auth
	//Get activation info by deviceId and clientId
	IsDeviceActivated(ctx context.Context, deviceId string, clientId string) (bool, error)
	GetActivationInfo(ctx context.Context, deviceId string, clientId string) (string, string, string, int)
	VerifyChallenge(ctx context.Context, deviceId string, clientId string, activationPayload types.ActivationPayload) (bool, error)

	// GetUserConfig returns the config of one device session
	GetUserConfig(ctx context.Context, userID string) (types.UConfig, error)

	// Get mqtt, mqtt_server, udp, ota, vision config
	GetSystemConfig(ctx context.Context) (string, error)

	//Register uplink event handler (e.g. device online/offline)
	NotifyDeviceEvent(ctx context.Context, eventType string, eventData map[string]interface{})
	//Register downlink event handler (e.g. message inject)
	RegisterMessageEventHandler(ctx context.Context, eventType string, eventHandler types.EventHandler)
}
