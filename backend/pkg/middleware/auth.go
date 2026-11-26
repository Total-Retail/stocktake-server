package middleware

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/totalretail/stocktake/internal/auth"
	"github.com/totalretail/stocktake/internal/devices"
	"gorm.io/gorm"
)

// HeaderCounterID carries the tapped-in counter on a shared device's requests.
const HeaderCounterID = "X-Counter-Id"

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
		c.Set("device_version", claims.DeviceVersion)
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

// RequireActiveDevice, after RequireAuth(TokenDevice), refuses a revoked device or a
// superseded token, and stores the *devices.Device for the handler.
func RequireActiveDevice(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		d, ok := activeDevice(c, db)
		if !ok {
			return
		}
		c.Set(devices.CtxDevice, d)
		c.Set("device_id", d.ID)
		c.Next()
	}
}

func activeDevice(c *gin.Context, db *gorm.DB) (*devices.Device, bool) {
	d, err := devices.CheckDeviceToken(c.Request.Context(), db, c.GetString("user_id"), c.GetInt("device_version"))
	if errors.Is(err, devices.ErrDeviceRevoked) {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": err.Error(), "code": "device_revoked"})
		return nil, false
	}
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "could not check the device"})
		return nil, false
	}
	devices.Touch(c.Request.Context(), db, d.ID)
	return d, true
}

// ResolveCounter, after RequireAuth(TokenCounter, TokenDevice), settles who is counting.
//
// A counter token: the counter is the token's user (signed in with phone + OTP).
// A device token (13 Oct 2026, NFC cards): the device must still be enrolled, and the
// counter is the one tapped in on it, sent in X-Counter-Id; "user_id" becomes that
// counter, so every counter route works unchanged, and "device_id"/"auth_method" say
// where the count came from. RequireSessionCounter then checks the counter is on the
// session, exactly as for a phone sign-in.
func ResolveCounter(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.GetString("token_type") != string(auth.TokenDevice) {
			c.Set("auth_method", devices.AuthOTP)
			c.Next()
			return
		}
		d, ok := activeDevice(c, db)
		if !ok {
			return
		}
		counterID := strings.TrimSpace(c.GetHeader(HeaderCounterID))
		if counterID == "" {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "no counter tapped in on this device", "code": "no_counter"})
			return
		}
		exists, err := devices.CounterExists(c.Request.Context(), db, counterID)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "could not check the counter"})
			return
		}
		if !exists {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "unknown counter", "code": "unknown_counter"})
			return
		}
		// A device tied to a store counts only that store's sessions.
		if sessionID := c.Param("id"); sessionID != "" {
			ok, err := devices.SessionInDeviceStore(c.Request.Context(), db, d, sessionID)
			if err != nil {
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "could not check the session"})
				return
			}
			if !ok {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "this device is set up for another store", "code": "wrong_store"})
				return
			}
		}
		c.Set(devices.CtxDevice, d)
		c.Set("device_id", d.ID)
		c.Set("user_id", counterID)
		c.Set("auth_method", devices.AuthNFC)
		c.Next()
	}
}
