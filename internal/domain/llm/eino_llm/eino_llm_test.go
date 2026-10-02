package eino_llm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewEinoLLMProvider(t *testing.T) {
	tests := []struct {
		name      string
		config    map[string]interface{}
		expectErr bool
	}{
		{
			name: "valid openai config",
			config: map[string]interface{}{
				"type":       "openai",
				"model_name": "gpt-3.5-turbo",
				"api_key":    "test-key",
				"base_url":   "https://api.openai.com/v1",
				"max_tokens": 500,
			},
			expectErr: false,
		},
		{
			name: "valid ollama config",
			config: map[string]interface{}{
				"type":       "ollama",
				"model_name": "llama2",
				"base_url":   "http://localhost:11434",
				"max_tokens": 500,
			},
			expectErr: false,
		},
		{
			name: "missing type",
			config: map[string]interface{}{
				"model_name": "gpt-3.5-turbo",
				"api_key":    "test-key",
			},
			expectErr: true,
		},
		{
			name: "missing model_name",
			config: map[string]interface{}{
				"type":    "openai",
				"api_key": "test-key",
			},
			expectErr: true,
		},
		{
			name: "with streamable config",
			config: map[string]interface{}{
				"type":       "openai",
				"model_name": "gpt-4",
				"api_key":    "test-key",
				"streamable": false,
			},
			expectErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, err := NewEinoLLMProvider(tt.config)

			if tt.expectErr {
				assert.Error(t, err)
				assert.Nil(t, provider)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, provider)
				assert.Equal(t, tt.config["model_name"], provider.modelName)
				assert.NotNil(t, provider.chatModel)
				if tt.config["type"] != nil {
					assert.Equal(t, tt.config["type"], provider.providerType)
				}
			}
		})
	}
}

func TestEinoLLMProvider_GetModelInfo(t *testing.T) {
	config := map[string]interface{}{
		"type":       "openai",
		"model_name": "gpt-3.5-turbo",
		"api_key":    "test-key",
		"max_tokens": 1000,
	}

	provider, err := NewEinoLLMProvider(config)
	require.NoError(t, err)

	info := provider.GetModelInfo()

	assert.Equal(t, "eino", info["framework"])
	assert.Equal(t, "eino", info["type"])
	assert.Equal(t, "openai", info["provider_type"])
	assert.Equal(t, "3.0.0", info["adapter_version"])
	assert.Equal(t, true, info["streamable"])
	assert.Contains(t, info, "model_name")
}

func TestEinoLLMProvider_WithMaxTokens(t *testing.T) {
	config := map[string]interface{}{
		"type":       "openai",
		"model_name": "gpt-3.5-turbo",
		"api_key":    "test-key",
		"max_tokens": 500,
	}

	provider, err := NewEinoLLMProvider(config)
	require.NoError(t, err)

	// Test method chaining
	newProvider := provider.WithMaxTokens(1000)

	assert.NotEqual(t, provider, newProvider)    // Should be a different instance
	assert.Equal(t, 500, provider.maxTokens)     // Original instance unchanged
	assert.Equal(t, 1000, newProvider.maxTokens) // New instance updated
}

func TestEinoLLMProvider_WithStreamable(t *testing.T) {
	config := map[string]interface{}{
		"type":       "openai",
		"model_name": "gpt-3.5-turbo",
		"api_key":    "test-key",
		"streamable": true,
	}

	provider, err := NewEinoLLMProvider(config)
	require.NoError(t, err)

	// Test method chaining
	newProvider := provider.WithStreamable(false)

	assert.NotEqual(t, provider, newProvider)      // Should be a different instance
	assert.Equal(t, true, provider.streamable)     // Original instance unchanged
	assert.Equal(t, false, newProvider.streamable) // New instance updated
}

func TestEinoLLMProvider_GetChatModel(t *testing.T) {
	config := map[string]interface{}{
		"type":       "openai",
		"model_name": "gpt-3.5-turbo",
		"api_key":    "test-key",
	}

	provider, err := NewEinoLLMProvider(config)
	require.NoError(t, err)

	chatModel := provider.GetChatModel()
	assert.NotNil(t, chatModel)
	assert.Equal(t, provider.chatModel, chatModel)
}

