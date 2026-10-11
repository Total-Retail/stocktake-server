package main

import (
	"log"

	"github.com/totalretail/stocktake/internal/auth"
	"github.com/totalretail/stocktake/internal/config"
	"github.com/totalretail/stocktake/internal/db"
	"github.com/totalretail/stocktake/internal/schema"
	"github.com/totalretail/stocktake/internal/server"
	"github.com/totalretail/stocktake/internal/settings"
	"golang.org/x/crypto/bcrypt"
)

func main() {
	cfg := config.Load()

	database, err := db.Connect(cfg)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}

	// Schema renames, then AutoMigrate of every model (internal/schema).
	if err := schema.Migrate(database); err != nil {
		log.Fatalf("schema migration failed: %v", err)
	}

	// Seed default admin if none exists
	var count int64
	database.Model(&auth.AdminUser{}).Count(&count)
	if count == 0 {
		hash, _ := bcrypt.GenerateFromPassword([]byte("admin123"), bcrypt.DefaultCost)
		database.Create(&auth.AdminUser{
			Username:     "admin",
			PasswordHash: string(hash),
			FullName:     "System Administrator",
			Active:       true,
		})
		log.Println("Created default admin user: admin / admin123 — change this password immediately")
	}

	// Seed default variance settings if none exist
	settings.SeedDefaultVarianceSettings(database)

	srv := server.New(cfg, database)
	log.Printf("starting stockcount-api on %s", cfg.ServerAddr)
	if err := srv.Run(); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
