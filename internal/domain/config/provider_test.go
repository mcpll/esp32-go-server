package user_config

import (
	"context"
	"testing"
)

func TestMemoryProvider(t *testing.T) {
	ctx := context.Background()

	// Create in-memory provider
	config := map[string]interface{}{
		"max_entries": 10,
	}

	provider, err := GetUserConfigProvider("memory", config)
	if err != nil {
		t.Fatalf("创建内存provider失败: %v", err)
	}
	// Note: interface has no Close method, so no call needed

	userID := "test_user_123"

	// No SetUserConfig on interface; only test GetUserConfig
	// Test get config for missing user (expect empty config)
	retrievedConfig, err := provider.GetUserConfig(ctx, userID)
	if err != nil {
		t.Fatalf("获取用户配置失败: %v", err)
	}

	// Verify empty config was returned
	if retrievedConfig.Llm.Provider != "" {
		t.Errorf("期望空配置，但得到了 LLM Provider: %s", retrievedConfig.Llm.Provider)
	}

	// Test system config fetch
	systemConfig, err := provider.GetSystemConfig(ctx)
	if err != nil {
		t.Fatalf("获取系统配置失败: %v", err)
	}
	_ = systemConfig // system config may be empty; that is normal
}

func TestProviderAdapter(t *testing.T) {
	ctx := context.Background()

	// Create in-memory provider
	provider, err := GetUserConfigProvider("memory", map[string]interface{}{
		"max_entries": 5,
	})
	if err != nil {
		t.Fatalf("创建内存provider失败: %v", err)
	}
	// Note: interface has no Close method, so no call needed

	// Test adapter config fetch
	userID := "adapter_test_user"

	// Fetch config via adapter (may be empty)
	adapter := NewUserConfigAdapter(provider)
	retrievedConfig, err := adapter.GetUserConfig(ctx, userID)
	if err != nil {
		t.Fatalf("通过适配器获取配置失败: %v", err)
	}

	// Verify adapter works (got a config struct)
	if retrievedConfig.SystemPrompt == "" {
		t.Logf("适配器获取到空的系统提示，这是正常的")
	} else {
		t.Logf("适配器获取到系统提示: %s", retrievedConfig.SystemPrompt)
	}
}

func TestDefaultConfig(t *testing.T) {
	// Test Redis default config
	redisConfig := DefaultConfig("redis")
	if redisConfig["host"] != "localhost" {
		t.Errorf("Redis默认host配置错误，期望: localhost, 实际: %v", redisConfig["host"])
	}

	// Test Memory default config
	memoryConfig := DefaultConfig("memory")
	if memoryConfig["max_entries"] != 1000 {
		t.Errorf("Memory默认max_entries配置错误，期望: 1000, 实际: %v", memoryConfig["max_entries"])
	}

	// Test unsupported type
	unknownConfig := DefaultConfig("unknown")
	if len(unknownConfig) != 0 {
		t.Errorf("未知类型应返回空配置，实际: %v", unknownConfig)
	}
}

func TestValidateConfig(t *testing.T) {
	// Test valid Redis config
	validRedisConfig := map[string]interface{}{
		"host": "localhost",
		"port": 6379,
	}
	err := ValidateConfig("redis", validRedisConfig)
	if err != nil {
		t.Errorf("有效Redis配置验证失败: %v", err)
	}

	// Test invalid Redis config (missing host)
	invalidRedisConfig := map[string]interface{}{
		"port": 6379,
	}
	err = ValidateConfig("redis", invalidRedisConfig)
	if err == nil {
		t.Error("缺少host的Redis配置应该验证失败")
	}

	// Test Memory config (no validation needed)
	err = ValidateConfig("memory", map[string]interface{}{})
	if err != nil {
		t.Errorf("Memory配置验证失败: %v", err)
	}
}