func TestEinoLLMProvider_GetProviderType(t *testing.T) {
	config := map[string]interface{}{
		"type":       "ollama",
		"model_name": "llama2",
		"base_url":   "http://localhost:11434",
	}

	provider, err := NewEinoLLMProvider(config)
	require.NoError(t, err)

	providerType := provider.GetProviderType()
	assert.Equal(t, "ollama", providerType)
}

func TestEinoConfig_Structure(t *testing.T) {
	// Test config struct
	config := EinoConfig{
		Type:       "openai",
		ModelName:  "gpt-4",
		APIKey:     "test-key",
		BaseURL:    "https://api.openai.com/v1",
		MaxTokens:  1000,
		Streamable: true,
		Parameters: map[string]interface{}{
			"temperature": 0.7,
		},
	}

	assert.Equal(t, "openai", config.Type)
	assert.Equal(t, "gpt-4", config.ModelName)
	assert.Equal(t, "test-key", config.APIKey)
	assert.Equal(t, true, config.Streamable)
	assert.Contains(t, config.Parameters, "temperature")
}

// BenchmarkEinoLLMProvider_WithMaxTokens method-chaining benchmark
func BenchmarkEinoLLMProvider_WithMaxTokens(b *testing.B) {
	config := map[string]interface{}{
		"type":       "openai", // Use openai type
		"model_name": "gpt-3.5-turbo",
		"api_key":    "test-key",
	}

	provider, _ := NewEinoLLMProvider(config)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		provider.WithMaxTokens(1000 + i)
	}
}

// TestExampleConfig test example config
func TestExampleConfig(t *testing.T) {
	assert.Equal(t, "eino_llm", ExampleConfig["type"])
	assert.Equal(t, "gpt-3.5-turbo", ExampleConfig["model_name"])
	assert.Equal(t, 500, ExampleConfig["max_tokens"])
	assert.Equal(t, true, ExampleConfig["streamable"])
}

// TestEinoLLMProvider_FullWorkflow full workflow test (structure only; no real API)
func TestEinoLLMProvider_FullWorkflow(t *testing.T) {
	config := map[string]interface{}{
		"type":       "openai", // Use openai type
		"model_name": "gpt-3.5-turbo",
		"api_key":    "test-key",
		"max_tokens": 500,
		"streamable": true,
	}

	// 1. Create provider
	provider, err := NewEinoLLMProvider(config)
	require.NoError(t, err)
	assert.NotNil(t, provider)

	// 2. Test config chaining
	enhancedProvider := provider.WithMaxTokens(1000).WithStreamable(false)
	assert.Equal(t, 1000, enhancedProvider.maxTokens)
	assert.Equal(t, false, enhancedProvider.streamable)

	// 3. Test model info
	info := enhancedProvider.GetModelInfo()
	assert.Equal(t, "eino", info["framework"])
	assert.Equal(t, "eino", info["type"])
	assert.Equal(t, "openai", info["provider_type"])

	// 4. Test underlying ChatModel access
	chatModel := enhancedProvider.GetChatModel()
	assert.NotNil(t, chatModel)

	// 5. Test provider type
	providerType := enhancedProvider.GetProviderType()
	assert.Equal(t, "openai", providerType)
}

// TestMultipleProviderTypes test multiple provider types
func TestMultipleProviderTypes(t *testing.T) {
	testCases := []struct {
		name         string
		providerType string
		modelName    string
	}{
		{
			name:         "OpenAI Provider",
			providerType: "openai",
			modelName:    "gpt-3.5-turbo",
		},
		{
			name:         "Ollama Provider",
			providerType: "ollama",
			modelName:    "llama2",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			config := map[string]interface{}{
				"type":       tc.providerType,
				"model_name": tc.modelName,
				"api_key":    "test-key",
			}

			if tc.providerType == "ollama" {
				config["base_url"] = "http://localhost:11434"
			}

			provider, err := NewEinoLLMProvider(config)
			require.NoError(t, err)
			assert.NotNil(t, provider)
			assert.Equal(t, tc.providerType, provider.GetProviderType())
			assert.Equal(t, tc.modelName, provider.modelName)
		})
	}
}
