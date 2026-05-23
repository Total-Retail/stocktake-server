package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/totalretail/stocktake/internal/auth"
)

func RequireAuth(authSvc *auth.Service, allowedTypes ...auth.TokenType) gin.HandlerFunc {
	allowed := make(map[auth.TokenType]bool)
	for _, t := range allowedTypes {
		allowed[t] = true
	}

	return func(c *gin.Context) {
		// Try Authorization header first, then ?token= query param (needed for WebSocket)
		tokenStr := ""
		header := c.GetHeader("Authorization")
		if strings.HasPrefix(header, "Bearer ") {
			tokenStr = strings.TrimPrefix(header, "Bearer ")
		} else if q := c.Query("token"); q != "" {
			tokenStr = q
		}

		if tokenStr == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing token"})
			return
		}

		claims, err := authSvc.ValidateToken(tokenStr)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			return
		}
		if len(allowed) > 0 && !allowed[claims.TokenType] {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
			return
		}
		c.Set("user_id", claims.UserID)
		c.Set("token_type", string(claims.TokenType))
		c.Set("is_super_admin", claims.IsSuperAdmin)
		c.Next()
	}
}