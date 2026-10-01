package inter

// VAD voice activity detection interface
type VAD interface {
	// IsVAD detect speech activity in audio
	IsVAD(pcmData []float32) (bool, error)

	IsVADExt(pcmData []float32, sampleRate int, frameSize int) (bool, error)
	// Reset reset detector state
	Reset() error
	// Close close and release resources
	Close() error
	// IsValid check whether the resource is valid
	IsValid() bool
}
