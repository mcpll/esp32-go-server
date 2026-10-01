package domain

// message type constants
const (
	MessageTypeHello  = "hello"  // handshake message
	MessageTypeAbort  = "abort"  // abort message
	MessageTypeListen = "listen" // listen message
	MessageTypeIot    = "iot"    // IoT message
)

// server message type constants
const (
	ServerMessageTypeHello = "hello" // handshake message
	ServerMessageTypeStt   = "stt"   // speech to text
	ServerMessageTypeTts   = "tts"   // text to speech
	ServerMessageTypeIot   = "iot"   // IoT message
	ServerMessageTypeLlm   = "llm"   // large language model
	ServerMessageTypeText  = "text"  // text message
)

// message state constants
const (
	MessageStateStart   = "start"   // start state
	MessageStateStop    = "stop"    // stop state
	MessageStateDetect  = "detect"  // detect state
	MessageStateAbort   = "abort"   // abort state
	MessageStateSuccess = "success" // success state
)
