package middleware

import (
	"gorm.io/gorm"
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

// RequireSessionCounter refuses a counter's request for a session it is not on (403).
// Routes without an :id parameter (the counter's own session list) pass through; that
// list is already limited to the counter's sessions.
func RequireSessionCounter(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.Param("id")
		if sessionID == "" {
			c.Next()
			return
		}
		ok, err := auth.CounterAssigned(c.Request.Context(), db, sessionID, c.GetString("user_id"))
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "could not check the session assignment"})
			return
		}
		if !ok {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "you are not on this stock count"})
			return
		}
		c.Next()
	}
}
