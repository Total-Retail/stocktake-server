// Package testdb gives database tests a fresh, migrated PostgreSQL database.
// Set STOCKTAKE_TEST_DSN to a server the tests may create databases on, e.g.
// postgres://postgres@localhost:5432/postgres?sslmode=disable; without it the
// database tests are skipped.
package testdb

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/totalretail/stocktake/internal/schema"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// New creates a scratch database, migrates it and drops it when the test ends.
func New(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("STOCKTAKE_TEST_DSN")
	if dsn == "" {
		t.Skip("STOCKTAKE_TEST_DSN not set: database test skipped")
	}
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	name := fmt.Sprintf("st_test_%d", time.Now().UnixNano())
	if err := admin.Exec("CREATE DATABASE " + name).Error; err != nil {
		t.Fatalf("create database: %v", err)
	}
	db, err := gorm.Open(postgres.Open(withDB(dsn, name)), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("connect scratch: %v", err)
	}
	if err := schema.Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
		admin.Exec("DROP DATABASE IF EXISTS " + name)
		if sqlDB, err := admin.DB(); err == nil {
			sqlDB.Close()
		}
	})
	return db
}

// withDB swaps the database name in a postgres:// URL.
func withDB(dsn, name string) string {
	q := ""
	if i := strings.Index(dsn, "?"); i >= 0 {
		dsn, q = dsn[:i], dsn[i:]
	}
	if i := strings.LastIndex(dsn, "/"); i > len("postgres://") {
		dsn = dsn[:i]
	}
	return dsn + "/" + name + q
}
