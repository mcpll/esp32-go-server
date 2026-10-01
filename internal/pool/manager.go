package pool

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"xiaozhi-esp32-server-golang/internal/domain/asr"
	"xiaozhi-esp32-server-golang/internal/domain/llm"
	"xiaozhi-esp32-server-golang/internal/domain/tts"
	"xiaozhi-esp32-server-golang/internal/domain/vad"
	vad_inter "xiaozhi-esp32-server-golang/internal/domain/vad/inter"
	"xiaozhi-esp32-server-golang/internal/util"
	log "xiaozhi-esp32-server-golang/logger"

	"github.com/mitchellh/hashstructure/v2"
	"github.com/spf13/viper"
)

var (
	globalManager *UniversalResourcePoolManager
	once          sync.Once
)

// UniversalResourcePoolManager universal resource pool manager
type UniversalResourcePoolManager struct {
	pools        map[string]*util.ResourcePool // key format: "resourceType:provider"
	creators     map[string]interface{}        // registered creator functions
	closeFuncs   map[string]func(interface{}) error
	isValidFuncs map[string]func(interface{}) bool
	resetFuncs   map[string]func(interface{}) error
	mu           sync.RWMutex
}

// GetGlobalResourcePoolManager returns the global resource pool manager (singleton)
func GetGlobalResourcePoolManager() *UniversalResourcePoolManager {
	once.Do(func() {
		globalManager = &UniversalResourcePoolManager{
			pools:        make(map[string]*util.ResourcePool),
			creators:     make(map[string]interface{}),
			closeFuncs:   make(map[string]func(interface{}) error),
			isValidFuncs: make(map[string]func(interface{}) bool),
			resetFuncs:   make(map[string]func(interface{}) error),
		}
		log.Info("generic resource pool manager initialized")
	})
	return globalManager
}

// ResourceTypeOption resource type registration option
type ResourceTypeOption func(*ResourceTypeConfig)

// ResourceTypeConfig resource type config
type ResourceTypeConfig struct {
	CloseFunc   func(interface{}) error
	IsValidFunc func(interface{}) bool
	ResetFunc   func(interface{}) error
}

// WithCloseFunc sets the close function
func WithCloseFunc(fn func(interface{}) error) ResourceTypeOption {
	return func(c *ResourceTypeConfig) {
		c.CloseFunc = fn
	}
}

// WithIsValidFunc sets the validation function
func WithIsValidFunc(fn func(interface{}) bool) ResourceTypeOption {
	return func(c *ResourceTypeConfig) {
		c.IsValidFunc = fn
	}
}

// WithResetFunc sets the reset function
func WithResetFunc(fn func(interface{}) error) ResourceTypeOption {
	return func(c *ResourceTypeConfig) {
		c.ResetFunc = fn
	}
}

// RegisterResourceType registers a resource type (called externally)
// resourceType: type name (e.g. "vad", "asr", "custom_type")
// creator: resource factory function
// opts: optional config (closeFunc, isValidFunc, resetFunc)
func RegisterResourceType[T any](
	resourceType string,
	creator CreatorFunc[T],
	opts ...ResourceTypeOption,
) error {
	mgr := GetGlobalResourcePoolManager()
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	// check whether already registered
	if _, exists := mgr.creators[resourceType]; exists {
		return fmt.Errorf("资源类型 %s 已注册", resourceType)
	}

	// register the creator
	mgr.creators[resourceType] = creator

	// apply options
	config := &ResourceTypeConfig{}
	for _, opt := range opts {
		opt(config)
	}

	if config.CloseFunc != nil {
		mgr.closeFuncs[resourceType] = config.CloseFunc
	}
	if config.IsValidFunc != nil {
		mgr.isValidFuncs[resourceType] = config.IsValidFunc
	}
	if config.ResetFunc != nil {
		mgr.resetFuncs[resourceType] = config.ResetFunc
	}

	log.Infof("registered resource type: %s", resourceType)
	return nil
}

// GenerateConfigKey builds a config key (to distinguish pools with different configs)
// use hashstructure for a map-key-order-independent fingerprint so equivalent configs share a pool.
func GenerateConfigKey(provider string, config map[string]interface{}) string {
	input := map[string]interface{}{"provider": provider, "config": config}
	h, err := hashstructure.Hash(input, hashstructure.FormatV2, nil)
	if err != nil {
		log.Warnf("failed to compute config fingerprint, using provider as key: %v", err)
		return provider
	}
	return fmt.Sprintf("%016x", h)
}

