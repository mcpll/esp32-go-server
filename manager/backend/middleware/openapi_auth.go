package middleware

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"time"
	"xiaozhi/manager/backend/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// OpenAPIAuth 支持 JWT 或 API Token 的鉴权。
// API Token 支持两种请求头：
// 1) Authorization: Bearer <token>
// 2) X-API-Token: <token>
func OpenAPIAuth(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		if db == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "数据库不可用，无法校验OpenAPI令牌"})
			c.Abort()
			return
		}

		if authHeader := strings.TrimSpace(c.GetHeader("Authorization")); strings.HasPrefix(authHeader, "Bearer ") {
			tokenString := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
			if claims, err := ParseToken(tokenString); err == nil {
				c.Set("user_id", claims.UserID)
				c.Set("username", claims.Username)
				c.Set("role", claims.Role)
				c.Set("auth_type", "jwt")
				c.Next()
				return
			}
		}

		rawToken := strings.TrimSpace(c.GetHeader("X-API-Token"))
		if rawToken == "" {
			authHeader := strings.TrimSpace(c.GetHeader("Authorization"))
			if strings.HasPrefix(authHeader, "Bearer ") {
				rawToken = strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
			}
		}

		if rawToken == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "缺少认证信息（JWT或API Token）"})
			c.Abort()
			return
		}

		now := time.Now()
		tokenHash := hashToken(rawToken)

		var apiToken models.APIToken
		err := db.Where("token_hash = ? AND is_active = ?", tokenHash, true).
			Where("expires_at IS NULL OR expires_at > ?", now).
			First(&apiToken).Error
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "无效或已过期的API Token"})
			c.Abort()
			return
		}

		var user models.User
		if err := db.First(&user, apiToken.UserID).Error; err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "API Token所属用户不存在"})
			c.Abort()
			return
		}

		db.Model(&apiToken).Updates(map[string]interface{}{"last_used_at": now})

		c.Set("user_id", user.ID)
		c.Set("username", user.Username)
		c.Set("role", user.Role)
		c.Set("auth_type", "api_token")
		c.Set("api_token_id", apiToken.ID)
		c.Next()
	}
}
