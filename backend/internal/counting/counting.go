package counting

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/totalretail/stocktake/internal/ws"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Service interface {
	SubmitBatch(ctx context.Context, sessionID, counterID string, lines []CountLine) error
	SubmitBin(ctx context.Context, sessionID, binID, counterID string) (*BinSubmission, error)
	GetRecounts(ctx context.Context, sessionID, counterID string) ([]CountLine, error)
}

type service struct{ db *gorm.DB }

func NewService(db *gorm.DB) Service { return &service{db: db} }

func (s *service) SubmitBatch(ctx context.Context, sessionID, counterID string, lines []CountLine) error {
	for i := range lines {
		lines[i].SessionID = sessionID
		lines[i].CounterID = counterID
		if lines[i].CountedAt.IsZero() {
			lines[i].CountedAt = time.Now()
		}
	}
	return s.db.WithContext(ctx).
		Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "client_uuid"}}, DoNothing: true}).
		Create(&lines).Error
}

func (s *service) SubmitBin(ctx context.Context, sessionID, binID, counterID string) (*BinSubmission, error) {
	sub := BinSubmission{SessionID: sessionID, BinID: binID, CounterID: counterID}
	return &sub, s.db.WithContext(ctx).Create(&sub).Error
}

func (s *service) GetRecounts(ctx context.Context, sessionID, counterID string) ([]CountLine, error) {
	var lines []CountLine
	err := s.db.WithContext(ctx).
		Joins("JOIN variance_flags ON variance_flags.session_id = count_lines.session_id AND variance_flags.item_no = count_lines.item_no").
		Where("count_lines.session_id = ? AND count_lines.counter_id = ? AND variance_flags.status = ?",
			sessionID, counterID, "PENDING").
		Order("count_lines.counted_at desc").
		Find(&lines).Error
	return lines, err
}

type Handler struct {
	svc Service
	hub *ws.Hub
}

func NewHandler(svc Service, hub *ws.Hub) *Handler { return &Handler{svc: svc, hub: hub} }

func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.POST("/sessions/:id/counts", h.SubmitBatch)
	rg.POST("/sessions/:id/bins/:bin_id/submit", h.SubmitBin)
	rg.GET("/counter/sessions/:id/recounts", h.GetRecounts)
}

func (h *Handler) SubmitBatch(c *gin.Context) {
	var batch CountBatch
	if err := c.ShouldBindJSON(&batch); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	counterID := c.GetString("user_id")
	sessionID := c.Param("id")
	if err := h.svc.SubmitBatch(c.Request.Context(), sessionID, counterID, batch.Lines); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	h.hub.Broadcast(sessionID, ws.Event{
		Type: ws.EventCountSubmitted, SessionID: sessionID,
		Payload: gin.H{"counter_id": counterID, "count": len(batch.Lines)},
	})
	c.JSON(http.StatusOK, gin.H{"synced": len(batch.Lines)})
}

func (h *Handler) SubmitBin(c *gin.Context) {
	sessionID := c.Param("id")
	binID     := c.Param("bin_id")
	counterID := c.GetString("user_id")
	sub, err := h.svc.SubmitBin(c.Request.Context(), sessionID, binID, counterID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	h.hub.Broadcast(sessionID, ws.Event{
		Type: ws.EventBinCompleted, SessionID: sessionID,
		Payload: gin.H{"bin_id": binID, "counter_id": counterID},
	})
	c.JSON(http.StatusOK, sub)
}

func (h *Handler) GetRecounts(c *gin.Context) {
	lines, err := h.svc.GetRecounts(c.Request.Context(), c.Param("id"), c.GetString("user_id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, lines)
}