// getOrCreatePool gets or creates a resource pool (generic)
// use the config fingerprint as poolKey so host/config changes get a new pool and instances.
func getOrCreatePool[T any](
	resourceType, provider string,
	config map[string]interface{},
) (*util.ResourcePool, error) {
	mgr := GetGlobalResourcePoolManager()
	// pool key format: type:configFingerprint (MD5 of provider+config)
	configKey := GenerateConfigKey(provider, config)
	poolKey := fmt.Sprintf("%s:%s", resourceType, configKey)

	mgr.mu.RLock()
	pool, exists := mgr.pools[poolKey]
	mgr.mu.RUnlock()

	if exists {
		return pool, nil
	}

	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	// double-check
	if pool, exists := mgr.pools[poolKey]; exists {
		return pool, nil
	}

	// get the registered creator
	creatorInterface, exists := mgr.creators[resourceType]
	if !exists {
		return nil, fmt.Errorf("资源类型未注册: %s (请先调用 RegisterResourceType 注册)", resourceType)
	}

	// type-assert to the generic creator
	creator, ok := creatorInterface.(CreatorFunc[T])
	if !ok {
		return nil, fmt.Errorf("资源类型 %s 的 creator 类型不匹配", resourceType)
	}

	// create the generic resource factory
	factory := &ResourceFactory[T]{
		resourceType: resourceType,
		provider:     provider,
		config:       config,
		configKey:    configKey,
		creator:      creator,
		closeFunc: func(p T) error {
			if closeFunc := mgr.closeFuncs[resourceType]; closeFunc != nil {
				return closeFunc(any(p))
			}
			return nil
		},
		isValidFunc: func(p T) bool {
			if isValidFunc := mgr.isValidFuncs[resourceType]; isValidFunc != nil {
				return isValidFunc(any(p))
			}
			return true
		},
		resetFunc: func(p T) error {
			if resetFunc := mgr.resetFuncs[resourceType]; resetFunc != nil {
				return resetFunc(any(p))
			}
			return nil
		},
	}

	// get pool config (defaults shared by all resource types)
	poolConfig := getPoolConfig()

	// create the resource pool
	pool, err := util.NewResourcePool(poolConfig, factory)
	if err != nil {
		return nil, fmt.Errorf("创建资源池失败 [%s:%s]: %w", resourceType, configKey, err)
	}

	mgr.pools[poolKey] = pool
	fpShort := configKey
	if len(configKey) > 8 {
		fpShort = configKey[:8] + "..."
	}
	log.Infof("created resource pool: type=%s, provider=%s, fingerprint=%s", resourceType, provider, fpShort)

	return pool, nil
}

// Acquire acquires a resource (generic, type-safe, lazy)
// T: resource type
// resourceType: type string (vad/asr/llm/tts, etc.)
// provider: provider name
// config: configuration
func Acquire[T any](
	resourceType, provider string,
	config map[string]interface{},
) (*ResourceWrapper[T], error) {
	pool, err := getOrCreatePool[T](resourceType, provider, config)
	if err != nil {
		return nil, err
	}

	resource, err := pool.Acquire()
	if err != nil {
		return nil, fmt.Errorf("获取资源失败 [%s:%s]: %w", resourceType, provider, err)
	}

	wrapper, ok := resource.(*ResourceWrapper[T])
	if !ok {
		pool.Release(resource)
		return nil, fmt.Errorf("资源类型错误: 期望 ResourceWrapper[%T]", *new(T))
	}

	return wrapper, nil
}

// Release releases a resource (generic, type-safe)
func Release[T any](wrapper *ResourceWrapper[T]) error {
	if wrapper == nil {
		return nil
	}

	mgr := GetGlobalResourcePoolManager()
	// all pool keys use type:provider
	poolKey := fmt.Sprintf("%s:%s", wrapper.resourceType, wrapper.configKey)

	mgr.mu.RLock()
	pool, exists := mgr.pools[poolKey]
	mgr.mu.RUnlock()

	if !exists {
		log.Warnf("resource pool not found: %s", poolKey)
		return nil
	}

	return pool.Release(wrapper)
}

// GetStats returns stats for all resource pools
func GetStats() map[string]interface{} {
	mgr := GetGlobalResourcePoolManager()
	mgr.mu.RLock()
	defer mgr.mu.RUnlock()

	stats := make(map[string]interface{})

	for poolKey, pool := range mgr.pools {
		stats[poolKey] = pool.Stats()
	}

	return stats
}

// StartStatsMonitor starts pool stats monitoring; logs stats every interval
func StartStatsMonitor(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				log.Debugf("resource pool stats monitor stopped")
				return
			case <-ticker.C:
				stats := GetStats()
				if len(stats) > 0 {
					statsJSON, err := json.MarshalIndent(stats, "", "  ")
					if err != nil {
						log.Errorf("failed to serialize resource pool stats: %v", err)
						continue
					}
					log.Infof("========== global resource pool stats ==========")
					log.Infof("stats time: %s", time.Now().Format("2006-01-02 15:04:05"))
					log.Infof("resource pool count: %d", len(stats))
					log.Infof("details:\n%s", string(statsJSON))
					log.Infof("========================================")
				} else {
					log.Infof("========== global resource pool stats ==========")
					log.Infof("stats time: %s", time.Now().Format("2006-01-02 15:04:05"))
					log.Infof("no active resource pools")
					log.Infof("========================================")
				}
			}
		}
	}()
	log.Infof("resource pool stats monitor started, logging every %v", interval)
}

