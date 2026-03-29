package main

import (
	"fmt"
	"os"

	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/logger"
)

func main() {
	fmt.Println("shopanda starting...")

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
	log.Info("app.ready", nil)
	return nil
}
