package mcp_market

import "strings"

var providerPresets = []MarketProviderPreset{
	{
		ID:          ProviderGeneric,
		Name:        "自定义",
		AuthType:    AuthTypeNone,
		Description: "手动填写目录/详情 API 地址，适配任意 MCP 市场。",
	},
	{
		ID:                ProviderModelScope,
		Name:              "魔搭 ModelScope",
		CatalogURL:        "https://www.modelscope.cn/openapi/v1/mcp/servers",
		DetailURLTemplate: "https://www.modelscope.cn/openapi/v1/mcp/servers/{raw_id}",
		AuthType:          AuthTypeBearer,
		Description:       "固定使用 Bearer Token 鉴权，仅拉取已激活服务（/operational）。",
	},
}

func ListProviderPresets() []MarketProviderPreset {
	out := make([]MarketProviderPreset, len(providerPresets))
	copy(out, providerPresets)
	return out
}

func NormalizeProviderID(id string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	if id == "" {
		return ProviderGeneric
	}
	for _, preset := range providerPresets {
		if id == preset.ID {
			return id
		}
	}
	return ProviderGeneric
}

func GetProviderPreset(id string) (MarketProviderPreset, bool) {
	id = NormalizeProviderID(id)
	for _, preset := range providerPresets {
		if preset.ID == id {
			return preset, true
		}
	}
	return MarketProviderPreset{}, false
}
