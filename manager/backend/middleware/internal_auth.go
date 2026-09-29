package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// InternalServiceAuth 内部服务鉴权中间件。
// 要求：Authorization: Bearer <token>
func InternalServiceAuth(expectedToken string) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := strings.TrimSpace(c.GetHeader("Authorization"))
		if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "缺少内部服务认证信息"})
			c.Abort()
			return
		}

		token := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
		if token == "" || token != expectedToken {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "无效的内部服务认证信息"})
			c.Abort()
			return
		}

		c.Next()
	}
}
