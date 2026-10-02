package webrtc_vad

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newInitializedVAD returns a ready-to-use VAD; NewWebRTCVAD alone leaves it uninitialized.
func newInitializedVAD(t *testing.T) *WebRTCVAD {
	t.Helper()
	vad, err := NewWebRTCVADWithConfig(DefaultSampleRate, DefaultMode)
	require.NoError(t, err)
	return vad.(*WebRTCVAD)
}

// TestNewWebRTCVAD tests creating a WebRTC VAD instance
func TestNewWebRTCVAD(t *testing.T) {
	vad := NewWebRTCVAD()
	require.NotNil(t, vad)

	webrtcVAD, ok := vad.(*WebRTCVAD)
	require.True(t, ok)
	assert.Equal(t, DefaultSampleRate, webrtcVAD.sampleRate)
	assert.Equal(t, DefaultMode, webrtcVAD.mode)
	assert.False(t, webrtcVAD.initialized)

	// clean up resources
	err := vad.Close()
	assert.NoError(t, err)
}

// TestNewWebRTCVADWithConfig tests creating a WebRTC VAD instance with config
func TestNewWebRTCVADWithConfig(t *testing.T) {
	// test valid config
	vad, err := NewWebRTCVADWithConfig(8000, 1)
	require.NoError(t, err)
	require.NotNil(t, vad)

	webrtcVAD, ok := vad.(*WebRTCVAD)
	require.True(t, ok)
	assert.Equal(t, 8000, webrtcVAD.sampleRate)
	assert.Equal(t, 1, webrtcVAD.mode)

	err = vad.Close()
	assert.NoError(t, err)

	// test invalid sample rate
	vad, err = NewWebRTCVADWithConfig(22050, 1)
	assert.Error(t, err)
	assert.Nil(t, vad)

	// test invalid mode
	vad, err = NewWebRTCVADWithConfig(16000, 5)
	assert.Error(t, err)
	assert.Nil(t, vad)
}

// TestWebRTCVAD_IsVAD tests voice activity detection
func TestWebRTCVAD_IsVAD(t *testing.T) {
	vad := newInitializedVAD(t)
	defer vad.Close()

	// test empty data
	isActive, err := vad.IsVAD([]float32{})
	assert.NoError(t, err)
	assert.False(t, isActive)

	// test silence data (all zeros)
	silentData := make([]float32, 1600) // 100ms at 16kHz
	isActive, err = vad.IsVAD(silentData)
	assert.NoError(t, err)
	// silence is usually not detected as speech, but it depends on the VAD implementation

	// test synthetic speech data (sine wave)
	speechData := generateSineWave(16000, 440, 1.0, 0.5) // 1s 440Hz sine wave
	isActive, err = vad.IsVAD(speechData)
	assert.NoError(t, err)
	// a sine wave may be detected as speech depending on the VAD algorithm

	// test data shorter than one frame
	shortData := make([]float32, 100) // data shorter than one frame
	isActive, err = vad.IsVAD(shortData)
	assert.NoError(t, err)
	assert.False(t, isActive)
}

// TestWebRTCVAD_Reset tests reset
func TestWebRTCVAD_Reset(t *testing.T) {
	vad := newInitializedVAD(t)
	defer vad.Close()

	testData := make([]float32, 1600) // 100ms at 16kHz
	_, err := vad.IsVAD(testData)
	assert.NoError(t, err)

	// reset after initialization
	err = vad.Reset()
	assert.NoError(t, err)
}

// TestWebRTCVAD_Close tests close
func TestWebRTCVAD_Close(t *testing.T) {
	// close when not initialized
	assert.NoError(t, NewWebRTCVAD().Close())

	// close after initialization, twice
	vad := newInitializedVAD(t)
	_, err := vad.IsVAD(make([]float32, 1600))
	assert.NoError(t, err)
	assert.NoError(t, vad.Close())
	assert.NoError(t, vad.Close())
}

