package streaming

// SentenceSignalType is the sentence-level control signal type sent before an audio segment.
type SentenceSignalType string

const (
	SentenceSignalStart SentenceSignalType = "sentence_start"
	SentenceSignalEnd   SentenceSignalType = "sentence_end"
)

// SentenceSignal is an ordered sentence-boundary signal bound to the current audio chunk.
type SentenceSignal struct {
	Type SentenceSignalType
	Text string
}

// SynthesisEvent is a dual-stream TTS output segment.
// Audio is the current chunk; SentenceSignals are sentence-boundary signals to send before that chunk.
type SynthesisEvent struct {
	Audio           []byte
	SentenceSignals []SentenceSignal
}
