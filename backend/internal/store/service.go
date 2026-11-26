package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Service interface {
	ListStores(ctx context.Context) ([]Store, error)
	GetStore(ctx context.Context, id string) (*Store, error)
	CreateStore(ctx context.Context, s Store) (*Store, error)
	UpdateStore(ctx context.Context, s Store) (*Store, error)
	GetLayout(ctx context.Context, storeID string) ([]Area, []Aisle, []Bin, error)
	CreateArea(ctx context.Context, a Area) (*Area, error)
	CreateAisle(ctx context.Context, a Aisle) (*Aisle, error)
	CreateBin(ctx context.Context, b Bin) (*Bin, error)
	BulkImportLayout(ctx context.Context, storeID string, rows []LayoutImportRow) error
	GetBinByBarcode(ctx context.Context, barcode string) (*Bin, error)
}

type service struct{ db *gorm.DB }

func NewService(db *gorm.DB) Service { return &service{db: db} }

func (s *service) ListStores(ctx context.Context) ([]Store, error) {
	var stores []Store
	err := s.db.WithContext(ctx).Where("active = ?", true).Order("store_name").Find(&stores).Error
	return stores, err
}

func (s *service) GetStore(ctx context.Context, id string) (*Store, error) {
	var st Store
	err := s.db.WithContext(ctx).First(&st, "id = ?", id).Error
	return &st, err
}

// ErrStoreCodeInUse is returned when an active store already has the store code.
var ErrStoreCodeInUse = errors.New("another store already has this store code")

// CreateStore adds a store. A store code that belongs to an inactive store brings that
// store back with the new details (25 Nov 2026): store codes are unique, inactive stores
// included, so adding it again used to fail with a duplicate-key error while the store
// was nowhere in the list.
func (s *service) CreateStore(ctx context.Context, st Store) (*Store, error) {
	var existing Store
	err := s.db.WithContext(ctx).Where("store_code = ?", st.StoreCode).First(&existing).Error
	switch {
	case err == nil && existing.Active:
		return nil, ErrStoreCodeInUse
	case err == nil:
		if err := s.db.WithContext(ctx).Model(&Store{}).Where("id = ?", existing.ID).Updates(map[string]interface{}{
			"store_name":    st.StoreName,
			"ls_store_code": st.LSStoreCode,
			"location_code": st.LocationCode,
			"active":        true,
		}).Error; err != nil {
			return nil, err
		}
		return s.GetStore(ctx, existing.ID)
	case !errors.Is(err, gorm.ErrRecordNotFound):
		return nil, err
	}
	st.Active = true
	err = s.db.WithContext(ctx).Create(&st).Error
	return &st, err
}

// UpdateStore changes the store's editable details only (25 Nov 2026). It used to save the
// whole record from the request, and the edit page does not send "active" or "created_at",
// so every edit deactivated the store (it vanished from the list) and blanked its date.
func (s *service) UpdateStore(ctx context.Context, st Store) (*Store, error) {
	var n int64
	if err := s.db.WithContext(ctx).Model(&Store{}).
		Where("store_code = ? AND id <> ? AND active", st.StoreCode, st.ID).Count(&n).Error; err != nil {
		return nil, err
	}
	if n > 0 {
		return nil, ErrStoreCodeInUse
	}
	res := s.db.WithContext(ctx).Model(&Store{}).Where("id = ?", st.ID).Updates(map[string]interface{}{
		"store_name":    st.StoreName,
		"store_code":    st.StoreCode,
		"ls_store_code": st.LSStoreCode,
		"location_code": st.LocationCode,
	})
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	return s.GetStore(ctx, st.ID)
}

func (s *service) GetLayout(ctx context.Context, storeID string) ([]Area, []Aisle, []Bin, error) {
	var areas []Area
	if err := s.db.WithContext(ctx).Where("store_id = ?", storeID).Order("area_code").Find(&areas).Error; err != nil {
		return nil, nil, nil, err
	}

	var aisles []Aisle
	if err := s.db.WithContext(ctx).
		Joins("JOIN areas ON areas.id = aisles.area_id").
		Where("areas.store_id = ?", storeID).
		Order("aisles.aisle_code").
		Find(&aisles).Error; err != nil {
		return nil, nil, nil, err
	}

	var bins []Bin
	if err := s.db.WithContext(ctx).
		Joins("JOIN aisles ON aisles.id = bins.aisle_id").
		Joins("JOIN areas ON areas.id = aisles.area_id").
		Where("areas.store_id = ? AND bins.active = ?", storeID, true).
		Order("bins.bin_code").
		Find(&bins).Error; err != nil {
		return nil, nil, nil, err
	}

	return areas, aisles, bins, nil
}

