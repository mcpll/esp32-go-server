package types

import "context"

// IConn protocol-agnostic connection interface implemented by websocket/mqtt_udp adapters
// Extend methods as needed

const (
	TransportTypeWebsocket = "websocket"
	TransportTypeMqttUdp   = "udp"
)

type IConn interface {
	// Send command/signaling data
	SendCmd(msg []byte) error
	// Receive command/signaling data
	RecvCmd(ctx context.Context, timeout int) ([]byte, error)
	// Send audio data
	SendAudio(audio []byte) error
	// Receive audio data
	RecvAudio(ctx context.Context, timeout int) ([]byte, error)

	GetDeviceID() string

	Close() error
	OnClose(func(deviceId string))

	CloseAudioChannel() error

	GetTransportType() string

	//Get private data
	GetData(key string) (interface{}, error)
}

type OnNewConnection func(conn IConn)
