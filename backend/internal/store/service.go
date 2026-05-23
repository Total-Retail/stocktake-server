package store

import (
	"context"
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

func (s *service) CreateStore(ctx context.Context, st Store) (*Store, error) {
	err := s.db.WithContext(ctx).Create(&st).Error
	return &st, err
}

func (s *service) UpdateStore(ctx context.Context, st Store) (*Store, error) {
	err := s.db.WithContext(ctx).Save(&st).Error
	return &st, err
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
		b.Barcode = fmt.Sprintf("BIN-%s", uuid.New().String()[:8])
	}
	err := s.db.WithContext(ctx).Create(&b).Error
	return &b, err
}

func (s *service) BulkImportLayout(ctx context.Context, storeID string, importRows []LayoutImportRow) error {
	// Fetch the store code once so barcodes are globally unique across stores.
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

			// Include store prefix so barcodes are unique even when multiple stores
			// share the same layout template (same aisle/bin codes).
			barcode := fmt.Sprintf("%s-%s-%s", storePrefix, row.AisleCode, row.BinCode)
			b := Bin{AisleID: aisleMap[aisleKey], BinCode: row.BinCode, BinName: row.BinName, Barcode: barcode}
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
