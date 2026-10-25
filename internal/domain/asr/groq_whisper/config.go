package groq_whisper

import (
	"os"
	"time"

	"github.com/spf13/viper"
)

const (
	defaultAPIURL         = "https://api.groq.com/openai/v1/audio/transcriptions"
	defaultModel          = "whisper-large-v3-turbo"
	defaultLanguage       = "it"
	defaultResponseFormat = "json"
	defaultSampleRate     = 16000
	defaultTimeoutSeconds = 30
)

// Config for Groq Whisper transcriptions.
type Config struct {
	APIKey         string
	APIURL         string
	Model          string
	Language       string
	ResponseFormat string
	SampleRate     int
	Temperature    float64
	Timeout        time.Duration
}

// DefaultConfig returns defaults matching config/config.yaml.
func DefaultConfig() Config {
	return Config{
		APIURL:         defaultAPIURL,
		Model:          defaultModel,
		Language:       defaultLanguage,
		ResponseFormat: defaultResponseFormat,
		SampleRate:     defaultSampleRate,
		Temperature:    0,
		Timeout:        time.Duration(defaultTimeoutSeconds) * time.Second,
	}
}

// ConfigFromMap merges a config map (and viper / GROQ_API_KEY) into Config.
func ConfigFromMap(cfg map[string]interface{}) Config {
	conf := DefaultConfig()
	applyViperDefaults(&conf)
	if nested, ok := cfg["groq_whisper"].(map[string]interface{}); ok {
		cfg = nested
	}
	applyMapOverrides(&conf, cfg)
	if conf.APIKey == "" {
		conf.APIKey = os.Getenv("GROQ_API_KEY")
	}
	return conf
}

func applyViperDefaults(conf *Config) {
	const prefix = "asr.groq_whisper."
	if viper.IsSet(prefix + "api_key") {
		conf.APIKey = viper.GetString(prefix + "api_key")
	}
	if viper.IsSet(prefix + "api_url") {
		conf.APIURL = viper.GetString(prefix + "api_url")
	}
	if viper.IsSet(prefix + "model") {
		conf.Model = viper.GetString(prefix + "model")
	}
	if viper.IsSet(prefix + "language") {
		conf.Language = viper.GetString(prefix + "language")
	}
	if viper.IsSet(prefix + "response_format") {
		conf.ResponseFormat = viper.GetString(prefix + "response_format")
	}
	if viper.IsSet(prefix + "sample_rate") {
		if sr := viper.GetInt(prefix + "sample_rate"); sr > 0 {
			conf.SampleRate = sr
		}
	}
	if viper.IsSet(prefix + "temperature") {
		conf.Temperature = viper.GetFloat64(prefix + "temperature")
	}
	if viper.IsSet(prefix + "timeout") {
		if sec := viper.GetInt(prefix + "timeout"); sec > 0 {
			conf.Timeout = time.Duration(sec) * time.Second
		}
	}
}

func applyMapOverrides(conf *Config, cfg map[string]interface{}) {
	if cfg == nil {
		return
	}
	if v, ok := cfg["api_key"].(string); ok && v != "" {
		conf.APIKey = v
	}
	if v, ok := cfg["api_url"].(string); ok && v != "" {
		conf.APIURL = v
	}
	if v, ok := cfg["model"].(string); ok && v != "" {
		conf.Model = v
	}
	if v, ok := cfg["language"].(string); ok && v != "" {
		conf.Language = v
	}
	if v, ok := cfg["response_format"].(string); ok && v != "" {
		conf.ResponseFormat = v
	}
	if v, ok := cfg["sample_rate"].(float64); ok && v > 0 {
		conf.SampleRate = int(v)
	} else if v, ok := cfg["sample_rate"].(int); ok && v > 0 {
		conf.SampleRate = v
	}
	if v, ok := cfg["temperature"].(float64); ok {
		conf.Temperature = v
	}
	if v, ok := cfg["timeout"].(float64); ok && v > 0 {
		conf.Timeout = time.Duration(v) * time.Second
	} else if v, ok := cfg["timeout"].(int); ok && v > 0 {
		conf.Timeout = time.Duration(v) * time.Second
	}
}
