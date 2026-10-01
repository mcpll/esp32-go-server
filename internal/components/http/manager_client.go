package http

import (
	"context"
	"time"
)

// ManagerClient HTTP client for the Manager backend
type ManagerClient struct {
	client *Client
}

// ManagerClientConfig Manager client config
type ManagerClientConfig struct {
	BaseURL    string        // Manager backend base URL
	AuthToken  string        // Auth token (optional)
	Timeout    time.Duration // Request timeout
	MaxRetries int           // Max retries
}

// NewManagerClient creates a Manager backend HTTP client
func NewManagerClient(cfg ManagerClientConfig) *ManagerClient {
	client := NewClient(ClientConfig{
		BaseURL:    cfg.BaseURL,
		AuthToken:  cfg.AuthToken,
		Timeout:    cfg.Timeout,
		MaxRetries: cfg.MaxRetries,
	})

	return &ManagerClient{
		client: client,
	}
}

// DoRequest run HTTP request (wraps shared client DoRequest)
func (m *ManagerClient) DoRequest(ctx context.Context, opts RequestOptions) error {
	return m.client.DoRequest(ctx, opts)
}

// DoRequestRaw run HTTP request and return the raw response
func (m *ManagerClient) DoRequestRaw(ctx context.Context, opts RequestOptions) ([]byte, error) {
	return m.client.DoRequestRaw(ctx, opts)
}
