package http

import "time"

// ClientConfig is HTTP client configuration
type ClientConfig struct {
	BaseURL    string        // Base URL
	AuthToken  string        // Auth token (optional)
	Timeout    time.Duration // Request timeout
	MaxRetries int           // Max retries (default 3)
}

// RequestOptions are request options
type RequestOptions struct {
	Method      string            // HTTP method
	Path        string            // Request path
	QueryParams map[string]string // Query parameters
	Headers     map[string]string // Custom request headers
	Body        interface{}       // Request body (auto-serialized as JSON)
	Response    interface{}       // Response body (auto-deserialized)
}
