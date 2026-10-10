// Package pocketbase is the UserConfigProvider backed by PocketBase.
//
// The device server talks to PocketBase over REST with one superuser account. There is no
// third-party SDK: this file is the whole client.
package pocketbase

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	superusersAuthPath = "/api/collections/_superusers/auth-with-password"
	superusersRefresh  = "/api/collections/_superusers/auth-refresh"

	// refreshMargin is how long before the token's exp claim the client swaps it for a new one.
	refreshMargin = time.Minute
	// fallbackTokenLife is used when the token has no readable exp claim.
	fallbackTokenLife = 10 * time.Minute

	defaultPerPage = 200
)

// ErrNotFound is returned by First when no record matches.
var ErrNotFound = errors.New("pocketbase: record not found")

// APIError is a non-2xx answer from PocketBase.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("pocketbase: HTTP %d: %s", e.Status, e.Message)
}

// Record is one PocketBase record as decoded JSON.
type Record map[string]any

// String returns a text field, or "" when it is missing or not text.
func (r Record) String(key string) string {
	s, _ := r[key].(string)
	return s
}

// Bool returns a bool field, false when missing.
func (r Record) Bool(key string) bool {
	b, _ := r[key].(bool)
	return b
}

// Object returns a json field holding an object, or nil.
func (r Record) Object(key string) map[string]any {
	m, _ := r[key].(map[string]any)
	return m
}

// Strings returns a json field holding an array of strings; other items are skipped.
func (r Record) Strings(key string) []string {
	items, _ := r[key].([]any)
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// Expanded returns the record behind an expanded relation, or nil.
func (r Record) Expanded(relation string) Record {
	expand, _ := r["expand"].(map[string]any)
	m, _ := expand[relation].(map[string]any)
	if m == nil {
		return nil
	}
	return Record(m)
}

// Client is a PocketBase REST client that authenticates as a superuser.
type Client struct {
	baseURL  string
	email    string
	password string
	http     *http.Client
	stream   *http.Client // no timeout: the SSE body stays open until the context ends
	perPage  int

	mu        sync.Mutex // guards the token fields; held across the auth request
	token     string
	expiresAt time.Time
}

// NewClient returns a client for the PocketBase at baseURL.
func NewClient(baseURL, email, password string) *Client {
	return &Client{
		baseURL:  strings.TrimRight(baseURL, "/"),
		email:    email,
		password: password,
		http:     &http.Client{Timeout: 10 * time.Second},
		stream:   &http.Client{},
		perPage:  defaultPerPage,
	}
}

// authorize returns a usable token: the cached one, a refreshed one close to expiry, or a new login.
func (c *Client) authorize(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	if c.token != "" && now.Before(c.expiresAt.Add(-refreshMargin)) {
		return c.token, nil
	}
	if c.token != "" && now.Before(c.expiresAt) {
		if err := c.fetchToken(ctx, superusersRefresh, nil); err == nil {
			return c.token, nil
		}
		// Refresh failed: fall through to a full login.
	}
	body := map[string]string{"identity": c.email, "password": c.password}
	if err := c.fetchToken(ctx, superusersAuthPath, body); err != nil {
		return "", err
	}
	return c.token, nil
}

// forget drops the cached token so the next request logs in again.
func (c *Client) forget(token string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token == token {
		c.token = ""
	}
}

// fetchToken calls an auth endpoint and stores the token. Callers hold c.mu.
func (c *Client) fetchToken(ctx context.Context, path string, body any) error {
	status, raw, err := c.send(ctx, http.MethodPost, path, nil, body, c.token)
	if err != nil {
		return err
	}
	if status < 200 || status > 299 {
		return apiError(status, raw)
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || out.Token == "" {
		return fmt.Errorf("pocketbase: auth answer has no token")
	}
	c.token = out.Token
	c.expiresAt = tokenExpiry(out.Token)
	return nil
}

// tokenExpiry reads the exp claim of a JWT without verifying it.
func tokenExpiry(token string) time.Time {
	parts := strings.Split(token, ".")
	if len(parts) == 3 {
		if payload, err := base64.RawURLEncoding.DecodeString(parts[1]); err == nil {
			var claims struct {
				Exp int64 `json:"exp"`
			}
			if json.Unmarshal(payload, &claims) == nil && claims.Exp > 0 {
				return time.Unix(claims.Exp, 0)
			}
		}
	}
	return time.Now().Add(fallbackTokenLife)
}

// send performs one HTTP request. It does not authenticate; token goes in the Authorization header as is.
func (c *Client) send(ctx context.Context, method, path string, query url.Values, body any, token string) (int, []byte, error) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		reader = bytes.NewReader(raw)
	}
	target := c.baseURL + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	return resp.StatusCode, raw, err
}

