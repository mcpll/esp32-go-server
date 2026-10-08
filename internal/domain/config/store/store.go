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

var (
	mu      sync.RWMutex
	applied map[string]any // last settings snapshot written into viper
)

// Merge writes system config into the shared viper. Readers must go through this
// package so they do not race the write. Secret leaves in data are ignored.
func Merge(data map[string]any) error {
	return MergeThen(data, nil)
}

// MergeThen applies data, then runs then while the write lock is still held.
// Keys written by the previous snapshot and absent from data are removed.
// Keys never present in a snapshot (file secrets, VAD model config) stay.
// An empty string is absent too: an empty seed never overrides the config file.
// then may call viper.Set (secrets.ApplyEnv does) and must not call back into this package.
func MergeThen(data map[string]any, then func()) error {
	mu.Lock()
	defer mu.Unlock()
	cleaned := stripSecrets(cloneMap(data))
	dropEmptyStrings(cleaned)
	for _, key := range unionKeys(applied, cleaned) {
		current := cloneStringMap(viper.Get(key))
		if prev, ok := applied[key].(map[string]any); ok {
			removeApplied(current, prev)
		}
		if next, ok := cleaned[key].(map[string]any); ok {
			deepMerge(current, next)
		}
		viper.Set(key, current)
	}
	applied = cleaned
	if then != nil {
		then()
	}
	return nil
}

func unionKeys(a, b map[string]any) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	var keys []string
	for k := range a {
		seen[k] = struct{}{}
		keys = append(keys, k)
	}
	for k := range b {
		if _, ok := seen[k]; ok {
			continue
		}
		keys = append(keys, k)
	}
	return keys
}

func removeApplied(dst, prev map[string]any) {
	for k, v := range prev {
		if child, ok := v.(map[string]any); ok {
			sub, ok := dst[k].(map[string]any)
			if !ok {
				delete(dst, k)
				continue
			}
			removeApplied(sub, child)
			if len(sub) == 0 {
				delete(dst, k)
			}
			continue
		}
		delete(dst, k)
	}
}

func deepMerge(dst, src map[string]any) {
	for k, v := range src {
		if srcChild, ok := v.(map[string]any); ok {
			dstChild, ok := dst[k].(map[string]any)
			if !ok {
				dst[k] = cloneMap(srcChild)
				continue
			}
			deepMerge(dstChild, srcChild)
			continue
		}
		dst[k] = cloneValue(v)
	}
}

func cloneStringMap(v any) map[string]any {
	m, ok := v.(map[string]any)
	if !ok || m == nil {
		return map[string]any{}
	}
	return cloneMap(m)
}

func stripSecrets(m map[string]any) map[string]any {
	return stripAt(m, "")
}

// dropEmptyStrings removes "" leaves so they are not merged and not remembered
// in the snapshot. An empty seed never overrides the config file.
func dropEmptyStrings(m map[string]any) {
	for k, v := range m {
		switch child := v.(type) {
		case string:
			if child == "" {
				delete(m, k)
			}
		case map[string]any:
			dropEmptyStrings(child)
		}
	}
}

func stripAt(m map[string]any, prefix string) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	for k, v := range m {
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}
		if dropSecret(path, k) {
			delete(m, k)
			continue
		}
		switch child := v.(type) {
		case map[string]any:
			stripAt(child, path)
		case []any:
			for _, item := range child {
				if nested, ok := item.(map[string]any); ok {
					stripAt(nested, path)
				}
			}
		}
	}
	return m
}

// mqtt.password is the external broker client password and lives in settings.
// mqtt_server.password is the embedded broker secret and stays in env / the file.
func dropSecret(path, key string) bool {
	if strings.EqualFold(path, "mqtt.password") {
		return false
	}
	_, drop := secretLeaves[strings.ToLower(key)]
	return drop
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
