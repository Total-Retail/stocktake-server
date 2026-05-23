package variance

import "time"

type FlagStatus string

const (
	StatusPending  FlagStatus = "PENDING"
	StatusAccepted FlagStatus = "ACCEPTED"
	StatusRejected FlagStatus = "REJECTED"
)

type VarianceFlag struct {
	ID        string     `json:"id"         gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	SessionID string     `json:"session_id" gorm:"type:uuid;not null;index"`
	ItemNo    string     `json:"item_no"    gorm:"not null"`
	FlaggedBy string     `json:"flagged_by" gorm:"type:uuid;not null"`
	FlaggedAt time.Time  `json:"flagged_at" gorm:"autoCreateTime"`
	Status    FlagStatus `json:"status"     gorm:"type:varchar(20);default:'PENDING'"`
}

type RecountDecision struct {
	ID         string     `json:"id"          gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	FlagID     string     `json:"flag_id"     gorm:"type:uuid;not null;index"`
	ReviewedBy string     `json:"reviewed_by" gorm:"type:uuid;not null"`
	Decision   FlagStatus `json:"decision"    gorm:"type:varchar(20);not null"`
	Notes      string     `json:"notes"`
	ReviewedAt time.Time  `json:"reviewed_at" gorm:"autoCreateTime"`
}

type ConsolidatedLine struct {
	ItemNo         string  `json:"item_no"`
	Description    string  `json:"description"`
	CountedQty     float64 `json:"counted_qty"`
	TheoreticalQty float64 `json:"theoretical_qty"`
	Variance       float64 `json:"variance"`
	VariancePct    float64 `json:"variance_pct"`
	UnitCost       float64 `json:"unit_cost"`
	VarianceCost   float64 `json:"variance_cost"`
	Flagged        bool    `json:"flagged"`
}

type AuditLine struct {
	ItemNo      string  `json:"item_no"`
	Description string  `json:"description"`
	BinCode     string  `json:"bin_code"`
	CounterName string  `json:"counter_name"`
	Quantity    float64 `json:"quantity"`
	RoundNo     int     `json:"round_no"`
	CountedAt   string  `json:"counted_at"`
}
