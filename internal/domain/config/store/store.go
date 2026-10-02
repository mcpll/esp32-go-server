package store

import (
	"sync"

	"github.com/spf13/viper"
)

var mu sync.RWMutex

// Merge writes system config into the shared viper. Readers must go through this
// package so they do not race the write.
func Merge(data map[string]any) error {
	mu.Lock()
	defer mu.Unlock()
	return viper.MergeConfigMap(data)
}

func GetString(key string) string {
	mu.RLock()
	defer mu.RUnlock()
	return viper.GetString(key)
}

func GetBool(key string) bool {
	mu.RLock()
	defer mu.RUnlock()
	return viper.GetBool(key)
}

func GetStringMap(key string) map[string]any {
	mu.RLock()
	defer mu.RUnlock()
	return cloneMap(viper.GetStringMap(key))
}

func AllSettings() map[string]any {
	mu.RLock()
	defer mu.RUnlock()
	return cloneMap(viper.AllSettings())
}

func cloneMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = cloneValue(v)
	}
	return out
}

func cloneValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		return cloneMap(t)
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = cloneValue(e)
		}
		return out
	default:
		return t
	}
}
