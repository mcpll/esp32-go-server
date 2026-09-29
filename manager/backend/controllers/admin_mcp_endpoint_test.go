package controllers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"xiaozhi/manager/backend/models"

	"github.com/gin-gonic/gin"
)

func TestAdminGetAgentMCPEndpointIncludesRuntimeStatusFields(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupAgentDeviceServiceTestDB(t)
	if err := db.Create(&models.Config{
		Type:      "ota",
		Name:      "ota-default",
		ConfigID:  "ota-default",
		IsDefault: true,
		Enabled:   true,
		JsonData:  `{"external":{"websocket":{"url":"wss://example.test/go_ws/xiaozhi/v1/"}}}`,
	}).Error; err != nil {
		t.Fatalf("create ota config: %v", err)
	}

	router := gin.New()
	adminController := &AdminController{
		DB:                db,
		EndpointAuthToken: "test-endpoint-secret",
	}
	router.GET("/api/admin/agents/:id/mcp-endpoint", func(c *gin.Context) {
		c.Set("user_id", uint(7))
		adminController.GetAgentMCPEndpoint(c)
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/admin/agents/42/mcp-endpoint", nil)
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var payload struct {
		Data map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	endpoint, _ := payload.Data["endpoint"].(string)
	if !strings.HasPrefix(endpoint, "wss://example.test/mcp?token=") {
		t.Fatalf("endpoint = %q, want wss://example.test/mcp?token=...", endpoint)
	}
	if payload.Data["status"] != "unknown" {
		t.Fatalf("status = %#v, want unknown", payload.Data["status"])
	}
	if payload.Data["connected"] != false {
		t.Fatalf("connected = %#v, want false", payload.Data["connected"])
	}
	if payload.Data["client_count"] != float64(0) {
		t.Fatalf("client_count = %#v, want 0", payload.Data["client_count"])
	}
	if payload.Data["status_message"] != "websocket controller unavailable" {
		t.Fatalf("status_message = %#v, want websocket controller unavailable", payload.Data["status_message"])
	}
}
