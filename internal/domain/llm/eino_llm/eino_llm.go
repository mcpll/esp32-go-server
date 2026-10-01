package eino_llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/cloudwego/eino-ext/components/model/ollama"
	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	log "xiaozhi-esp32-server-golang/logger"
)

// EinoLLMProvider LLM provider built on the Eino framework
// uses Eino ChatModel interfaces/types directly; supports openai and ollama
type EinoLLMProvider struct {
	chatModel        model.ToolCallingChatModel
	modelName        string
	maxTokens        int
	streamable       bool
	config           map[string]interface{}
	providerType     string // "openai" or "ollama"
	reasoningTracker *reasoningContentTracker
}

// EinoConfig Eino LLM config
type EinoConfig struct {
	Type       string                 `json:"type"` // "openai" or "ollama"
	ModelName  string                 `json:"model_name"`
	APIKey     string                 `json:"api_key"`
	BaseURL    string                 `json:"base_url"`
	MaxTokens  int                    `json:"max_tokens"`
	Parameters map[string]interface{} `json:"parameters,omitempty"`
	Streamable bool                   `json:"streamable,omitempty"`
}

// connection pool config
const (
	maxIdleConns          = 200
	maxIdleConnsPerHost   = 50
	idleConnTimeout       = 90 * time.Second
	dialTimeout           = 30 * time.Second
	keepAliveTimeout      = 30 * time.Second
	tlsHandshakeTimeout   = 10 * time.Second
	responseHeaderTimeout = 60 * time.Second
)

// shared HTTP client for all OpenAI requests
var (
	httpClient     *http.Client
	httpClientOnce sync.Once
)

// getHTTPClient returns an HTTP client with a connection pool
func getHTTPClient() *http.Client {
	httpClientOnce.Do(func() {
		transport := &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   dialTimeout,
				KeepAlive: keepAliveTimeout,
			}).DialContext,
			MaxIdleConns:          maxIdleConns,
			MaxIdleConnsPerHost:   maxIdleConnsPerHost,
			IdleConnTimeout:       idleConnTimeout,
			TLSHandshakeTimeout:   tlsHandshakeTimeout,
			ResponseHeaderTimeout: responseHeaderTimeout,
			ExpectContinueTimeout: 1 * time.Second,
			DisableKeepAlives:     false,
		}

		httpClient = &http.Client{
			Transport: transport,
			// for streaming, do not use http.Client.Timeout to cut the whole connection; let ctx own the request lifetime.
			Timeout: 0,
		}
	})

	return httpClient
}

// NewEinoLLMProvider creates an Eino LLM provider; type supports openai and ollama
func NewEinoLLMProvider(config map[string]interface{}) (*EinoLLMProvider, error) {
	//log.Debugf("NewEinoLLMProvider config: %+v", config)
	var tracker *reasoningContentTracker
	if enabled, _ := config[reasoningDetectConfigKey].(bool); enabled {
		tracker = &reasoningContentTracker{}
		config[reasoningTrackerConfigKey] = tracker
	}
	parsedConfig, err := decodeOpenAICompatibleConfig(config)
	if err != nil {
		return nil, fmt.Errorf("解析LLM配置失败: %v", err)
	}

	providerType := parsedConfig.Type
	if providerType == "" {
		return nil, fmt.Errorf("type不能为空，必须是 'openai' 或 'ollama'")
	}

	modelName := parsedConfig.ModelName
	if modelName == "" {
		return nil, fmt.Errorf("model_name不能为空")
	}

	maxTokens := 500
	if parsedConfig.MaxTokens != nil {
		maxTokens = *parsedConfig.MaxTokens
	}

	streamable := true
	if parsedConfig.Streamable != nil {
		streamable = *parsedConfig.Streamable
	}

	var chatModel model.ToolCallingChatModel

	// create the ChatModel implementation for the type
	switch providerType {
	case "openai":
		chatModel, err = createOpenAIChatModel(config)
		if err != nil {
			return nil, fmt.Errorf("创建OpenAI ChatModel失败: %v", err)
		}
	case "ollama":
		chatModel, err = createOllamaChatModel(config)
		if err != nil {
			return nil, fmt.Errorf("创建Ollama ChatModel失败: %v", err)
		}
	default:
		return nil, fmt.Errorf("不支持的模型类型: %s", providerType)
	}

	provider := &EinoLLMProvider{
		chatModel:        chatModel,
		modelName:        modelName,
		maxTokens:        maxTokens,
		streamable:       streamable,
		config:           config,
		providerType:     providerType,
		reasoningTracker: tracker,
	}

	return provider, nil
}

