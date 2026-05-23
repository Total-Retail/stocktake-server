package db

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
)

// RunSchemaRenames performs idempotent one-time schema renames.
// Must be called BEFORE AutoMigrate in main.go so that GORM sees the new table
// names and only applies additive column changes (not recreate tables).
func RunSchemaRenames(db *gorm.DB) error {
	steps := []struct {
		sql  string
		desc string
	}{
		// ── Table renames ─────────────────────────────────────────────────────
		{"ALTER TABLE IF EXISTS zones RENAME TO areas", "zones → areas"},
		{"ALTER TABLE IF EXISTS bays RENAME TO bins", "bays → bins"},
		{"ALTER TABLE IF EXISTS sessions RENAME TO stock_count_sessions", "sessions → stock_count_sessions"},

		// ── Column renames on areas (was zones) ───────────────────────────────
		{"ALTER TABLE IF EXISTS areas RENAME COLUMN zone_code TO area_code", "areas: zone_code → area_code"},
		{"ALTER TABLE IF EXISTS areas RENAME COLUMN zone_name TO area_name", "areas: zone_name → area_name"},

		// ── FK column on aisles (was zone_id) ────────────────────────────────
		{"ALTER TABLE IF EXISTS aisles RENAME COLUMN zone_id TO area_id", "aisles: zone_id → area_id"},

		// ── Column renames on bins (was bays) ─────────────────────────────────
		{"ALTER TABLE IF EXISTS bins RENAME COLUMN bay_code TO bin_code", "bins: bay_code → bin_code"},
		{"ALTER TABLE IF EXISTS bins RENAME COLUMN bay_name TO bin_name", "bins: bay_name → bin_name"},

		// ── FK column renames on count_lines and bin_submissions ──────────────
		{"ALTER TABLE IF EXISTS count_lines RENAME COLUMN bay_id TO bin_id", "count_lines: bay_id → bin_id"},
		{"ALTER TABLE IF EXISTS bin_submissions RENAME COLUMN bay_id TO bin_id", "bin_submissions: bay_id → bin_id"},

		// ── Store location code ───────────────────────────────────────────────
		{"ALTER TABLE IF EXISTS stores ADD COLUMN IF NOT EXISTS location_code TEXT", "stores: add location_code"},

		// ── Composite unique indexes required by ON CONFLICT upserts ──────────
		// The original SQL migration named constraints after zone_code/bay_code.
		// Creating them explicitly with IF NOT EXISTS ensures upserts always work.
		{"CREATE UNIQUE INDEX IF NOT EXISTS idx_areas_store_area  ON areas(store_id, area_code)", "areas: unique(store_id, area_code)"},
		{"CREATE UNIQUE INDEX IF NOT EXISTS idx_aisles_area_aisle ON aisles(area_id, aisle_code)", "aisles: unique(area_id, aisle_code)"},
		{"CREATE UNIQUE INDEX IF NOT EXISTS idx_bins_aisle_bin    ON bins(aisle_id, bin_code)", "bins: unique(aisle_id, bin_code)"},

		// ── Drop old partial-unique index (references old status values) ──────
		{"DROP INDEX IF EXISTS idx_sessions_store_type_active", "drop stale session unique index"},
	}

	for _, step := range steps {
		if err := db.Exec(step.sql).Error; err != nil {
			// Ignore "does not exist" — means the rename already ran
			if strings.Contains(err.Error(), "does not exist") ||
				strings.Contains(err.Error(), "already exists") {
				continue
			}
			return fmt.Errorf("schema rename [%s]: %w", step.desc, err)
		}
	}
	return nil
}
