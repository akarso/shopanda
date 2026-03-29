package main

import (
	"fmt"
	"os"

	"github.com/shopanda/shopanda/internal/platform/config"
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

	fmt.Println("config:", cfg)
	fmt.Println("shopanda ready")
	return nil
}