// Close closes all resource pools
func Close() error {
	mgr := GetGlobalResourcePoolManager()
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	var errs []error

	for poolKey, pool := range mgr.pools {
		if err := pool.Close(); err != nil {
			errs = append(errs, fmt.Errorf("关闭资源池 %s 失败: %w", poolKey, err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("关闭资源池时发生错误: %v", errs)
	}

	return nil
}

// getPoolConfig loads pool config from settings (defaults shared by all types)
func getPoolConfig() *util.PoolConfig {
	// use default config
	config := util.DefaultConfig()

	// if resource_pools is set, override the defaults
	if viper.IsSet("resource_pools.max_size") {
		config.MaxSize = viper.GetInt("resource_pools.max_size")
	}
	if viper.IsSet("resource_pools.min_size") {
		config.MinSize = viper.GetInt("resource_pools.min_size")
	}
	if viper.IsSet("resource_pools.max_idle") {
		config.MaxIdle = viper.GetInt("resource_pools.max_idle")
	}
	if viper.IsSet("resource_pools.acquire_timeout") {
		config.AcquireTimeout = viper.GetDuration("resource_pools.acquire_timeout")
	}
	if viper.IsSet("resource_pools.idle_timeout") {
		config.IdleTimeout = viper.GetDuration("resource_pools.idle_timeout")
	}
	if viper.IsSet("resource_pools.validate_on_borrow") {
		config.ValidateOnBorrow = viper.GetBool("resource_pools.validate_on_borrow")
	}
	if viper.IsSet("resource_pools.validate_on_return") {
		config.ValidateOnReturn = viper.GetBool("resource_pools.validate_on_return")
	}

	return config
}

// init registers built-in resource types
func init() {
	// register VAD resource type
	RegisterResourceType[vad_inter.VAD](
		"vad",
		func(rt, p string, cfg map[string]interface{}) (vad_inter.VAD, error) {
			vadProvider, err := vad.AcquireVAD(p, cfg)
			if err != nil {
				return nil, err
			}
			if vadProvider != nil {
				vadProvider.Reset()
			}
			return vadProvider, nil
		},
		WithCloseFunc(func(p interface{}) error {
			if vadProvider, ok := p.(vad_inter.VAD); ok && vadProvider != nil {
				return vadProvider.Close()
			}
			return nil
		}),
		WithIsValidFunc(func(p interface{}) bool {
			if vadProvider, ok := p.(vad_inter.VAD); ok && vadProvider != nil {
				return vadProvider.IsValid()
			}
			return false
		}),
		WithResetFunc(func(p interface{}) error {
			if vadProvider, ok := p.(vad_inter.VAD); ok && vadProvider != nil {
				return vadProvider.Reset()
			}
			return nil
		}),
	)

	// register ASR resource type
	RegisterResourceType[asr.AsrProvider](
		"asr",
		func(rt, p string, cfg map[string]interface{}) (asr.AsrProvider, error) {
			return asr.NewAsrProvider(p, cfg)
		},
		WithIsValidFunc(func(p interface{}) bool {
			if asrProvider, ok := p.(asr.AsrProvider); ok && asrProvider != nil {
				return asrProvider.IsValid()
			}
			return false
		}),
		WithCloseFunc(func(p interface{}) error {
			if asrProvider, ok := p.(asr.AsrProvider); ok && asrProvider != nil {
				return asrProvider.Close()
			}
			return nil
		}),
	)

	// register LLM resource type
	RegisterResourceType[llm.LLMProvider](
		"llm",
		func(rt, p string, cfg map[string]interface{}) (llm.LLMProvider, error) {
			providerName, ok := cfg["provider"].(string)
			if !ok || providerName == "" {
				providerName = p
			}
			return llm.GetLLMProvider(providerName, cfg)
		},
		WithIsValidFunc(func(p interface{}) bool {
			if llmProvider, ok := p.(llm.LLMProvider); ok && llmProvider != nil {
				return llmProvider.IsValid()
			}
			return false
		}),
		WithCloseFunc(func(p interface{}) error {
			if llmProvider, ok := p.(llm.LLMProvider); ok && llmProvider != nil {
				return llmProvider.Close()
			}
			return nil
		}),
	)

	// register TTS resource type
	RegisterResourceType[tts.TTSProvider](
		"tts",
		func(rt, p string, cfg map[string]interface{}) (tts.TTSProvider, error) {
			return tts.GetTTSProvider(p, cfg)
		},
		WithIsValidFunc(func(p interface{}) bool {
			if ttsProvider, ok := p.(tts.TTSProvider); ok && ttsProvider != nil {
				return ttsProvider.IsValid()
			}
			return false
		}),
		WithCloseFunc(func(p interface{}) error {
			if ttsProvider, ok := p.(tts.TTSProvider); ok && ttsProvider != nil {
				return ttsProvider.Close()
			}
			return nil
		}),
	)
}
