package controllers

import (
	"context"
	"net/http"
	"strings"
	"time"

	"xiaozhi/manager/backend/models"

	"github.com/gin-gonic/gin"
)

type discoverMCPConfigToolsRequest struct {
	Transport string            `json:"transport" binding:"required"`
	URL       string            `json:"url" binding:"required"`
	Headers   map[string]string `json:"headers"`
}

func (ac *AdminController) DiscoverMCPConfigTools(c *gin.Context) {
	var req discoverMCPConfigToolsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	service := models.MCPMarketService{
		Transport:   strings.TrimSpace(req.Transport),
		URL:         strings.TrimSpace(req.URL),
		HeadersJSON: encodeHeadersJSON(req.Headers),
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()

	tools, err := listImportedServiceTools(ctx, service)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"tools": tools,
	}})
}
