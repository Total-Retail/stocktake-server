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
	ID           string    `json:"id"            gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	Name         string    `json:"name"          gorm:"not null"`
	MobileNumber string    `json:"mobile_number" gorm:"uniqueIndex;not null"`
	CreatedAt    time.Time `json:"created_at"`
}
