package store

import (
	"strings"
	"sync"

	"github.com/spf13/viper"
)

// secretLeaves are config keys a settings record must not replace.
// Env and the config file set them; ApplyEnv runs again after the merge.
var secretLeaves = map[string]struct{}{
	"api_key":       {},
	"apikey":        {},
	"api_secret":    {},
	"access_token":  {},
	"token":         {},
	"password":      {},
	"signature_key": {},
}

var mu sync.RWMutex

// Merge writes system config into the shared viper. Readers must go through this
// package so they do not race the write. Secret leaves in data are ignored.
func Merge(data map[string]any) error {
	return MergeThen(data, nil)
}

// MergeThen merges data, then runs then while the write lock is still held.
// then may call viper.Set (secrets.ApplyEnv does) and must not call back into this package.
func MergeThen(data map[string]any, then func()) error {
	mu.Lock()
	defer mu.Unlock()
	if err := viper.MergeConfigMap(stripSecrets(cloneMap(data))); err != nil {
		return err
	}
	if then != nil {
		then()
	}
	return nil
}

func stripSecrets(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	for k, v := range m {
		if _, drop := secretLeaves[strings.ToLower(k)]; drop {
			delete(m, k)
			continue
		}
		switch child := v.(type) {
		case map[string]any:
			stripSecrets(child)
		case []any:
			for _, item := range child {
				if nested, ok := item.(map[string]any); ok {
					stripSecrets(nested)
				}
			}
		}
	}
	return m
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
