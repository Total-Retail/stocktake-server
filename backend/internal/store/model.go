package store

import "time"

type Store struct {
	ID           string    `json:"id"            gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	StoreCode    string    `json:"store_code"    gorm:"uniqueIndex;not null"`
	StoreName    string    `json:"store_name"    gorm:"not null"`
	LSStoreCode  string    `json:"ls_store_code" gorm:"not null"`
	LocationCode string    `json:"location_code" gorm:"type:text"`
	Active       bool      `json:"active"        gorm:"default:true"`
	CreatedAt    time.Time `json:"created_at"`
}

// Area was formerly Zone. TableName is explicit so GORM finds the renamed table.
type Area struct {
	ID        string    `json:"id"         gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	StoreID   string    `json:"store_id"   gorm:"type:uuid;not null;uniqueIndex:idx_areas_store_area"`
	AreaCode  string    `json:"area_code"  gorm:"not null;uniqueIndex:idx_areas_store_area"`
	AreaName  string    `json:"area_name"  gorm:"not null"`
	CreatedAt time.Time `json:"created_at"`
}

func (Area) TableName() string { return "areas" }

type Aisle struct {
	ID        string    `json:"id"         gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	AreaID    string    `json:"area_id"    gorm:"type:uuid;not null;uniqueIndex:idx_aisles_area_aisle"`
	AisleCode string    `json:"aisle_code" gorm:"not null;uniqueIndex:idx_aisles_area_aisle"`
	AisleName string    `json:"aisle_name" gorm:"not null"`
	CreatedAt time.Time `json:"created_at"`
}

// Bin was formerly Bay. TableName is explicit so GORM finds the renamed table.
type Bin struct {
	ID        string    `json:"id"         gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	AisleID   string    `json:"aisle_id"   gorm:"type:uuid;not null;uniqueIndex:idx_bins_aisle_bin"`
	BinCode   string    `json:"bin_code"   gorm:"not null;uniqueIndex:idx_bins_aisle_bin"`
	BinName   string    `json:"bin_name"   gorm:"not null"`
	Barcode   string    `json:"barcode"    gorm:"uniqueIndex;not null"`
	Active    bool      `json:"active"     gorm:"default:true"`
	CreatedAt time.Time `json:"created_at"`
}

func (Bin) TableName() string { return "bins" }

// LayoutImportRow is used for CSV bulk-import of the store layout.
// CSV headers must match the lowercase snake_case field names.
type LayoutImportRow struct {
	AreaCode  string
	AreaName  string
	AisleCode string
	AisleName string
	BinCode   string
	BinName   string
}
