package main

import (
	"log"

	"github.com/calliope/api/internal/config"
	"github.com/calliope/api/internal/infra"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	db, err := infra.NewDB(cfg.DB)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		log.Fatalf("failed to get sql.DB: %v", err)
	}
	defer sqlDB.Close()

	log.Printf("Calliope API server starting (env=%s)...", cfg.App.Env)
	// HTTP server will be added in the next module (user auth)
}
