package settings

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// VarianceSetting stores the variance tolerance percentage per stock count type.
// This replaces the old per-session variance_tolerance_pct field.
type VarianceSetting struct {
	ID             string    `json:"id"               gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	StockCountType string    `json:"stock_count_type" gorm:"type:varchar(50);uniqueIndex;not null"`
	TolerancePct   float64   `json:"tolerance_pct"    gorm:"type:numeric(5,2);not null;default:2.00"`
	UpdatedBy      *string   `json:"updated_by"       gorm:"type:uuid"`
	UpdatedAt      time.Time `json:"updated_at"       gorm:"autoUpdateTime"`
}

// defaultTypes are the stock count types that always need a variance setting.
var defaultTypes = []string{
	"FLOOR", "BAKERY", "BUTCHERY", "FRUIT_VEG",
	"DELI_COLD", "DELI_HOT", "QSR", "RESTAURANT", "PARTIAL",
}

// SeedDefaultVarianceSettings inserts the default tolerance rows if they don't exist.
func SeedDefaultVarianceSettings(db *gorm.DB) {
	for _, t := range defaultTypes {
		db.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "stock_count_type"}},
			DoNothing: true,
		}).Create(&VarianceSetting{StockCountType: t, TolerancePct: 2.0})
	}
}

// GetToleranceForType returns the variance tolerance for a given stock count type.
// Falls back to the defaultPct if no setting exists.
func GetToleranceForType(ctx context.Context, db *gorm.DB, stockCountType string, defaultPct float64) float64 {
	var setting VarianceSetting
	if err := db.WithContext(ctx).
		Where("stock_count_type = ?", stockCountType).
		First(&setting).Error; err != nil {
		return defaultPct
	}
	return setting.TolerancePct
}

// Service interface

type Service interface {
	ListVarianceSettings(ctx context.Context) ([]VarianceSetting, error)
	UpdateVarianceSetting(ctx context.Context, stockCountType string, tolerancePct float64, updatedBy string) (*VarianceSetting, error)
}

type service struct{ db *gorm.DB }

func NewService(db *gorm.DB) Service { return &service{db: db} }

func (s *service) ListVarianceSettings(ctx context.Context) ([]VarianceSetting, error) {
	var settings []VarianceSetting
	err := s.db.WithContext(ctx).Order("stock_count_type").Find(&settings).Error
	return settings, err
}

func (s *service) UpdateVarianceSetting(ctx context.Context, stockCountType string, tolerancePct float64, updatedBy string) (*VarianceSetting, error) {
	var setting VarianceSetting
	if err := s.db.WithContext(ctx).
		Where("stock_count_type = ?", stockCountType).
		First(&setting).Error; err != nil {
		return nil, err
	}
	setting.TolerancePct = tolerancePct
	setting.UpdatedBy = &updatedBy
	if err := s.db.WithContext(ctx).Save(&setting).Error; err != nil {
		return nil, err
	}
	return &setting, nil
}

// Handler

type Handler struct{ svc Service }

func NewHandler(svc Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/settings/variance", h.ListVarianceSettings)
	rg.PUT("/settings/variance/:type", h.UpdateVarianceSetting)
}

func (h *Handler) ListVarianceSettings(c *gin.Context) {
	list, err := h.svc.ListVarianceSettings(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, list)
}

func (h *Handler) UpdateVarianceSetting(c *gin.Context) {
	var req struct {
		TolerancePct float64 `json:"tolerance_pct" binding:"required,min=0,max=100"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	adminID := c.GetString("user_id")
	setting, err := h.svc.UpdateVarianceSetting(c.Request.Context(), c.Param("type"), req.TolerancePct, adminID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, setting)
}