func (p *EinoLLMProvider) HasReasoningContent() bool {
	return p != nil && p.reasoningTracker != nil && p.reasoningTracker.HasReturned()
}

// createOpenAIChatModel creates an OpenAI ChatModel
func createOpenAIChatModel(config map[string]interface{}) (model.ToolCallingChatModel, error) {
	ctx := context.Background()

	parsedConfig, err := decodeOpenAICompatibleConfig(config)
	if err != nil {
		return nil, fmt.Errorf("解析OpenAI兼容配置失败: %v", err)
	}

	modelName := parsedConfig.ModelName
	if modelName == "" {
		modelName = "gpt-3.5-turbo"
	}

	apiKey := parsedConfig.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}

	httpClient := buildThinkingHTTPClient(config, getHTTPClient())
	useMaxCompletionTokens := shouldUseMaxCompletionTokens(parsedConfig.Provider, modelName)

	// build OpenAI ChatModel config
	openaiConfig := &openai.ChatModelConfig{
		Model:      modelName,
		APIKey:     apiKey,
		HTTPClient: httpClient,
	}

	if parsedConfig.BaseURL != "" {
		openaiConfig.BaseURL = parsedConfig.BaseURL
	}
	if parsedConfig.APIVersion != "" {
		openaiConfig.APIVersion = parsedConfig.APIVersion
	}
	if !useMaxCompletionTokens && parsedConfig.MaxTokens != nil && *parsedConfig.MaxTokens > 0 {
		openaiConfig.MaxTokens = parsedConfig.MaxTokens
	}
	if parsedConfig.Temperature != nil {
		openaiConfig.Temperature = parsedConfig.Temperature
	}
	if parsedConfig.TopP != nil {
		openaiConfig.TopP = parsedConfig.TopP
	}

	log.Debugf("openaiConfig: %+v", openaiConfig)

	// use the official eino-ext OpenAI implementation
	chatModel, err := openai.NewChatModel(ctx, openaiConfig)
	if err != nil {
		return nil, fmt.Errorf("创建OpenAI ChatModel失败: %v", err)
	}

	log.Infof("successfully created OpenAI ChatModel, model: %s", modelName)
	return chatModel, nil
}

// createOllamaChatModel creates an Ollama ChatModel
func createOllamaChatModel(config map[string]interface{}) (model.ToolCallingChatModel, error) {
	ctx := context.Background()

	modelName, _ := config["model_name"].(string)
	baseURL, _ := config["base_url"].(string)

	if modelName == "" || baseURL == "" {
		log.Warnf("model_name and base_url cannot be empty, using default model: %s", modelName)
		return nil, fmt.Errorf("model_name和base_url不能为空")
	}

	// build Ollama ChatModel config
	ollamaConfig := &ollama.ChatModelConfig{
		BaseURL: baseURL,
		Model:   modelName,
	}

	// use the official eino-ext Ollama implementation
	chatModel, err := ollama.NewChatModel(ctx, ollamaConfig)
	if err != nil {
		return nil, fmt.Errorf("创建Ollama ChatModel失败: %v", err)
	}

	log.Infof("successfully created Ollama ChatModel, model: %s", modelName)
	return chatModel, nil
}

