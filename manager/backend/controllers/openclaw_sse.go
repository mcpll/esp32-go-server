package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type openClawChatStreamCaller interface {
	CallOpenClawChatStreamFromClient(
		ctx context.Context,
		body map[string]interface{},
		onResponse func(*WebSocketResponse) error,
	) (map[string]interface{}, error)
}

func wantsOpenClawSSE(c *gin.Context) bool {
	streamParam := strings.ToLower(strings.TrimSpace(c.Query("stream")))
	if streamParam == "1" || streamParam == "true" || streamParam == "yes" {
		return true
	}
	accept := strings.ToLower(strings.TrimSpace(c.GetHeader("Accept")))
	return strings.Contains(accept, "text/event-stream")
}

func prepareOpenClawSSE(c *gin.Context) bool {
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "streaming not supported"})
		return false
	}

	c.Header("Content-Type", "text/event-stream; charset=utf-8")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)
	flusher.Flush()
	return true
}

func writeOpenClawSSE(c *gin.Context, event string, payload interface{}) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	if strings.TrimSpace(event) != "" {
		if _, err := fmt.Fprintf(c.Writer, "event: %s\n", strings.TrimSpace(event)); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(c.Writer, "data: %s\n\n", data); err != nil {
		return err
	}

	if flusher, ok := c.Writer.(http.Flusher); ok {
		flusher.Flush()
	}
	return nil
}
