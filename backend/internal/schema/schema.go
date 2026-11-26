// Package schema creates and upgrades the database schema: the idempotent renames,
// then GORM AutoMigrate of every model. main runs it at start-up; the database
// tests run the same thing against a scratch database.
package schema

import (
	"github.com/totalretail/stocktake/internal/auth"
	"github.com/totalretail/stocktake/internal/counting"
	"github.com/totalretail/stocktake/internal/db"
	"github.com/totalretail/stocktake/internal/devices"
	"github.com/totalretail/stocktake/internal/session"
	"github.com/totalretail/stocktake/internal/settings"
	"github.com/totalretail/stocktake/internal/store"
	"github.com/totalretail/stocktake/internal/variance"
	"gorm.io/gorm"
)

// Migrate brings the schema up to date. Additive only: AutoMigrate adds tables and
// columns, it never drops or renames (the renames are db.RunSchemaRenames).
func Migrate(database *gorm.DB) error {
	database.Exec("CREATE EXTENSION IF NOT EXISTS pgcrypto")
	if err := db.RunSchemaRenames(database); err != nil {
		return err
	}
	if err := database.AutoMigrate(
		&store.Store{},
		&store.Area{},
		&store.Aisle{},
		&store.Bin{},
		&auth.AdminUser{},
		&auth.Counter{},
		&session.Session{},
		&session.SessionItem{},
		&session.SessionCounter{},
		&session.TheoreticalStock{},
		&counting.CountLine{},
		&counting.BinSubmission{},
		&variance.VarianceFlag{},
		&variance.RecountDecision{},
		&settings.VarianceSetting{},
		&devices.Device{},
		&devices.CounterTag{},
	); err != nil {
		return err
	}
	return devices.EnsureIndexes(database)
}
