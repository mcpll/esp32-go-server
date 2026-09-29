package eventbus

const (
	TopicAddMessage = "add_message"
	TopicSessionEnd = "session_end"
	TopicExitChat   = "exit_chat" // exit-chat event

	// Chat-history events (deprecated; use TopicAddMessage)
	// Deprecated: use TopicAddMessage instead
	TopicChatHistoryUserMessage      = "chat_history_user_message"      // user message (post-ASR) - deprecated
	TopicChatHistoryAssistantMessage = "chat_history_assistant_message" // assistant reply (post-LLM+TTS) - deprecated
)
