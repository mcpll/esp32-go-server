package pocketbase

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Watch opens the PocketBase realtime stream and subscribes to subs.
// onSubscribed runs after every successful subscribe. onEvent runs for each
// later server-sent event. subscribed is true once the subscribe POST succeeded,
// including when the stream then drops.
func (c *Client) Watch(ctx context.Context, subs []string, onSubscribed func(), onEvent func(event string)) (subscribed bool, err error) {
	token, err := c.authorize(ctx)
	if err != nil {
		return false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/realtime", nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Authorization", token)
	resp, err := c.stream.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		c.forget(token)
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return false, apiError(resp.StatusCode, raw)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return false, apiError(resp.StatusCode, raw)
	}

	err = readSSE(resp.Body, func(event, data string) error {
		if !subscribed {
			if event != "PB_CONNECT" {
				return fmt.Errorf("pocketbase: expected PB_CONNECT, got %q", event)
			}
			var msg struct {
				ClientID string `json:"clientId"`
			}
			if json.Unmarshal([]byte(data), &msg) != nil || msg.ClientID == "" {
				return errors.New("pocketbase: realtime connect has no client id")
			}
			if err := c.do(ctx, http.MethodPost, "/api/realtime", nil, map[string]any{
				"clientId":      msg.ClientID,
				"subscriptions": subs,
			}, nil); err != nil {
				return err
			}
			subscribed = true
			if onSubscribed != nil {
				onSubscribed()
			}
			return nil
		}
		if onEvent != nil && event != "" {
			onEvent(event)
		}
		return nil
	})
	return subscribed, err
}

func readSSE(r io.Reader, fn func(event, data string) error) error {
	br := bufio.NewReader(r)
	var event, data strings.Builder
	flush := func() error {
		if event.Len() == 0 && data.Len() == 0 {
			return nil
		}
		err := fn(event.String(), data.String())
		event.Reset()
		data.Reset()
		return err
	}
	for {
		line, err := br.ReadString('\n')
		if len(line) > 0 {
			line = strings.TrimRight(line, "\r\n")
			switch {
			case line == "":
				if flushErr := flush(); flushErr != nil {
					return flushErr
				}
			case strings.HasPrefix(line, ":"):
			case strings.HasPrefix(line, "event:"):
				event.Reset()
				event.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "event:")))
			case strings.HasPrefix(line, "data:"):
				if data.Len() > 0 {
					data.WriteByte('\n')
				}
				data.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				if flushErr := flush(); flushErr != nil {
					return flushErr
				}
				return io.EOF
			}
			return err
		}
	}
}