// GetModelInfo returns model info
func (p *EinoLLMProvider) GetModelInfo() map[string]interface{} {
	return map[string]interface{}{
		"model_name":      p.modelName,
		"max_tokens":      p.maxTokens,
		"streamable":      p.streamable,
		"type":            "eino",
		"provider_type":   p.providerType,
		"framework":       "eino",
		"adapter_version": "3.0.0",
		"base_url":        p.config["base_url"],
	}
}

// ResponseWithFunctions response with function calling; uses Eino native tool types and calls EinoResponseWithTools directly
func (p *EinoLLMProvider) ResponseWithContext(ctx context.Context, sessionID string, dialogue []*schema.Message, functions []*schema.ToolInfo) chan *schema.Message {

	log.Infof("[Eino-LLM] starting tool-enabled request - SessionID: %s, Type: %s", sessionID, p.providerType)

	logMessages(dialogue)
	// call EinoResponseWithTools directly for a native Eino response
	einoResponseChan := p.EinoResponseWithTools(ctx, sessionID, dialogue, functions)

	log.Infof("[Eino-LLM] tool-enabled request completed - SessionID: %s", sessionID)

	return einoResponseChan
}

func logMessages(messages []*schema.Message) {
	for _, msg := range messages {
		if msg == nil {
			log.Debugf("history llm msg: <nil>")
			continue
		}
		log.Debugf("history llm msg: %s\n", msg.String())
	}
}

// llmExtraErrorKey matches domain/llm.LLMExtraErrorKey for forwarding errors on failure (avoids import cycles)
const llmExtraErrorKey = "error"

// sendLLMError sends an error message with Extra.error on the channel
func sendLLMError(ch chan *schema.Message, err error) {
	ch <- &schema.Message{
		Role:  schema.System,
		Extra: map[string]any{llmExtraErrorKey: err.Error()},
	}
}

