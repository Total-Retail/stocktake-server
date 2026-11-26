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
	// DeviceID is the shared device the line was counted on (NFC card sign-in); nil
	// for a counter signed in with phone + OTP. Set by the server, never by the client.
	DeviceID *string `json:"device_id"   gorm:"type:uuid"`
	// AuthMethod is how the counter was identified: "OTP" or "NFC" (internal/devices).
	AuthMethod string `json:"auth_method" gorm:"type:varchar(10);not null;default:'OTP'"`
}

type BinSubmission struct {
	ID          string    `json:"id"           gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	SessionID   string    `json:"session_id"   gorm:"type:uuid;not null;index"`
	BinID       string    `json:"bin_id"       gorm:"type:uuid;not null"`
	CounterID   string    `json:"counter_id"   gorm:"type:uuid;not null"`
	SubmittedAt time.Time `json:"submitted_at" gorm:"autoCreateTime"`
	DeviceID    *string   `json:"device_id"    gorm:"type:uuid"`
	AuthMethod  string    `json:"auth_method"  gorm:"type:varchar(10);not null;default:'OTP'"`
}

// Source is where a submission came from: the device (shared, NFC) or none (OTP).
type Source struct {
	DeviceID   string
	AuthMethod string
}

func (s Source) deviceID() *string {
	if s.DeviceID == "" {
		return nil
	}
	id := s.DeviceID
	return &id
}

func (s Source) method() string {
	if s.AuthMethod == "" {
		return "OTP"
	}
	return s.AuthMethod
}

type CountBatch struct {
	Lines []CountLine `json:"lines"`
}
