package mcp_market

import "time"

const (
	MCPMarketConfigType = "mcp_market"
	MCPConfigType       = "mcp"
)

const (
	ProviderGeneric    = "generic"
	ProviderModelScope = "modelscope"
)

const (
	AuthTypeNone   = "none"
	AuthTypeBearer = "bearer"
	AuthTypeHeader = "header"
)

const (
	TransportSSE            = "sse"
	TransportStreamableHTTP = "streamablehttp"
)

// AuthConfig defines how to authenticate when pulling marketplace data.
type AuthConfig struct {
	Type         string            `json:"type"`
	HeaderName   string            `json:"header_name,omitempty"`
	Token        string            `json:"token,omitempty"`
	ExtraHeaders map[string]string `json:"extra_headers,omitempty"`
}

// MarketConnection stores a marketplace connection config in Config.json_data.
type MarketConnection struct {
	Name                   string `json:"name"`
	ProviderID             string `json:"provider_id,omitempty"`
	CatalogURL             string `json:"catalog_url"`
	DetailURLTemplate      string `json:"detail_url_template,omitempty"`
	Enabled                bool   `json:"enabled"`
	AuthType               string `json:"auth_type"`
	HeaderName             string `json:"header_name,omitempty"`
	TokenCiphertext        string `json:"token_ciphertext,omitempty"`
	TokenNonce             string `json:"token_nonce,omitempty"`
	TokenMask              string `json:"token_mask,omitempty"`
	ExtraHeadersCiphertext string `json:"extra_headers_ciphertext,omitempty"`
	ExtraHeadersNonce      string `json:"extra_headers_nonce,omitempty"`
	UpdatedAtUnix          int64  `json:"updated_at_unix,omitempty"`
}

// MarketConnectionView is the API response shape for market connections.
type MarketConnectionView struct {
	ID                uint   `json:"id"`
	ConfigID          string `json:"config_id"`
	Name              string `json:"name"`
	ProviderID        string `json:"provider_id"`
	CatalogURL        string `json:"catalog_url"`
	DetailURLTemplate string `json:"detail_url_template,omitempty"`
	Enabled           bool   `json:"enabled"`
	AuthType          string `json:"auth_type"`
	HeaderName        string `json:"header_name,omitempty"`
	TokenMask         string `json:"token_mask,omitempty"`
	HasToken          bool   `json:"has_token"`
	CreatedAt         string `json:"created_at"`
	UpdatedAt         string `json:"updated_at"`
}

// MarketServiceSummary is a normalized list item from remote marketplaces.
type MarketServiceSummary struct {
	MarketID    uint   `json:"market_id"`
	MarketName  string `json:"market_name"`
	ServiceID   string `json:"service_id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// ParsedEndpoint represents an MCP endpoint parsed from marketplace detail payload.
type ParsedEndpoint struct {
	Name      string            `json:"name,omitempty"`
	Transport string            `json:"transport"`
	URL       string            `json:"url"`
	Headers   map[string]string `json:"headers,omitempty"`
}

// MarketServiceDetail is normalized service detail for UI/import.
type MarketServiceDetail struct {
	MarketID    uint                   `json:"market_id"`
	MarketName  string                 `json:"market_name"`
	ServiceID   string                 `json:"service_id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Endpoints   []ParsedEndpoint       `json:"endpoints"`
	Raw         map[string]interface{} `json:"raw,omitempty"`
}

// ImportResult provides import response data.
type ImportResult struct {
	ServiceID      string   `json:"service_id"`
	ServiceName    string   `json:"service_name"`
	MarketID       uint     `json:"market_id"`
	MarketName     string   `json:"market_name"`
	ImportedNames  []string `json:"imported_names"`
	ImportedCount  int      `json:"imported_count"`
	PrecheckPassed bool     `json:"precheck_passed"`
}

// MarketProviderPreset defines builtin provider presets for marketplace onboarding.
type MarketProviderPreset struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	CatalogURL        string `json:"catalog_url"`
	DetailURLTemplate string `json:"detail_url_template,omitempty"`
	AuthType          string `json:"auth_type"`
	HeaderName        string `json:"header_name,omitempty"`
	Description       string `json:"description,omitempty"`
}

// MergedMCPConfig is an internal structure used during apply/import.
type MergedMCPConfig struct {
	MCP      map[string]interface{} `json:"mcp"`
	LocalMCP map[string]interface{} `json:"local_mcp,omitempty"`
}

// HTTPOptions controls outbound request behavior.
type HTTPOptions struct {
	Timeout  time.Duration
	Method   string
	JSONBody interface{}
	Query    map[string]string
	Cookies  map[string]string
}
