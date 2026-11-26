package devices

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/totalretail/stocktake/internal/auth"
	"gorm.io/gorm"
)

// CtxDevice is the gin context key the device middleware stores the *Device under.
const CtxDevice = "device"

// TokenIssuer issues device tokens (auth.Service).
type TokenIssuer interface {
	IssueDeviceToken(deviceID string, version, expiryDays int) (string, error)
}

type Handler struct {
	db              *gorm.DB
	tokens          TokenIssuer
	deviceTokenDays int
}

func NewHandler(db *gorm.DB, tokens TokenIssuer, deviceTokenDays int) *Handler {
	return &Handler{db: db, tokens: tokens, deviceTokenDays: deviceTokenDays}
}

// RegisterAdminRoutes: enrolling and managing devices, and linking cards (admin token).
func (h *Handler) RegisterAdminRoutes(rg *gin.RouterGroup) {
	rg.GET("/devices", h.ListDevices)
	rg.POST("/devices", h.Enrol)
	rg.PUT("/devices/:device_id", h.UpdateDevice)
	rg.POST("/devices/:device_id/revoke", h.RevokeDevice)

	rg.GET("/counters", h.ListCounters)
	rg.POST("/counters", h.CreateCounter)
	rg.PUT("/counters/:counter_id", h.UpdateCounter)
	rg.PUT("/counters/:counter_id/tag", h.LinkTag)
	rg.DELETE("/counters/:counter_id/tag", h.UnlinkTag)
	rg.GET("/tags/:uid", h.TagOwner)
}

// RegisterDeviceRoutes: what a shared device does before a counter is tapped in
// (device token, mounted under /device).
func (h *Handler) RegisterDeviceRoutes(rg *gin.RouterGroup) {
	rg.GET("/me", h.Me)
	rg.GET("/roster", h.Roster)
	rg.POST("/identify", h.Identify)
}

func fail(c *gin.Context, err error) {
	var inUse *TagInUseError
	switch {
	case errors.As(err, &inUse):
		c.JSON(http.StatusConflict, gin.H{"error": inUse.Error(), "code": "tag_in_use",
			"counter_id": inUse.CounterID, "counter_name": inUse.CounterName})
	case errors.Is(err, ErrBadUID):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "bad_uid"})
	case errors.Is(err, ErrTagUnknown):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error(), "code": "tag_unknown"})
	case errors.Is(err, ErrNotOnCount):
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error(), "code": "not_on_count"})
	case errors.Is(err, ErrDeviceRevoked):
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error(), "code": "device_revoked"})
	case errors.Is(err, ErrBadInput):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "bad_input"})
	case errors.Is(err, ErrMobileInUse):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error(), "code": "mobile_in_use"})
	case errors.Is(err, ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	}
}

// ── Admin ────────────────────────────────────────────────────────────────────

func (h *Handler) ListDevices(c *gin.Context) {
	out, err := ListDevices(c.Request.Context(), h.db)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// Enrol is called by a supervisor signed in on the device itself; the token in the
// answer is what the device then counts with.
func (h *Handler) Enrol(c *gin.Context) {
	var req EnrolRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	d, err := Enrol(c.Request.Context(), h.db, req, c.GetString("user_id"))
	if err != nil {
		fail(c, err)
		return
	}
	token, err := h.tokens.IssueDeviceToken(d.ID, d.TokenVersion, h.deviceTokenDays)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to issue device token"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"device": d, "token": token})
}

// UpdateDevice takes {"name"} and/or {"store_id"}; a store_id left out is left as it is,
// "store_id": null means any store.
func (h *Handler) UpdateDevice(c *gin.Context) {
	var raw map[string]json.RawMessage
	if err := c.ShouldBindJSON(&raw); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	var name string
	var storeID *string
	if v, ok := raw["name"]; ok {
		json.Unmarshal(v, &name)
	}
	_, setStore := raw["store_id"]
	if setStore {
		json.Unmarshal(raw["store_id"], &storeID)
	}
	d, err := UpdateDevice(c.Request.Context(), h.db, c.Param("device_id"), name, setStore, storeID)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, d)
}

func (h *Handler) RevokeDevice(c *gin.Context) {
	if err := RevokeDevice(c.Request.Context(), h.db, c.Param("device_id"), c.GetString("user_id")); err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"revoked": true})
}

func (h *Handler) ListCounters(c *gin.Context) {
	out, err := ListCounters(c.Request.Context(), h.db, c.Query("session_id"), c.Query("q"))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// CreateCounter adds a person on the Staff page (or updates the one with that number).
func (h *Handler) CreateCounter(c *gin.Context) {
	var req struct {
		Name           string `json:"name" binding:"required"`
		Mobile         string `json:"mobile" binding:"required"`
		CanPrintLabels bool   `json:"can_print_labels"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	out, err := CreateCounter(c.Request.Context(), h.db, req.Name, auth.NormalizeMobile(req.Mobile), req.CanPrintLabels)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, out)
}

// UpdateCounter changes a person's name, number or label-printing permission.
func (h *Handler) UpdateCounter(c *gin.Context) {
	var req CounterInput
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	out, err := UpdateCounter(c.Request.Context(), h.db, c.Param("counter_id"), req, auth.NormalizeMobile)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handler) LinkTag(c *gin.Context) {
	var req struct {
		UID   string `json:"uid" binding:"required"`
		Label string `json:"label"`
		Force bool   `json:"force"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	out, err := LinkTag(c.Request.Context(), h.db, c.Param("counter_id"), req.UID, req.Label, c.GetString("user_id"), req.Force)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handler) UnlinkTag(c *gin.Context) {
	if err := UnlinkTag(c.Request.Context(), h.db, c.Param("counter_id"), c.GetString("user_id")); err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"unlinked": true})
}

func (h *Handler) TagOwner(c *gin.Context) {
	uid, err := NormalizeUID(c.Param("uid"))
	if err != nil {
		fail(c, err)
		return
	}
	out, err := ActiveTagOwner(c.Request.Context(), h.db, uid)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// ── Device ───────────────────────────────────────────────────────────────────

func device(c *gin.Context) *Device {
	d, _ := c.MustGet(CtxDevice).(*Device)
	return d
}

func (h *Handler) Me(c *gin.Context) {
	d := device(c)
	var storeName *string
	if d.StoreID != nil {
		var row struct{ StoreName string }
		h.db.WithContext(c.Request.Context()).Raw(`SELECT store_name FROM stores WHERE id = ?`, *d.StoreID).Scan(&row)
		storeName = &row.StoreName
	}
	c.JSON(http.StatusOK, gin.H{"device": d, "store_name": storeName})
}

func (h *Handler) Roster(c *gin.Context) {
	out, err := Roster(c.Request.Context(), h.db, device(c))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handler) Identify(c *gin.Context) {
	var req struct {
		UID string `json:"uid" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	out, err := Identify(c.Request.Context(), h.db, device(c), req.UID)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}
