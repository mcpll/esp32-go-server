package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// Client generic HTTP client
type Client struct {
	httpClient *http.Client
	baseURL    string
	authToken  string
	maxRetries int
}

// NewClient creates an HTTP client
func NewClient(cfg ClientConfig) *Client {
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 1 // default retries 3 times
	}

	return &Client{
		httpClient: &http.Client{
			Timeout: cfg.Timeout,
		},
		baseURL:    cfg.BaseURL,
		authToken:  cfg.AuthToken,
		maxRetries: cfg.MaxRetries,
	}
}

// DoRequest performs an HTTP request
func (c *Client) DoRequest(ctx context.Context, opts RequestOptions) error {
	return c.doRequestOnce(ctx, opts)
}

// doRequestOnce performs a single HTTP request
func (c *Client) doRequestOnce(ctx context.Context, opts RequestOptions) error {
	// Build URL
	reqURL := c.baseURL + opts.Path

	// Add query params
	if len(opts.QueryParams) > 0 {
		params := url.Values{}
		for k, v := range opts.QueryParams {
			params.Set(k, v)
		}
		reqURL += "?" + params.Encode()
	}

	// Build request body
	var bodyReader io.Reader
	if opts.Body != nil {
		data, err := json.Marshal(opts.Body)
		if err != nil {
			return fmt.Errorf("序列化请求体失败: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, opts.Method, reqURL, bodyReader)
	if err != nil {
		return fmt.Errorf("创建请求失败: %w", err)
	}

	// Set default headers
	req.Header.Set("Content-Type", "application/json")

	// Set auth token
	if c.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.authToken)
	}

	// Set custom headers
	for k, v := range opts.Headers {
		req.Header.Set(k, v)
	}

	// Send request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取响应失败: %w", err)
	}

	// Check HTTP status code
	/*if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}*/

	// Parse response body
	if opts.Response != nil {
		if err := json.Unmarshal(body, opts.Response); err != nil {
			return fmt.Errorf("解析响应失败: %w, 响应体: %s", err, string(body))
		}
	}

	return nil
}

// DoRequestRaw performs HTTP request and returns raw response (no auto JSON parse)
func (c *Client) DoRequestRaw(ctx context.Context, opts RequestOptions) ([]byte, error) {
	var responseBody []byte
	var err error

	operation := func() error {
		// Build URL
		reqURL := c.baseURL + opts.Path

		// Add query params
		if len(opts.QueryParams) > 0 {
			params := url.Values{}
			for k, v := range opts.QueryParams {
				params.Set(k, v)
			}
			reqURL += "?" + params.Encode()
		}

		// Build request body
		var bodyReader io.Reader
		if opts.Body != nil {
			data, marshalErr := json.Marshal(opts.Body)
			if marshalErr != nil {
				return fmt.Errorf("序列化请求体失败: %w", marshalErr)
			}
			bodyReader = bytes.NewReader(data)
		}

		// Create HTTP request
		req, createErr := http.NewRequestWithContext(ctx, opts.Method, reqURL, bodyReader)
		if createErr != nil {
			return fmt.Errorf("创建请求失败: %w", createErr)
		}

		// Set default headers
		req.Header.Set("Content-Type", "application/json")

		// Set auth token
		if c.authToken != "" {
			req.Header.Set("Authorization", "Bearer "+c.authToken)
		}

		// Set custom headers
		for k, v := range opts.Headers {
			req.Header.Set(k, v)
		}

		// Send request
		resp, doErr := c.httpClient.Do(req)
		if doErr != nil {
			return fmt.Errorf("请求失败: %w", doErr)
		}
		defer resp.Body.Close()

		// Read response body
		responseBody, err = io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("读取响应失败: %w", err)
		}

		// Check HTTP status code
		if resp.StatusCode >= 400 {
			return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(responseBody))
		}

		return nil
	}

	if err := operation(); err != nil {
		return nil, err
	}

	return responseBody, nil
}
