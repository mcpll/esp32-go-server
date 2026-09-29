package controllers

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"time"
	"xiaozhi/manager/backend/models"

	"github.com/gin-gonic/gin"
)

type APITokenResponse struct {
	ID          uint       `json:"id"`
	Name        string     `json:"name"`
	TokenPrefix string     `json:"token_prefix"`
	IsActive    bool       `json:"is_active"`
	LastUsedAt  *time.Time `json:"last_used_at"`
	ExpiresAt   *time.Time `json:"expires_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

func toAPITokenResponse(t models.APIToken) APITokenResponse {
	return APITokenResponse{
		ID:          t.ID,
		Name:        t.Name,
		TokenPrefix: t.TokenPrefix,
		IsActive:    t.IsActive,
		LastUsedAt:  t.LastUsedAt,
		ExpiresAt:   t.ExpiresAt,
		CreatedAt:   t.CreatedAt,
	}
}

func generateAPIToken() (string, string, string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", "", "", err
	}
	raw := "xzpat_" + hex.EncodeToString(buf)
	sum := sha256.Sum256([]byte(raw))
	hash := hex.EncodeToString(sum[:])
	prefixLen := 12
	if len(raw) < prefixLen {
		prefixLen = len(raw)
	}
	return raw, raw[:prefixLen], hash, nil
}

// CreateAPIToken 创建当前用户的API Token（明文仅返回一次）
func (uc *UserController) CreateAPIToken(c *gin.Context) {
	userIDRaw, _ := c.Get("user_id")
	userID, ok := userIDRaw.(uint)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "无效用户上下文"})
		return
	}

	var req struct {
		Name      string `json:"name" binding:"required,min=2,max=100"`
		ExpiresIn int    `json:"expires_in_days"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数错误: " + err.Error()})
		return
	}

	rawToken, prefix, hash, err := generateAPIToken()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "生成API Token失败"})
		return
	}

	var expiresAt *time.Time
	if req.ExpiresIn > 0 {
		t := time.Now().Add(time.Duration(req.ExpiresIn) * 24 * time.Hour)
		expiresAt = &t
	}

	token := models.APIToken{
		UserID:      userID,
		Name:        strings.TrimSpace(req.Name),
		TokenPrefix: prefix,
		TokenHash:   hash,
		IsActive:    true,
		ExpiresAt:   expiresAt,
	}
	if err := uc.DB.Create(&token).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存API Token失败"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message": "API Token创建成功，请妥善保存明文，后续无法再次查看",
		"data": gin.H{
			"token": rawToken,
			"meta":  toAPITokenResponse(token),
		},
	})
}

// ListAPITokens 获取当前用户的API Token列表（不返回明文）
func (uc *UserController) ListAPITokens(c *gin.Context) {
	userIDRaw, _ := c.Get("user_id")
	userID, ok := userIDRaw.(uint)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "无效用户上下文"})
		return
	}

	var tokens []models.APIToken
	if err := uc.DB.Where("user_id = ?", userID).Order("id DESC").Find(&tokens).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取API Token列表失败"})
		return
	}

	result := make([]APITokenResponse, 0, len(tokens))
	for _, t := range tokens {
		result = append(result, toAPITokenResponse(t))
	}

	c.JSON(http.StatusOK, gin.H{"data": result})
}

// RevokeAPIToken 吊销当前用户的API Token
func (uc *UserController) RevokeAPIToken(c *gin.Context) {
	userIDRaw, _ := c.Get("user_id")
	userID, ok := userIDRaw.(uint)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "无效用户上下文"})
		return
	}

	tokenID := c.Param("id")
	res := uc.DB.Model(&models.APIToken{}).
		Where("id = ? AND user_id = ?", tokenID, userID).
		Updates(map[string]interface{}{"is_active": false})
	if res.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "吊销API Token失败"})
		return
	}
	if res.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "API Token不存在"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "API Token已吊销"})
}