func (s *service) CreateArea(ctx context.Context, a Area) (*Area, error) {
	err := s.db.WithContext(ctx).Create(&a).Error
	return &a, err
}

func (s *service) CreateAisle(ctx context.Context, a Aisle) (*Aisle, error) {
	err := s.db.WithContext(ctx).Create(&a).Error
	return &a, err
}

func (s *service) CreateBin(ctx context.Context, b Bin) (*Bin, error) {
	if b.Barcode == "" {
		if b.BinCode != "" {
			b.Barcode = b.BinCode
		} else {
			b.Barcode = fmt.Sprintf("BIN-%s", uuid.New().String()[:8])
		}
	}
	err := s.db.WithContext(ctx).Create(&b).Error
	return &b, err
}

func (s *service) BulkImportLayout(ctx context.Context, storeID string, importRows []LayoutImportRow) error {
	// Fetch store code once — used as a prefix so barcodes are globally unique
	// across stores that share the same layout template.
	// The prefix is NOT printed on labels; the mobile strips it before encoding
	// and looks bins up by bin code within the active session's scope.
	var st Store
	if err := s.db.WithContext(ctx).Select("store_code").First(&st, "id = ?", storeID).Error; err != nil {
		return fmt.Errorf("fetch store: %w", err)
	}
	storePrefix := st.StoreCode

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		areaMap := map[string]string{}
		aisleMap := map[string]string{}

		for _, row := range importRows {
			if _, ok := areaMap[row.AreaCode]; !ok {
				a := Area{StoreID: storeID, AreaCode: row.AreaCode, AreaName: row.AreaName}
				if err := tx.Clauses(clause.OnConflict{
					Columns:   []clause.Column{{Name: "store_id"}, {Name: "area_code"}},
					DoUpdates: clause.AssignmentColumns([]string{"area_name"}),
				}).Create(&a).Error; err != nil {
					return fmt.Errorf("upsert area %s: %w", row.AreaCode, err)
				}
				areaMap[row.AreaCode] = a.ID
			}

			aisleKey := row.AreaCode + "|" + row.AisleCode
			if _, ok := aisleMap[aisleKey]; !ok {
				a := Aisle{AreaID: areaMap[row.AreaCode], AisleCode: row.AisleCode, AisleName: row.AisleName}
				if err := tx.Clauses(clause.OnConflict{
					Columns:   []clause.Column{{Name: "area_id"}, {Name: "aisle_code"}},
					DoUpdates: clause.AssignmentColumns([]string{"aisle_name"}),
				}).Create(&a).Error; err != nil {
					return fmt.Errorf("upsert aisle %s: %w", row.AisleCode, err)
				}
				aisleMap[aisleKey] = a.ID
			}

			// Barcode = "{storeCode}-{binCode}" — store prefix ensures DB uniqueness;
			// label SVG encodes only the bin-code portion (after the first dash).
			b := Bin{AisleID: aisleMap[aisleKey], BinCode: row.BinCode, BinName: row.BinName,
				Barcode: fmt.Sprintf("%s-%s", storePrefix, row.BinCode)}
			if err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "aisle_id"}, {Name: "bin_code"}},
				DoUpdates: clause.AssignmentColumns([]string{"bin_name", "barcode"}),
			}).Create(&b).Error; err != nil {
				return fmt.Errorf("upsert bin %s: %w", row.BinCode, err)
			}
		}
		return nil
	})
}

func (s *service) GetBinByBarcode(ctx context.Context, barcode string) (*Bin, error) {
	var b Bin
	err := s.db.WithContext(ctx).First(&b, "barcode = ?", barcode).Error
	return &b, err
}