func apiError(status int, raw []byte) error {
	var out struct {
		Message string `json:"message"`
	}
	msg := strings.TrimSpace(string(raw))
	if json.Unmarshal(raw, &out) == nil && out.Message != "" {
		msg = out.Message
	}
	return &APIError{Status: status, Message: msg}
}

// do runs an authenticated request and decodes the JSON answer into out (when not nil).
// A 401 drops the token and retries once with a fresh login.
func (c *Client) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	for attempt := 0; ; attempt++ {
		token, err := c.authorize(ctx)
		if err != nil {
			return err
		}
		status, raw, err := c.send(ctx, method, path, query, body, token)
		if err != nil {
			return err
		}
		if status == http.StatusUnauthorized && attempt == 0 {
			c.forget(token)
			continue
		}
		if status < 200 || status > 299 {
			return apiError(status, raw)
		}
		if out == nil {
			return nil
		}
		return json.Unmarshal(raw, out)
	}
}

type listPage struct {
	Page       int      `json:"page"`
	TotalPages int      `json:"totalPages"`
	Items      []Record `json:"items"`
}

// List returns every record of a collection that matches filter, reading all pages.
func (c *Client) List(ctx context.Context, collection, filter, expand string) ([]Record, error) {
	var all []Record
	for page := 1; ; page++ {
		q := url.Values{}
		q.Set("page", strconv.Itoa(page))
		q.Set("perPage", strconv.Itoa(c.perPage))
		if filter != "" {
			q.Set("filter", filter)
		}
		if expand != "" {
			q.Set("expand", expand)
		}
		var res listPage
		if err := c.do(ctx, http.MethodGet, recordsPath(collection, ""), q, nil, &res); err != nil {
			return nil, err
		}
		all = append(all, res.Items...)
		if page >= res.TotalPages {
			return all, nil
		}
	}
}

// First returns the first record that matches filter, or ErrNotFound.
func (c *Client) First(ctx context.Context, collection, filter, expand string) (Record, error) {
	q := url.Values{}
	q.Set("page", "1")
	q.Set("perPage", "1")
	q.Set("filter", filter)
	if expand != "" {
		q.Set("expand", expand)
	}
	var res listPage
	if err := c.do(ctx, http.MethodGet, recordsPath(collection, ""), q, nil, &res); err != nil {
		return nil, err
	}
	if len(res.Items) == 0 {
		return nil, ErrNotFound
	}
	return res.Items[0], nil
}

// Create adds a record.
func (c *Client) Create(ctx context.Context, collection string, fields map[string]any) (Record, error) {
	var rec Record
	if err := c.do(ctx, http.MethodPost, recordsPath(collection, ""), nil, fields, &rec); err != nil {
		return nil, err
	}
	return rec, nil
}

// Update changes fields of a record.
func (c *Client) Update(ctx context.Context, collection, id string, fields map[string]any) error {
	return c.do(ctx, http.MethodPatch, recordsPath(collection, id), nil, fields, nil)
}

// Delete removes a record.
func (c *Client) Delete(ctx context.Context, collection, id string) error {
	return c.do(ctx, http.MethodDelete, recordsPath(collection, id), nil, nil, nil)
}

// File downloads one file stored on a record.
func (c *Client) File(ctx context.Context, collection, id, name string) ([]byte, error) {
	path := "/api/files/" + url.PathEscape(collection) + "/" + url.PathEscape(id) + "/" + url.PathEscape(name)
	for attempt := 0; ; attempt++ {
		token, err := c.authorize(ctx)
		if err != nil {
			return nil, err
		}
		status, raw, err := c.send(ctx, http.MethodGet, path, nil, nil, token)
		if err != nil {
			return nil, err
		}
		if status == http.StatusUnauthorized && attempt == 0 {
			c.forget(token)
			continue
		}
		if status < 200 || status > 299 {
			return nil, apiError(status, raw)
		}
		return raw, nil
	}
}

func recordsPath(collection, id string) string {
	p := "/api/collections/" + url.PathEscape(collection) + "/records"
	if id != "" {
		p += "/" + url.PathEscape(id)
	}
	return p
}

// textEquals builds a PocketBase filter that matches a text field exactly.
func textEquals(field, value string) string {
	escaped := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value)
	return field + ` = "` + escaped + `"`
}
