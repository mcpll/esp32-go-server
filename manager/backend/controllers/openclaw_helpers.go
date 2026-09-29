package controllers

import (
	"encoding/json"
	"strings"

	"xiaozhi/manager/backend/models"
)

type OpenClawConfigResponse struct {
	Allowed       bool     `json:"allowed"`
	EnterKeywords []string `json:"enter_keywords"`
	ExitKeywords  []string `json:"exit_keywords"`
}

var (
	defaultOpenClawEnterKeywords = []string{"打开龙虾", "进入龙虾"}
	defaultOpenClawExitKeywords  = []string{"关闭龙虾", "退出龙虾"}
)

func cloneOpenClawKeywords(keywords []string) []string {
	if len(keywords) == 0 {
		return []string{}
	}
	cloned := make([]string, len(keywords))
	copy(cloned, keywords)
	return cloned
}

func normalizeOpenClawConfig(cfg OpenClawConfigResponse) OpenClawConfigResponse {
	enterKeywords := normalizeOpenClawKeywords(cfg.EnterKeywords)
	if len(enterKeywords) == 0 {
		enterKeywords = cloneOpenClawKeywords(defaultOpenClawEnterKeywords)
	}
	exitKeywords := normalizeOpenClawKeywords(cfg.ExitKeywords)
	if len(exitKeywords) == 0 {
		exitKeywords = cloneOpenClawKeywords(defaultOpenClawExitKeywords)
	}

	return OpenClawConfigResponse{
		Allowed:       cfg.Allowed,
		EnterKeywords: enterKeywords,
		ExitKeywords:  exitKeywords,
	}
}

func defaultOpenClawConfig() OpenClawConfigResponse {
	return normalizeOpenClawConfig(OpenClawConfigResponse{
		Allowed:       false,
		EnterKeywords: cloneOpenClawKeywords(defaultOpenClawEnterKeywords),
		ExitKeywords:  cloneOpenClawKeywords(defaultOpenClawExitKeywords),
	})
}

func normalizeOpenClawKeywords(keywords []string) []string {
	normalized := make([]string, 0, len(keywords))
	seen := make(map[string]struct{}, len(keywords))
	for _, keyword := range keywords {
		trimmed := strings.TrimSpace(keyword)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		normalized = append(normalized, trimmed)
	}
	return normalized
}

func parseOpenClawConfig(raw string) OpenClawConfigResponse {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultOpenClawConfig()
	}

	var parsed OpenClawConfigResponse
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return defaultOpenClawConfig()
	}
	return normalizeOpenClawConfig(parsed)
}

func mustOpenClawConfigJSON(cfg OpenClawConfigResponse) string {
	normalized := normalizeOpenClawConfig(cfg)
	data, err := json.Marshal(normalized)
	if err != nil {
		return `{"allowed":false,"enter_keywords":["打开龙虾","进入龙虾"],"exit_keywords":["关闭龙虾","退出龙虾"]}`
	}
	return string(data)
}

func applyOpenClawConfigToAgent(agent *models.Agent, cfg OpenClawConfigResponse) {
	if agent == nil {
		return
	}
	normalized := normalizeOpenClawConfig(cfg)
	agent.OpenClawConfig = mustOpenClawConfigJSON(normalized)
}

func mergeOpenClawConfig(
	base OpenClawConfigResponse,
	direct *OpenClawConfigResponse,
) OpenClawConfigResponse {
	result := normalizeOpenClawConfig(base)
	if direct != nil {
		result = normalizeOpenClawConfig(*direct)
	}
	return normalizeOpenClawConfig(result)
}

func buildOpenClawConfigFromAgent(agent models.Agent) OpenClawConfigResponse {
	return parseOpenClawConfig(agent.OpenClawConfig)
}
