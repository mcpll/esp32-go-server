package controllers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestGetSystemConfigsReturnsServiceUnavailableWhenDatabaseMissing(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	adminController := &AdminController{}
	router.GET("/api/system/configs", adminController.GetSystemConfigs)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/system/configs", nil)
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
}
