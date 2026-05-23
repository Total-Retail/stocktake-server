package reporting

import (
	"context"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type Service interface {
	GetSessionSummary(ctx context.Context, sessionID string) (*SessionSummary, error)
	GetCounterPerformance(ctx context.Context, sessionID string) ([]CounterPerformance, error)
	GetCounterDetail(ctx context.Context, sessionID, counterID string) (*CounterPerformance, error)
}

type service struct{ db *gorm.DB }

func NewService(db *gorm.DB) Service { return &service{db: db} }

func (s *service) GetCounterPerformance(ctx context.Context, sessionID string) ([]CounterPerformance, error) {
	var results []CounterPerformance
	err := s.db.WithContext(ctx).Raw(`
		SELECT
			c.id                                                                         AS counter_id,
			c.name                                                                       AS counter_name,
			c.mobile_number                                                              AS mobile,
			COUNT(DISTINCT cl.item_no)                                                   AS items_counted,
			COUNT(DISTINCT bs.bin_id)                                                    AS bins_completed,
			COALESCE(ROUND(
				COUNT(DISTINCT CASE WHEN vf.id IS NOT NULL THEN cl.item_no END)::numeric
				/ NULLIF(COUNT(DISTINCT cl.item_no), 0) * 100, 2), 0)                  AS recount_rate,
			COUNT(DISTINCT CASE WHEN rd.decision = 'ACCEPTED' THEN rd.id END)           AS recount_accepted,
			COUNT(DISTINCT CASE WHEN rd.decision = 'REJECTED' THEN rd.id END)           AS recount_rejected,
			COALESCE(MAX(cl.counted_at), NOW())                                         AS last_activity
		FROM counters c
		JOIN session_counters sc ON sc.counter_id = c.id AND sc.session_id = ?
		LEFT JOIN count_lines cl ON cl.counter_id = c.id AND cl.session_id = ?
		LEFT JOIN bin_submissions bs ON bs.counter_id = c.id AND bs.session_id = ?
		LEFT JOIN variance_flags vf ON vf.session_id = ? AND vf.item_no = cl.item_no
		LEFT JOIN recount_decisions rd ON rd.flag_id = vf.id
		GROUP BY c.id, c.name, c.mobile_number
		ORDER BY items_counted DESC`,
		sessionID, sessionID, sessionID, sessionID).Scan(&results).Error
	return results, err
}

func (s *service) GetCounterDetail(ctx context.Context, sessionID, counterID string) (*CounterPerformance, error) {
	all, err := s.GetCounterPerformance(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	for _, p := range all {
		if p.CounterID == counterID {
			return &p, nil
		}
	}
	return nil, fmt.Errorf("counter not found")
}

func (s *service) GetSessionSummary(ctx context.Context, sessionID string) (*SessionSummary, error) {
	var summary SessionSummary
	summary.SessionID = sessionID

	s.db.WithContext(ctx).Raw(`
		SELECT
			COUNT(DISTINCT si.item_no) AS total_items,
			COUNT(DISTINCT cl.id)      AS total_counts
		FROM session_items si
		LEFT JOIN count_lines cl ON cl.session_id = si.session_id
		WHERE si.session_id = ?`, sessionID).Scan(&summary)

	var hourly []HourlyActivity
	s.db.WithContext(ctx).Raw(`
		SELECT counter_id, EXTRACT(HOUR FROM counted_at)::int AS hour, COUNT(*) AS count
		FROM count_lines WHERE session_id = ?
		GROUP BY counter_id, hour ORDER BY hour`, sessionID).Scan(&hourly)
	summary.HourlyActivity = hourly

	counters, err := s.GetCounterPerformance(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	summary.Counters = counters
	return &summary, nil
}

type Handler struct{ svc Service }

func NewHandler(svc Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/sessions/:id/performance", h.GetSessionSummary)
	rg.GET("/sessions/:id/counter-performance", h.GetCounterPerformance)
	rg.GET("/sessions/:id/counter-performance/:counter_id", h.GetCounterDetail)
}

func (h *Handler) GetSessionSummary(c *gin.Context) {
	summary, err := h.svc.GetSessionSummary(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, summary)
}

func (h *Handler) GetCounterPerformance(c *gin.Context) {
	perf, err := h.svc.GetCounterPerformance(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, perf)
}

func (h *Handler) GetCounterDetail(c *gin.Context) {
	p, err := h.svc.GetCounterDetail(c.Request.Context(), c.Param("id"), c.Param("counter_id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "counter not found in session"})
		return
	}
	c.JSON(http.StatusOK, p)
}