package types

import "context"

type EventHandler func(ctx context.Context, eventType string, eventData map[string]interface{}) (string, error)

// upstream push events: main process => console control
const (
	EventDeviceOnline  = "/api/device/active"   //device online
	EventDeviceOffline = "/api/device/inactive" //device offline
)

// downstream pull events: console control => main process
const (
	EventHandleMessageInject = "/api/device/inject_msg" //handle message injection
)
