package counting

import "time"

type CountLine struct {
	ID         string    `json:"id"          gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	SessionID  string    `json:"session_id"  gorm:"type:uuid;not null;index"`
	BinID      string    `json:"bin_id"      gorm:"type:uuid;not null;index"`
	ItemNo     string    `json:"item_no"     gorm:"not null;index"`
	CounterID  string    `json:"counter_id"  gorm:"type:uuid;not null;index"`
	Quantity   float64   `json:"quantity"    gorm:"type:numeric(14,4);not null"`
	CountedAt  time.Time `json:"counted_at"`
	SyncedAt   time.Time `json:"synced_at"   gorm:"autoCreateTime"`
	RoundNo    int       `json:"round_no"    gorm:"default:0"`
	ClientUUID string    `json:"client_uuid" gorm:"type:uuid;uniqueIndex"`
}

type BinSubmission struct {
	ID          string    `json:"id"           gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	SessionID   string    `json:"session_id"   gorm:"type:uuid;not null;index"`
	BinID       string    `json:"bin_id"       gorm:"type:uuid;not null"`
	CounterID   string    `json:"counter_id"   gorm:"type:uuid;not null"`
	SubmittedAt time.Time `json:"submitted_at" gorm:"autoCreateTime"`
}

type CountBatch struct {
	Lines []CountLine `json:"lines"`
}
