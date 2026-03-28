package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Println("shopanda starting...")

	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	fmt.Println("shopanda ready")
	return nil
}