// EinoResponseWithTools tool-aware response using Eino types directly
func (p *EinoLLMProvider) EinoResponseWithTools(ctx context.Context, sessionID string, messages []*schema.Message, tools []*schema.ToolInfo) chan *schema.Message {
	responseChan := make(chan *schema.Message, 200)

	var err error
	go func() {
		defer close(responseChan)
		if p.reasoningTracker != nil {
			p.reasoningTracker.Reset()
		}

		log.Infof("[Eino-LLM] starting Eino tool request - SessionID: %s, tools: %+v", sessionID, tools)

		// if tools exist, bind them to ChatModel
		if len(tools) > 0 {
			p.chatModel, err = p.chatModel.WithTools(tools)
			if err != nil {
				log.Errorf("failed to bind tools: %v", err)
				sendLLMError(responseChan, err)
				return
			}
		}

		if p.streamable {
			log.Debugf("EinoLLMProvider.EinoResponseWithTools() streamable: %t", p.streamable)
			// call Eino Stream directly
			streamReader, err := p.chatModel.Stream(ctx, messages, p.buildModelCallOptions()...)
			if err != nil {
				log.Errorf("Eino tool stream call failed: %v", err)
				// for mock implementations, if Stream fails fall back to Generate
				message, genErr := p.chatModel.Generate(ctx, messages, p.buildModelCallOptions()...)
				if genErr != nil {
					log.Errorf("Eino tool generate response failed: %v", genErr)
					sendLLMError(responseChan, genErr)
					return
				}
				if message != nil {
					responseChan <- message
				}
				return
			}

			if streamReader != nil {
				defer streamReader.Close()

				var currentToolCall *schema.ToolCall
				var toolCallBuffer string
				var isToolCallComplete bool
				var streamChunkCount int

				// handle streaming response
				for {
					message, err := streamReader.Recv()
					//log.Debugf("streamReader.Recv() message: %+v", message)
					if err == io.EOF {
						if streamChunkCount == 0 {
							sendLLMError(responseChan, errors.New("流式响应为空"))
							break
						}
						// if a tool call is still incomplete, send the last update
						if currentToolCall != nil {
							completeMessage := &schema.Message{
								Role:      schema.Assistant,
								ToolCalls: []schema.ToolCall{*currentToolCall},
							}
							responseChan <- completeMessage
						}
						break
					}
					if err != nil {
						if ctxErr := ctx.Err(); ctxErr != nil {
							if errors.Is(ctxErr, context.Canceled) {
								log.Debugf("stream response canceled: %v", ctxErr)
							} else {
								log.Warnf("stream response ended: %v", ctxErr)
							}
							break
						}
						log.Errorf("failed to receive stream response: %v", err)
						sendLLMError(responseChan, err)
						break
					}

					if message != nil {
						streamChunkCount++
						// check whether a tool call is starting
						if len(message.ToolCalls) > 0 {
							toolCall := message.ToolCalls[0]

							if toolCall.Function.Name != "" {
								// new tool call starting
								currentToolCall = &toolCall
								toolCallBuffer = toolCall.Function.Arguments
								isToolCallComplete = false
							} else if currentToolCall != nil {
								// accumulate tool-call arguments
								toolCallBuffer += toolCall.Function.Arguments
								currentToolCall.Function.Arguments = toolCallBuffer

								// check whether the args are complete JSON
								if isValidJSON(toolCallBuffer) {
									isToolCallComplete = true
								}
							}

							// if the tool call is complete, send the message
							if isToolCallComplete {
								completeMessage := &schema.Message{
									Role:      schema.Assistant,
									ToolCalls: []schema.ToolCall{*currentToolCall},
								}
								responseChan <- completeMessage

								// reset state
								currentToolCall = nil
								toolCallBuffer = ""
								isToolCallComplete = false
							}
						} else if message.Content != "" {
							// send a normal non-tool-call message
							message.ToolCalls = nil
							responseChan <- message
						}
					}
				}
			} else {
				sendLLMError(responseChan, errors.New("流式响应为空"))
			}
		} else {
			// call Eino Generate directly
			message, err := p.chatModel.Generate(ctx, messages, p.buildModelCallOptions()...)
			if err != nil {
				log.Errorf("Eino tool generate response failed: %v", err)
				sendLLMError(responseChan, err)
				return
			}

			if message != nil {
				responseChan <- message
			}
		}

		log.Infof("[Eino-LLM] Eino tool request completed - SessionID: %s", sessionID)
	}()

	return responseChan
}

func (p *EinoLLMProvider) buildModelCallOptions() []model.Option {
	if p == nil || p.maxTokens <= 0 {
		return nil
	}

	provider := ""
	if p.config != nil {
		if rawProvider, ok := p.config["provider"].(string); ok {
			provider = rawProvider
		}
	}

	if shouldUseMaxCompletionTokens(provider, p.modelName) {
		return nil
	}

	return []model.Option{model.WithMaxTokens(p.maxTokens)}
}

// isValidJSON checks whether a string is valid JSON
func isValidJSON(str string) bool {
	var js map[string]interface{}
	return json.Unmarshal([]byte(str), &js) == nil
}

// GetChatModel returns the underlying Eino ChatModel
func (p *EinoLLMProvider) GetChatModel() model.ToolCallingChatModel {
	return p.chatModel
}

// GetProviderType returns the provider type
func (p *EinoLLMProvider) GetProviderType() string {
	return p.providerType
}

// WithMaxTokens sets max tokens
func (p *EinoLLMProvider) WithMaxTokens(maxTokens int) *EinoLLMProvider {
	newProvider := *p
	newProvider.maxTokens = maxTokens
	return &newProvider
}

// WithStreamable sets whether streaming is supported
func (p *EinoLLMProvider) WithStreamable(streamable bool) *EinoLLMProvider {
	newProvider := *p
	newProvider.streamable = streamable
	return &newProvider
}

// Close closes resources (stateless Provider; no-op)
func (p *EinoLLMProvider) Close() error {
	return nil
}

// IsValid checks whether the resource is valid
func (p *EinoLLMProvider) IsValid() bool {
	return p != nil && p.chatModel != nil
}
