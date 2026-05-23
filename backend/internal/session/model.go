package session

import "time"

type SessionType   string
type SessionStatus string

const (
	// Deprecated – kept only for existing data migration safety
	TypeFull SessionType = "FULL"

	// Current stock count types
	TypeFloor      SessionType = "FLOOR"
	TypeBakery     SessionType = "BAKERY"
	TypeButchery   SessionType = "BUTCHERY"
	TypeFruitVeg   SessionType = "FRUIT_VEG"
	TypeDeliCold   SessionType = "DELI_COLD"
	TypeDeliHot    SessionType = "DELI_HOT"
	TypeQSR        SessionType = "QSR"
	TypeRestaurant SessionType = "RESTAURANT"
	TypePartial    SessionType = "PARTIAL"

	// Session status lifecycle:
	// DRAFT → ACTIVE → PENDING_REVIEW → POSTED
	// DRAFT or ACTIVE → ABORTED
	// POSTED → REOPENED (privileged admin only)
	StatusDraft         SessionStatus = "DRAFT"
	StatusActive        SessionStatus = "ACTIVE"
	StatusPendingReview SessionStatus = "PENDING_REVIEW"
	StatusPosted        SessionStatus = "POSTED"
	StatusReopened      SessionStatus = "REOPENED"
	StatusAborted       SessionStatus = "ABORTED"
)

// SessionTypes returns all valid stock count types for validation and UI.
var SessionTypes = []SessionType{
	TypeFloor, TypeBakery, TypeButchery, TypeFruitVeg,
	TypeDeliCold, TypeDeliHot, TypeQSR, TypeRestaurant, TypePartial,
}

// IsPartial returns true only for PARTIAL sessions (admin-selected item list).
func (t SessionType) IsPartial() bool {
	return t == TypePartial
}

// IsValid checks the type is a known value.
func (t SessionType) IsValid() bool {
	for _, v := range SessionTypes {
		if t == v {
			return true
		}
	}
	return t == TypeFull // Accept legacy FULL during transition
}

// Session represents a stock count session.
// TableName overrides GORM's default ("sessions") to use the renamed table.
type Session struct {
	ID             string        `json:"id"               gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	StoreID        string        `json:"store_id"         gorm:"type:uuid;not null;index"`
	StockCountDate string        `json:"stock_count_date" gorm:"column:session_date;type:date;not null"`
	Type           SessionType   `json:"type"             gorm:"type:varchar(20);not null;default:'FLOOR'"`
	Status         SessionStatus `json:"status"           gorm:"type:varchar(30);not null;default:'DRAFT'"`
	WorksheetNo    *string       `json:"worksheet_no"     gorm:"type:text"`
	// Document number from LS once the session is posted
	DocumentNumber *string       `json:"document_number"  gorm:"type:text"`
	DocumentCheckedAt *time.Time `json:"document_checked_at"`
	// Export file path (set after Excel export is generated on submit)
	ExportFilePath *string       `json:"export_file_path" gorm:"type:text"`
	// Abort fields
	AbortReason *string    `json:"abort_reason"  gorm:"type:text"`
	AbortedAt   *time.Time `json:"aborted_at"`
	AbortedBy   *string    `json:"aborted_by"    gorm:"type:uuid"`
	CreatedBy   string     `json:"created_by"    gorm:"type:uuid;not null"`
	CreatedAt   time.Time  `json:"created_at"`
}

func (Session) TableName() string { return "stock_count_sessions" }

type SessionCounter struct {
	SessionID  string    `json:"session_id"  gorm:"type:uuid;primaryKey"`
	CounterID  string    `json:"counter_id"  gorm:"type:uuid;primaryKey"`
	AssignedAt time.Time `json:"assigned_at" gorm:"autoCreateTime"`
	Active     bool      `json:"active"      gorm:"default:true"`
}

func (SessionCounter) TableName() string { return "session_counters" }

type SessionItem struct {
	SessionID   string  `json:"session_id"  gorm:"type:uuid;primaryKey"`
	ItemNo      string  `json:"item_no"     gorm:"primaryKey"`
	Description string  `json:"description"`
	Barcode     string  `json:"barcode"`
	UoM         string  `json:"uo_m"`
	UnitCost    float64 `json:"unit_cost"   gorm:"type:numeric(14,4);default:0"`
}

func (SessionItem) TableName() string { return "session_items" }

type TheoreticalStock struct {
	SessionID      string  `json:"session_id"      gorm:"type:uuid;primaryKey"`
	ItemNo         string  `json:"item_no"         gorm:"primaryKey"`
	TheoreticalQty float64 `json:"theoretical_qty" gorm:"type:numeric(14,4)"`
	UnitCost       float64 `json:"unit_cost"       gorm:"type:numeric(14,4);default:0"`
}

func (TheoreticalStock) TableName() string { return "theoretical_stocks" }
