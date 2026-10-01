package pool

import (
	"xiaozhi-esp32-server-golang/internal/util"
)

// ResourceWrapper generic resource wrapper
// T: concrete resource type (e.g. vad.VAD, asr.AsrProvider)
type ResourceWrapper[T any] struct {
	provider     T             // Actual resource provider (type-safe)
	configKey    string        // Config key identifying the resource pool
	resourceType string        // Resource type (vad/asr/llm/tts, etc.)
	closeFunc    func(T) error // Function to close the resource
	isValidFunc  func(T) bool  // Function to validate the resource
	resetFunc    func(T) error // Function to reset resource state (optional)
}

// Close closes the resource
func (r *ResourceWrapper[T]) Close() error {
	if r.closeFunc != nil {
		return r.closeFunc(r.provider)
	}
	return nil
}

// IsValid checks whether the resource is valid
func (r *ResourceWrapper[T]) IsValid() bool {
	if r.isValidFunc != nil {
		return r.isValidFunc(r.provider)
	}
	var zero T
	return any(r.provider) != any(zero)
}

// GetProvider returns the actual resource provider (type-safe, no type assert)
func (r *ResourceWrapper[T]) GetProvider() T {
	return r.provider
}

// GetConfigKey returns the config key
func (r *ResourceWrapper[T]) GetConfigKey() string {
	return r.configKey
}

// GetResourceType returns the resource type
func (r *ResourceWrapper[T]) GetResourceType() string {
	return r.resourceType
}

// Reset resets resource state
func (r *ResourceWrapper[T]) Reset() error {
	if r.resetFunc != nil {
		return r.resetFunc(r.provider)
	}
	return nil
}

// CreatorFunc generic resource creator function type
// T: resource type
// Args: resourceType, provider, config
// Returns: resource instance (type T) and error
type CreatorFunc[T any] func(resourceType, provider string, config map[string]interface{}) (T, error)

// ResourceFactory generic resource factory
type ResourceFactory[T any] struct {
	resourceType string
	provider     string
	config       map[string]interface{}
	configKey    string
	creator      CreatorFunc[T]
	closeFunc    func(T) error
	isValidFunc  func(T) bool
	resetFunc    func(T) error
}

// Create creates a resource
func (f *ResourceFactory[T]) Create() (util.Resource, error) {
	provider, err := f.creator(f.resourceType, f.provider, f.config)
	if err != nil {
		return nil, err
	}

	return &ResourceWrapper[T]{
		provider:     provider,
		configKey:    f.configKey,
		resourceType: f.resourceType,
		closeFunc:    f.closeFunc,
		isValidFunc:  f.isValidFunc,
		resetFunc:    f.resetFunc,
	}, nil
}

// Validate validates a resource
func (f *ResourceFactory[T]) Validate(resource util.Resource) bool {
	if wrapper, ok := resource.(*ResourceWrapper[T]); ok {
		if f.isValidFunc != nil {
			return f.isValidFunc(wrapper.provider)
		}
		return wrapper.IsValid()
	}
	return resource != nil && resource.IsValid()
}

// Reset resets a resource
func (f *ResourceFactory[T]) Reset(resource util.Resource) error {
	if wrapper, ok := resource.(*ResourceWrapper[T]); ok {
		if wrapper.resetFunc != nil {
			return wrapper.resetFunc(wrapper.provider)
		}
		return wrapper.Reset()
	}
	return nil
}
