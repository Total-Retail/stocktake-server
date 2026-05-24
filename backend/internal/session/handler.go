package session

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/totalretail/stocktake/internal/auth"
	"github.com/totalretail/stocktake/internal/sms"
)

type Handler struct {
	svc               Service
	authSvc           *auth.Service
	smsSvc            sms.Service
	counterTokenHours int
	exportDir         string
}

func NewHandler(svc Service, authSvc *auth.Service, smsSvc sms.Service, counterHours int, exportDir string) *Handler {
	return &Handler{svc: svc, authSvc: authSvc, smsSvc: smsSvc, counterTokenHours: counterHours, exportDir: exportDir}
}

// RegisterRoutes registers admin-only session routes.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/sessions", h.ListSessions)
	rg.POST("/sessions", h.CreateSession)
	rg.GET("/sessions/:id", h.GetSession)
	rg.PUT("/sessions/:id/status", h.UpdateStatus)
	rg.POST("/sessions/:id/abort", h.AbortSession)
	rg.POST("/sessions/:id/reopen", h.ReopenSession)

	rg.GET("/sessions/:id/counters", h.ListCounters)
	rg.POST("/sessions/:id/counters", h.AddCounter)
	rg.DELETE("/sessions/:id/counters/:counter_id", h.RemoveCounter)
	rg.POST("/sessions/:id/counters/:counter_id/resend-otp", h.ResendOTP)

	rg.POST("/sessions/:id/pull-theoretical", h.PullTheoretical)
	rg.POST("/sessions/:id/submit", h.SubmitToLS)
	rg.GET("/sessions/:id/export", h.DownloadExport)

	rg.GET("/ls/worksheets", h.GetAvailableWorksheets)
	rg.GET("/ls/stores", h.GetLSStores)
	rg.PUT("/sessions/:id", h.UpdateSession)
}

// RegisterCounterRoutes registers session routes accessible with a counter (mobile) token.
func (h *Handler) RegisterCounterRoutes(rg *gin.RouterGroup) {
	rg.GET("/counter/sessions", h.GetCounterSessionViews)
	rg.GET("/counter/sessions/:id", h.GetCounterSessionView)
	rg.GET("/counter/sessions/:id/bins", h.GetSessionBins)
	rg.GET("/counter/sessions/:id/items/:barcode", h.GetSessionItemByBarcode)
}

func (h *Handler) GetLSStores(c *gin.Context) {
	lsStores, err := h.svc.GetLSStores(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, lsStores)
}

func (h *Handler) GetAvailableWorksheets(c *gin.Context) {
	worksheets, err := h.svc.GetAvailableWorksheets(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, worksheets)
}

