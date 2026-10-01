package types

const (
	EmptyReasonNone               = ""
	EmptyReasonNoServerResponse   = "no_server_response"
	EmptyReasonProviderEmptyFinal = "provider_empty_final"

	RetryReasonNone                           = ""
	RetryReasonDoubaoResponseCode45000081     = "doubao_response_code_45000081"
	RetryReasonDoubaoWaitingNextPacketTimeout = "doubao_waiting_next_packet_timeout"
	RetryReasonXunfeiServiceInstanceInvalid   = "xunfei_service_instance_invalid"
	RetryReasonAliyunQwen3ConnectionClosed    = "aliyun_qwen3_connection_closed"
)

// StreamingResult streaming recognition result
type StreamingResult struct {
	Text        string // recognized text
	IsFinal     bool   // whether this is the final result
	Error       error  // error info
	AsrType     string // asr type
	Mode        string // mode
	EmptyReason string // empty-result reason; only when Text is empty, to distinguish upstream empty vs idle
	RetryReason string // recoverable error reason; used only when the current resource must be released and retried
}
