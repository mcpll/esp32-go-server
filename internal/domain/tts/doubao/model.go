package doubao

import (
	"fmt"
	"strings"
)

const (
	defaultDoubaoTTSModel  = "seed-tts-1.1"
	defaultDoubaoHTTPURL   = "https://openspeech.bytedance.com/api/v3/tts/unidirectional"
	defaultDoubaoWSURL     = "wss://openspeech.bytedance.com/api/v3/tts/unidirectional/stream"
	defaultDoubaoAudioFmt  = "mp3"
	defaultDoubaoSampleHz  = 24000
	resourceSeedTTS10      = "seed-tts-1.0"
	resourceSeedTTS20      = "seed-tts-2.0"
	modelSeedTTS11         = "seed-tts-1.1"
	modelSeedTTS20Standard = "seed-tts-2.0-standard"
	modelSeedTTS20Expr     = "seed-tts-2.0-expressive"
)

type resolvedTTSModel struct {
	ConfigModel    string
	RequestModel   string
	ResourceID     string
	VoiceFamily    string
	DerivedByVoice bool
}

func normalizeDoubaoTTSURL(raw, fallback string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback
	}
	return raw
}

func normalizeDoubaoModel(model string) string {
	model = strings.TrimSpace(strings.ToLower(model))
	switch model {
	case "", "default":
		return ""
	case strings.ToLower(modelSeedTTS11):
		return modelSeedTTS11
	case strings.ToLower(modelSeedTTS20Standard), "seed-tts-2.0":
		return modelSeedTTS20Standard
	case strings.ToLower(modelSeedTTS20Expr):
		return modelSeedTTS20Expr
	default:
		return strings.TrimSpace(model)
	}
}

func inferDoubaoVoiceFamily(voice string) string {
	voice = strings.TrimSpace(strings.ToLower(voice))
	switch {
	case voice == "":
		return "unknown"
	case strings.HasPrefix(voice, "saturn_"):
		return "tts2"
	case strings.HasPrefix(voice, "s_"), strings.HasPrefix(voice, "icl_"):
		return "clone"
	case strings.Contains(voice, "_bigtts"):
		return "tts2"
	default:
		return "tts1"
	}
}

func resolveDoubaoTTSModel(model, voice string) (resolvedTTSModel, error) {
	voiceFamily := inferDoubaoVoiceFamily(voice)
	if voiceFamily == "clone" {
		return resolvedTTSModel{}, fmt.Errorf("豆包声音复刻已移除")
	}
	normalized := normalizeDoubaoModel(model)
	if strings.HasPrefix(normalized, "seed-icl-") {
		return resolvedTTSModel{}, fmt.Errorf("豆包声音复刻已移除")
	}
	if voiceFamily == "tts2" && normalized == modelSeedTTS11 {
		normalized = modelSeedTTS20Standard
	}
	if normalized == "" {
		switch voiceFamily {
		case "tts2":
			return resolvedTTSModel{
				ConfigModel:    modelSeedTTS20Standard,
				RequestModel:   "",
				ResourceID:     resourceSeedTTS20,
				VoiceFamily:    voiceFamily,
				DerivedByVoice: true,
			}, nil
		default:
			return resolvedTTSModel{
				ConfigModel:    defaultDoubaoTTSModel,
				RequestModel:   modelSeedTTS11,
				ResourceID:     resourceSeedTTS10,
				VoiceFamily:    voiceFamily,
				DerivedByVoice: true,
			}, nil
		}
	}

	resolved := resolvedTTSModel{
		ConfigModel: normalized,
		VoiceFamily: voiceFamily,
	}
	switch normalized {
	case modelSeedTTS11:
		if voiceFamily == "tts2" {
			resolved.RequestModel = ""
			resolved.ResourceID = resourceSeedTTS20
			break
		}
		resolved.RequestModel = modelSeedTTS11
		resolved.ResourceID = resourceSeedTTS10
	case modelSeedTTS20Standard:
		if voiceFamily == "tts2" {
			resolved.RequestModel = ""
		} else {
			resolved.RequestModel = modelSeedTTS20Standard
		}
		resolved.ResourceID = resourceSeedTTS20
	case modelSeedTTS20Expr:
		if voiceFamily == "tts2" {
			resolved.RequestModel = ""
		} else {
			resolved.RequestModel = modelSeedTTS20Expr
		}
		resolved.ResourceID = resourceSeedTTS20
	default:
		return resolvedTTSModel{}, fmt.Errorf("不支持的豆包 TTS 模型: %s", model)
	}

	if voiceFamily == "tts1" && resolved.ResourceID == resourceSeedTTS20 {
		return resolvedTTSModel{}, fmt.Errorf("豆包 1.0 公版音色需要匹配 seed-tts-1.0 模型族")
	}

	return resolved, nil
}
