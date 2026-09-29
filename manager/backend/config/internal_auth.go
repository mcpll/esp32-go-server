package config

import "strings"

const DefaultInternalAuthToken = "xiaozhi_admin_secret_key"
const DefaultEndpointAuthToken = "xiaozhi_mcp_openclaw_secret_key"

// ResolveInternalAuthToken 解析控制台内部服务通用 Token。
// 优先级：
// 1. 配置文件中的 internal_auth_token
// 2. 默认值（与主程序保持一致）
func ResolveInternalAuthToken(cfg *Config) string {
	if cfg != nil {
		if token := strings.TrimSpace(cfg.InternalAuthToken); token != "" {
			return token
		}
	}
	return DefaultInternalAuthToken
}

// ResolveEndpointAuthToken 解析 MCP/OpenClaw 端点 JWT 的签名 Token。
// 优先级：
// 1. 配置文件中的 endpoint_auth_token
// 2. 默认值（与主程序保持一致）
func ResolveEndpointAuthToken(cfg *Config) string {
	if cfg != nil {
		if token := strings.TrimSpace(cfg.EndpointAuthToken); token != "" {
			return token
		}
	}
	return DefaultEndpointAuthToken
}
