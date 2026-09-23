// Package main is the entry point for the application server.
package main

import (
	"context"
	"flag"
	"log"

	"github.com/fox-gonic/fox"

	"github.com/miclle/routex/internal/routex/config"
	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/handler"
	"github.com/miclle/routex/internal/routex/service"
)

var (
	// CommitID is the git commit hash, injected at build time via ldflags.
	CommitID = "dev"
	// BuildTime is the build timestamp, injected at build time via ldflags.
	BuildTime = ""
)

func main() {
	configPath := flag.String("c", "config.yaml", "path to config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	ctx := context.Background()

	db, err := database.Open(ctx, cfg.Driver, cfg.DSN)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	log.Printf("database connected")

	if err := database.Migrate(ctx, db); err != nil {
		log.Fatalf("migrate database: %v", err)
	}

	svc, err := service.New(ctx, db)
	if err != nil {
		log.Fatalf("init service: %v", err)
	}

	engine := fox.Default()
	ctrl := handler.New(svc)
	ctrl.RegisterRoutes(engine)

	log.Printf("server starting on %s (commit=%s, built=%s)", cfg.Addr, CommitID, BuildTime)
	if err := engine.Run(cfg.Addr); err != nil {
		log.Fatalf("server run: %v", err)
	}
}
