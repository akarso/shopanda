package main

import (
	"fmt"
	"os"

	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/db"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/migrate"

	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load(config.FindConfigFile())
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	log := logger.New(cfg.Log.Level)

	log.Info("app.config.loaded", map[string]interface{}{
		"config": cfg.String(),
	})

	// Subcommand dispatch.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "migrate":
			return runMigrate(cfg, log)
		case "serve":
			return runServe(cfg, log)
		default:
			return fmt.Errorf("unknown command: %s", os.Args[1])
		}
	}

	// Default: start HTTP server.
	return runServe(cfg, log)
}

func runServe(cfg *config.Config, log logger.Logger) error {
	router := shophttp.NewRouter()

	// Middleware: outermost first.
	router.Use(shophttp.RecoveryMiddleware(log))
	router.Use(shophttp.RequestIDMiddleware())
	router.Use(shophttp.LoggingMiddleware(log))

	// Routes.
	router.HandleFunc("GET /healthz", shophttp.HealthHandler())

	srv := shophttp.NewServer(cfg.Server.Host, cfg.Server.Port, router.Handler(), log)
	return srv.ListenAndServe()
}

func runMigrate(cfg *config.Config, log logger.Logger) error {
	dsn := config.DatabaseDSN(cfg)
	conn, err := db.Open(dsn)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer conn.Close()

	log.Info("migrate.start", nil)

	applied, err := migrate.Run(conn, "migrations")
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	log.Info("migrate.complete", map[string]interface{}{
		"applied": applied,
	})
	return nil
}
