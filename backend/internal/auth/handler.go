package auth

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/totalretail/stocktake/internal/sms"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type Handler struct {
	svc               *Service
	smsSvc            sms.Service
	db                *gorm.DB
	counterTokenHours int
	adminTokenHours   int
}

func NewHandler(svc *Service, smsSvc sms.Service, db *gorm.DB, counterHours, adminHours int) *Handler {
	return &Handler{svc: svc, smsSvc: smsSvc, db: db,
		counterTokenHours: counterHours, adminTokenHours: adminHours}
}

func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.POST("/auth/admin/login", h.AdminLogin)
	rg.POST("/auth/counter/request-otp", h.RequestOTP)
	rg.POST("/auth/counter/verify-otp", h.VerifyOTP)
}

func (h *Handler) AdminLogin(c *gin.Context) {
	var req struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var admin AdminUser
	if err := h.db.Where("username = ? AND active = ?", req.Username, true).First(&admin).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte(req.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}

	token, err := h.svc.IssueToken(admin.ID, TokenAdmin, h.adminTokenHours, admin.IsSuperAdmin)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to issue token"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"token": token, "admin_id": admin.ID, "is_super_admin": admin.IsSuperAdmin})
}

func (h *Handler) RequestOTP(c *gin.Context) {
	var req struct {
		Mobile string `json:"mobile" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	req.Mobile = NormalizeMobile(req.Mobile)
	otp, err := h.svc.GenerateOTP(c.Request.Context(), req.Mobile)
	if err != nil {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": err.Error()})
		return
	}
	// DEV: log OTP so it can be used when SMS gateway is unavailable
	log.Printf("DEV [RequestOTP] OTP for %s: %s", req.Mobile, otp)
	if err := h.smsSvc.Send(c.Request.Context(), req.Mobile, "Your StockCount OTP is: "+otp+". Valid for 10 minutes."); err != nil {
		log.Printf("WARN [RequestOTP] SMS send failed for %s: %v — OTP still valid, check logs", req.Mobile, err)
	}
	c.JSON(http.StatusOK, gin.H{"message": "OTP sent"})
}

func (h *Handler) VerifyOTP(c *gin.Context) {
	var req struct {
		Mobile string `json:"mobile" binding:"required"`
		OTP    string `json:"otp" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	req.Mobile = NormalizeMobile(req.Mobile)
	valid, err := h.svc.VerifyOTP(c.Request.Context(), req.Mobile, req.OTP)
	if err != nil || !valid {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired OTP"})
		return
	}

	counter := Counter{Name: req.Mobile, MobileNumber: req.Mobile}
	h.db.Where(Counter{MobileNumber: req.Mobile}).FirstOrCreate(&counter)

	token, err := h.svc.IssueToken(counter.ID, TokenCounter, h.counterTokenHours)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to issue token"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"token": token, "counter_id": counter.ID, "counter_name": counter.Name})
}