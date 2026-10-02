//go:build !cgo || (linux && arm64)

package ten_vad

import (
	"errors"
	"sync"
	"unsafe"
)

// TEN-VAD ships no binary for linux/arm64 (upstream only has Linux/x64), so this stub keeps the
// package compiling there. Use silero_vad or webrtc_vad on those builds.
var errUnavailable = errors.New("ten-vad is not available on this build (needs cgo and a platform other than linux/arm64)")

// TenVADDLL is the stub counterpart of the cgo bindings.
type TenVADDLL struct{}

var (
	globalTenVAD *TenVADDLL
	dllOnce      sync.Once
)

// GetInstance returns the stub singleton.
func GetInstance() *TenVADDLL {
	dllOnce.Do(func() { globalTenVAD = &TenVADDLL{} })
	return globalTenVAD
}

func (t *TenVADDLL) CreateInstance(hopSize int, threshold float32) (unsafe.Pointer, error) {
	return nil, errUnavailable
}

func (t *TenVADDLL) ProcessAudio(handle unsafe.Pointer, audioData []int16) (float32, int32, error) {
	return 0, 0, errUnavailable
}

func (t *TenVADDLL) DestroyInstance(handle unsafe.Pointer) error {
	return errUnavailable
}

func (t *TenVADDLL) GetVersion() string {
	return "unavailable"
}
