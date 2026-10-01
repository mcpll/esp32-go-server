package ten_vad

import (
	"errors"
	"fmt"
	"sync"
	"unsafe"

	log "xiaozhi-esp32-server-golang/logger"

	. "xiaozhi-esp32-server-golang/internal/domain/vad/inter"
)

// VAD defaults
var defaultVADConfig = map[string]interface{}{
	"hop_size":  512,
	"threshold": 0.3,
}

// TenVAD TEN-VAD model implementation
type TenVAD struct {
	handle    unsafe.Pointer
	hopSize   int
	threshold float32
	mu        sync.Mutex
}

// NewTenVAD creates a TenVAD instance
func NewTenVAD(config map[string]interface{}) (*TenVAD, error) {
	hopSize, ok := config["hop_size"].(int)
	if !ok {
		// Try converting from float64
		if hopSizeFloat, ok := config["hop_size"].(float64); ok {
			hopSize = int(hopSizeFloat)
		} else {
			hopSize = 512 // default
		}
	}

	threshold, ok := config["threshold"].(float64)
	if !ok {
		// Try converting from float32
		if thresholdFloat32, ok := config["threshold"].(float32); ok {
			threshold = float64(thresholdFloat32)
		} else {
			threshold = 0.3 // default
		}
	}

	// Create TEN-VAD instance
	tenVAD := GetInstance()
	handle, err := tenVAD.CreateInstance(hopSize, float32(threshold))
	if err != nil {
		return nil, fmt.Errorf("创建TEN-VAD实例失败: %v", err)
	}

	log.Debugf("created TEN-VAD instance ok, hopSize: %d, threshold: %f", hopSize, threshold)

	return &TenVAD{
		handle:    handle,
		hopSize:   hopSize,
		threshold: float32(threshold),
	}, nil
}

// IsVAD implements the VAD IsVAD method
func (t *TenVAD) IsVAD(pcmData []float32) (bool, error) {
	return t.IsVADExt(pcmData, 16000, t.hopSize)
}

// IsVADExt implements the VAD IsVADExt method
func (t *TenVAD) IsVADExt(pcmData []float32, sampleRate int, frameSize int) (bool, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.handle == nil {
		return false, errors.New("TEN-VAD实例未初始化")
	}

	if len(pcmData) == 0 {
		return false, nil
	}

	// Convert float32 to int16
	// float32 range: -1.0 to 1.0
	// int16 range: -32768 to 32767
	int16Data := make([]int16, len(pcmData))
	for i, f := range pcmData {
		// Clamp and convert
		if f > 1.0 {
			f = 1.0
		} else if f < -1.0 {
			f = -1.0
		}
		int16Data[i] = int16(f * 32768.0)
	}

	// Process in hopSize frames
	tenVAD := GetInstance()
	hasVoice := false
	voiceFrameCount := 0

	for i := 0; i < len(int16Data); i += t.hopSize {
		end := i + t.hopSize
		if end > len(int16Data) {
			end = len(int16Data)
		}

		frame := int16Data[i:end]
		// If frame length < hopSize, pad or skip
		if len(frame) < t.hopSize {
			// For the last short frame, skip or pad
			// Here we skip short frames
			continue
		}

		_, flag, err := tenVAD.ProcessAudio(t.handle, frame)
		if err != nil {
			log.Errorf("TEN-VAD failed to process audio frame: %v", err)
			continue
		}

		// flag == 1 means speech detected
		if flag == 1 {
			hasVoice = true
			voiceFrameCount++
		}
	}

	// If any frame has speech, treat as voice activity
	return hasVoice, nil
}

// Reset resets VAD detector state
func (t *TenVAD) Reset() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	// TEN-VAD needs no reset; each process is independent
	// but we could recreate the instance to reset state
	// no-op here because TEN-VAD is stateless
	return nil
}

// Close releases resources
func (t *TenVAD) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.handle != nil {
		tenVAD := GetInstance()
		err := tenVAD.DestroyInstance(t.handle)
		if err != nil {
			return fmt.Errorf("销毁TEN-VAD实例失败: %v", err)
		}
		t.handle = nil
	}
	return nil
}

// IsValid checks whether the resource is valid
func (t *TenVAD) IsValid() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.handle != nil
}

// AcquireVAD creates and returns a TEN-VAD instance (global pool)
func AcquireVAD(config map[string]interface{}) (VAD, error) {
	return NewTenVAD(config)
}

// ReleaseVAD releases a VAD instance
func ReleaseVAD(vad VAD) error {
	if vad != nil {
		return vad.Close()
	}
	return nil
}