func (h *Handler) UpdateSession(c *gin.Context) {
	var req struct {
		WorksheetSeqNo int `json:"worksheet_seq_no"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.requireMutableSession(c); err != nil {
		return
	}
	sess, err := h.svc.UpdateSession(c.Request.Context(), c.Param("id"), req.WorksheetSeqNo)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, sess)
}

func (h *Handler) ListSessions(c *gin.Context) {
	storeID := c.Query("store_id")
	sessions, err := h.svc.ListSessions(c.Request.Context(), storeID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, sessions)
}

func (h *Handler) GetSession(c *gin.Context) {
	sess, err := h.svc.GetSession(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return
	}
	c.JSON(http.StatusOK, sess)
}

func (h *Handler) CreateSession(c *gin.Context) {
	var s Session
	if err := c.ShouldBindJSON(&s); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	s.CreatedBy = c.GetString("user_id")
	created, err := h.svc.CreateSession(c.Request.Context(), s)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, created)
}

func (h *Handler) UpdateStatus(c *gin.Context) {
	var req struct {
		Status SessionStatus `json:"status" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.svc.UpdateStatus(c.Request.Context(), c.Param("id"), req.Status); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": req.Status})
}

// AbortSession aborts a DRAFT or ACTIVE stock count session with a mandatory reason.
func (h *Handler) AbortSession(c *gin.Context) {
	var req struct {
		Reason string `json:"reason" binding:"required,min=10"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	adminID := c.GetString("user_id")
	if err := h.svc.AbortSession(c.Request.Context(), c.Param("id"), req.Reason, adminID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"aborted": true})
}

// ReopenSession reopens a POSTED session (super admin only — enforced at middleware level).
func (h *Handler) ReopenSession(c *gin.Context) {
	if !c.GetBool("is_super_admin") {
		c.JSON(http.StatusForbidden, gin.H{"error": "only super admins can reopen sessions"})
		return
	}
	if err := h.svc.ReopenSession(c.Request.Context(), c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"reopened": true})
}

func (h *Handler) ListCounters(c *gin.Context) {
	counters, err := h.svc.ListCounters(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, counters)
}

func (h *Handler) AddCounter(c *gin.Context) {
	var req struct {
		Name   string `json:"name"   binding:"required"`
		Mobile string `json:"mobile" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.requireMutableSession(c); err != nil {
		return
	}
	counter, err := h.svc.UpsertCounter(c.Request.Context(), req.Name, req.Mobile)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := h.svc.AddCounter(c.Request.Context(), c.Param("id"), counter.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, counter)
}

func (h *Handler) RemoveCounter(c *gin.Context) {
	if err := h.requireMutableSession(c); err != nil {
		return
	}
	if err := h.svc.RemoveCounter(c.Request.Context(), c.Param("id"), c.Param("counter_id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"removed": true})
}

func (h *Handler) ResendOTP(c *gin.Context) {
	counters, err := h.svc.ListCounters(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var mobile string
	for _, ct := range counters {
		if ct.ID == c.Param("counter_id") {
			mobile = ct.MobileNumber
			break
		}
	}
	if mobile == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "counter not found on this session"})
		return
	}
	otp, err := h.authSvc.GenerateOTP(c.Request.Context(), mobile)
	if err != nil {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": err.Error()})
		return
	}
	if err := h.smsSvc.Send(c.Request.Context(), mobile,
		"Your StockCount OTP is: "+otp+". Valid for 10 minutes."); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to send OTP"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"sent": true})
}

func (h *Handler) PullTheoretical(c *gin.Context) {
	sessionID := c.Param("id")

	// Validate the session and worksheet synchronously so we can return a
	// meaningful error before detaching from the request context.
	if err := h.svc.ValidatePullReady(c.Request.Context(), sessionID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Return 202 immediately — the retail-item catalogue fetch can take > 60 s
	// on large stores and would exceed the browser/proxy request timeout.
	c.JSON(http.StatusAccepted, gin.H{"message": "theoretical pull started — items will be ready shortly"})

	go func() {
		log.Printf("INFO [PullTheoretical] goroutine started for session=%s", sessionID)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if err := h.svc.PullTheoretical(ctx, sessionID); err != nil {
			log.Printf("ERROR [PullTheoretical] goroutine failed for session=%s: %v", sessionID, err)
		}
	}()
}

func (h *Handler) SubmitToLS(c *gin.Context) {
	sessionID := c.Param("id")
	exportPath, err := h.svc.SubmitToLS(c.Request.Context(), sessionID, h.exportDir)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	resp := gin.H{"message": "submitted to LS"}
	if exportPath != "" {
		resp["export_url"] = fmt.Sprintf("/api/v1/sessions/%s/export", sessionID)
	}
	c.JSON(http.StatusOK, resp)
}

func (h *Handler) DownloadExport(c *gin.Context) {
	sessionID := c.Param("id")
	filePath := filepath.Join(h.exportDir, fmt.Sprintf("session-%s.xlsx", sessionID))
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		c.JSON(http.StatusNotFound, gin.H{"error": "export not available yet — submit the session first"})
		return
	}
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="stockcount-%s.xlsx"`, sessionID))
	c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.File(filePath)
}

func (h *Handler) GetCounterSessions(c *gin.Context) {
	counterID := c.GetString("user_id")
	sessions, err := h.svc.GetCounterSessions(c.Request.Context(), counterID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, sessions)
}

func (h *Handler) GetCounterSessionViews(c *gin.Context) {
	counterID := c.GetString("user_id")
	views, err := h.svc.GetCounterSessionViews(c.Request.Context(), counterID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, views)
}

func (h *Handler) GetCounterSessionView(c *gin.Context) {
	counterID := c.GetString("user_id")
	view, err := h.svc.GetCounterSessionView(c.Request.Context(), c.Param("id"), counterID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return
	}
	c.JSON(http.StatusOK, view)
}

func (h *Handler) GetSessionBins(c *gin.Context) {
	counterID := c.GetString("user_id")
	bins, err := h.svc.GetSessionBins(c.Request.Context(), c.Param("id"), counterID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, bins)
}

func (h *Handler) GetSessionItemByBarcode(c *gin.Context) {
	item, err := h.svc.GetSessionItemByBarcode(c.Request.Context(), c.Param("id"), c.Param("barcode"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "item not found in session"})
		return
	}
	c.JSON(http.StatusOK, item)
}

// requireMutableSession fetches the session and returns 409 if it is POSTED or
// ABORTED. Call at the top of any handler that modifies session data. Returns
// a non-nil error (and writes the response) when the check fails.
func (h *Handler) requireMutableSession(c *gin.Context) error {
	sess, err := h.svc.GetSession(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return err
	}
	if sess.Status == StatusPosted || sess.Status == StatusAborted {
		err := fmt.Errorf("session is %s and cannot be modified", sess.Status)
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return err
	}
	return nil
}