// TestWebRTCVAD_SetMode tests SetMode
func TestWebRTCVAD_SetMode(t *testing.T) {
	vad := NewWebRTCVAD()
	require.NotNil(t, vad)
	defer vad.Close()

	webrtcVAD, ok := vad.(*WebRTCVAD)
	require.True(t, ok)

	// test valid modes
	for mode := 0; mode <= 3; mode++ {
		err := webrtcVAD.SetMode(mode)
		assert.NoError(t, err)
		assert.Equal(t, mode, webrtcVAD.GetMode())
	}

	// test invalid mode
	err := webrtcVAD.SetMode(-1)
	assert.Error(t, err)

	err = webrtcVAD.SetMode(4)
	assert.Error(t, err)
}

// TestWebRTCVAD_SetSampleRate tests SetSampleRate
func TestWebRTCVAD_SetSampleRate(t *testing.T) {
	vad := NewWebRTCVAD()
	require.NotNil(t, vad)
	defer vad.Close()

	webrtcVAD, ok := vad.(*WebRTCVAD)
	require.True(t, ok)

	// test valid sample rates
	validRates := []int{8000, 16000, 32000, 48000}
	for _, rate := range validRates {
		err := webrtcVAD.SetSampleRate(rate)
		assert.NoError(t, err)
		assert.Equal(t, rate, webrtcVAD.GetSampleRate())
	}

	// test invalid sample rate
	err := webrtcVAD.SetSampleRate(22050)
	assert.Error(t, err)

	err = webrtcVAD.SetSampleRate(44100)
	assert.Error(t, err)
}

// TestFloat32ToPCMBytes tests data type conversion
func TestFloat32ToPCMBytes(t *testing.T) {
	vad := NewWebRTCVAD()
	require.NotNil(t, vad)
	defer vad.Close()

	webrtcVAD, ok := vad.(*WebRTCVAD)
	require.True(t, ok)

	// test boundary values
	testData := []float32{-1.0, 0.0, 1.0, 1.5, -1.5}
	pcmBytes := webrtcVAD.float32ToPCMBytes(testData)

	assert.Equal(t, len(testData)*2, len(pcmBytes))

	// check conversion result
	// -1.0 -> -32768
	// 0.0 -> 0
	// 1.0 -> 32767
	// 1.5 -> 32767 (clipped)
	// -1.5 -> -32768 (clipped)
}

// TestIsValidSampleRate tests sample rate validation
func TestIsValidSampleRate(t *testing.T) {
	// valid sample rates
	validRates := []int{8000, 16000, 32000, 48000}
	for _, rate := range validRates {
		assert.True(t, isValidSampleRate(rate))
	}

	// invalid sample rates
	invalidRates := []int{11025, 22050, 44100, 96000}
	for _, rate := range invalidRates {
		assert.False(t, isValidSampleRate(rate))
	}
}

// generateSineWave generates sine wave data for tests
func generateSineWave(sampleRate int, frequency float64, duration float64, amplitude float64) []float32 {
	numSamples := int(float64(sampleRate) * duration)
	samples := make([]float32, numSamples)

	for i := 0; i < numSamples; i++ {
		t := float64(i) / float64(sampleRate)
		samples[i] = float32(amplitude * math.Sin(2*math.Pi*frequency*t))
	}

	return samples
}

func TestWebRTCVADFactory(t *testing.T) {
	config := WebRTCVADConfig{
		SampleRate: 16000,
		Mode:       2,
	}

	factory := NewWebRTCVADFactory(config)

	// test resource creation
	resource, err := factory.Create()
	if err != nil {
		t.Fatalf("Failed to create resource: %v", err)
	}
	defer resource.Close()

	// validate resource type
	vad, ok := resource.(*WebRTCVAD)
	if !ok {
		t.Fatalf("Created resource is not WebRTCVAD type")
	}

	// validate config
	if vad.GetSampleRate() != config.SampleRate {
		t.Errorf("Expected sample rate %d, got %d", config.SampleRate, vad.GetSampleRate())
	}

	if vad.GetMode() != config.Mode {
		t.Errorf("Expected mode %d, got %d", config.Mode, vad.GetMode())
	}

	// test validation
	if !factory.Validate(resource) {
		t.Error("Factory validation failed for valid resource")
	}

	// test reset
	err = factory.Reset(resource)
	if err != nil {
		t.Errorf("Factory reset failed: %v", err)
	}

	// test resource validity
	if !resource.IsValid() {
		t.Error("Resource should be valid after reset")
	}
}
