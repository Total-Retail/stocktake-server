package auth

import "time"

type AdminUser struct {
	ID           string    `json:"id"             gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	Username     string    `json:"username"       gorm:"uniqueIndex;not null"`
	PasswordHash string    `json:"-"              gorm:"not null"`
	FullName     string    `json:"full_name"      gorm:"not null"`
	Active       bool      `json:"active"         gorm:"default:true"`
	IsSuperAdmin bool      `json:"is_super_admin" gorm:"default:false"`
	CreatedAt    time.Time `json:"created_at"`
}

type Counter struct {
	ID           string `json:"id"            gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	Name         string `json:"name"          gorm:"not null"`
	MobileNumber string `json:"mobile_number" gorm:"uniqueIndex;not null"`
	// CanPrintLabels (24 Nov 2026) lets the person use Label Printing in the Vantage Suite
	// app — scanning items onto a Business Central label worksheet — and sign in for it
	// even when they are not on a stock count. Set by an admin on the portal's Staff page.
	CanPrintLabels bool      `json:"can_print_labels" gorm:"not null;default:false"`
	CreatedAt      time.Time `json:"created_at"`
}
