package variance

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Service interface {
	GetConsolidated(ctx context.Context, sessionID string) ([]ConsolidatedLine, error)
	GetAudit(ctx context.Context, sessionID string) ([]AuditLine, error)
	GetVarianceReport(ctx context.Context, sessionID string, tolerancePct float64) ([]ConsolidatedLine, error)
	FlagItems(ctx context.Context, sessionID, flaggedBy string, itemNos []string) error
	UpdateFlag(ctx context.Context, flagID, reviewedBy, decision, notes string) error
	GetFlags(ctx context.Context, sessionID string) ([]VarianceFlag, error)
}

type service struct{ db *gorm.DB }

func NewService(db *gorm.DB) Service { return &service{db: db} }

func (s *service) GetFlags(ctx context.Context, sessionID string) ([]VarianceFlag, error) {
	var flags []VarianceFlag
	err := s.db.WithContext(ctx).
		Where("session_id = ?", sessionID).
		Order("flagged_at desc").
		Find(&flags).Error
	return flags, err
}

func (s *service) GetConsolidated(ctx context.Context, sessionID string) ([]ConsolidatedLine, error) {
	var lines []ConsolidatedLine
	err := s.db.WithContext(ctx).Raw(`
		SELECT
			si.item_no,
			si.description,
			COALESCE(SUM(cl.quantity), 0)                                              AS counted_qty,
			COALESCE(ts.theoretical_qty, 0)                                            AS theoretical_qty,
			COALESCE(SUM(cl.quantity), 0) - COALESCE(ts.theoretical_qty, 0)           AS variance,
			COALESCE(si.unit_cost, 0)                                                  AS unit_cost,
			(COALESCE(SUM(cl.quantity), 0) - COALESCE(ts.theoretical_qty, 0))
				* COALESCE(si.unit_cost, 0)                                            AS variance_cost,
			CASE WHEN COALESCE(ts.theoretical_qty,0) = 0 THEN 0
			     ELSE ROUND(((COALESCE(SUM(cl.quantity),0) - COALESCE(ts.theoretical_qty,0))
			          / ts.theoretical_qty * 100)::numeric, 2)
			END                                                                        AS variance_pct,
			EXISTS(
				SELECT 1 FROM variance_flags vf
				WHERE vf.session_id = ? AND vf.item_no = si.item_no AND vf.status = 'PENDING'
			)                                                                          AS flagged
		FROM session_items si
		LEFT JOIN count_lines cl
			ON cl.session_id = si.session_id AND cl.item_no = si.item_no
			AND cl.round_no = (
				SELECT MAX(round_no) FROM count_lines
				WHERE session_id = si.session_id AND item_no = si.item_no
			)
		LEFT JOIN theoretical_stocks ts
			ON ts.session_id = si.session_id AND ts.item_no = si.item_no
		WHERE si.session_id = ?
		GROUP BY si.item_no, si.description, ts.theoretical_qty
		ORDER BY si.item_no`, sessionID, sessionID).Scan(&lines).Error
	return lines, err
}

func (s *service) GetAudit(ctx context.Context, sessionID string) ([]AuditLine, error) {
	var lines []AuditLine
	err := s.db.WithContext(ctx).Raw(`
		SELECT si.item_no, si.description, b.bin_code, c.name AS counter_name,
		       cl.quantity, cl.round_no, cl.counted_at
		FROM count_lines cl
		JOIN session_items si ON si.session_id = cl.session_id AND si.item_no = cl.item_no
		JOIN bins b ON b.id = cl.bin_id
		JOIN counters c ON c.id = cl.counter_id
		WHERE cl.session_id = ?
		ORDER BY si.item_no, cl.round_no, cl.counted_at`, sessionID).Scan(&lines).Error
	return lines, err
}

func (s *service) GetVarianceReport(ctx context.Context, sessionID string, tolerancePct float64) ([]ConsolidatedLine, error) {
	all, err := s.GetConsolidated(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	var flagged []ConsolidatedLine
	for _, l := range all {
		if l.VariancePct > tolerancePct || l.VariancePct < -tolerancePct {
			flagged = append(flagged, l)
		}
	}
	return flagged, nil
}

func (s *service) FlagItems(ctx context.Context, sessionID, flaggedBy string, itemNos []string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, itemNo := range itemNos {
			flag := VarianceFlag{SessionID: sessionID, ItemNo: itemNo, FlaggedBy: flaggedBy, Status: StatusPending}
			if err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "session_id"}, {Name: "item_no"}},
				DoUpdates: clause.Assignments(map[string]interface{}{"status": "PENDING", "flagged_by": flaggedBy, "flagged_at": gorm.Expr("NOW()")}),
			}).Create(&flag).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *service) UpdateFlag(ctx context.Context, flagID, reviewedBy, decision, notes string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&VarianceFlag{}).Where("id = ?", flagID).Update("status", decision).Error; err != nil {
			return err
		}
		return tx.Create(&RecountDecision{
			FlagID: flagID, ReviewedBy: reviewedBy,
			Decision: FlagStatus(decision), Notes: notes,
		}).Error
	})
}

// Handler

type Handler struct {
	svc          Service
	tolerancePct float64
}

func NewHandler(svc Service, tolerancePct float64) *Handler {
	return &Handler{svc: svc, tolerancePct: tolerancePct}
}

func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/sessions/:id/consolidated", h.GetConsolidated)
	rg.GET("/sessions/:id/audit", h.GetAudit)
	rg.GET("/sessions/:id/variance-report", h.GetVarianceReport)
	rg.POST("/sessions/:id/variance-flags", h.FlagItems)
	rg.PUT("/sessions/:id/variance-flags/:flag_id", h.UpdateFlag)
	rg.GET("/sessions/:id/variance-flags", h.GetFlags)
}

func (h *Handler) GetFlags(c *gin.Context) {
	flags, err := h.svc.GetFlags(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, flags)
}

func (h *Handler) GetConsolidated(c *gin.Context) {
	lines, err := h.svc.GetConsolidated(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, lines)
}

func (h *Handler) GetAudit(c *gin.Context) {
	lines, err := h.svc.GetAudit(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, lines)
}

func (h *Handler) GetVarianceReport(c *gin.Context) {
	lines, err := h.svc.GetVarianceReport(c.Request.Context(), c.Param("id"), h.tolerancePct)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, lines)
}

func (h *Handler) FlagItems(c *gin.Context) {
	var req struct {
		ItemNos []string `json:"item_nos" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.svc.FlagItems(c.Request.Context(), c.Param("id"), c.GetString("user_id"), req.ItemNos); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"flagged": len(req.ItemNos)})
}

func (h *Handler) UpdateFlag(c *gin.Context) {
	var req struct {
		Decision string `json:"decision" binding:"required"`
		Notes    string `json:"notes"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.svc.UpdateFlag(c.Request.Context(), c.Param("flag_id"), c.GetString("user_id"), req.Decision, req.Notes); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"updated": true})
}
