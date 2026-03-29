package db

import (
	"testing"
)

func TestOpen_InvalidDSN(t *testing.T) {
	_, err := Open("postgres://invalid:invalid@localhost:1/nonexistent?sslmode=disable&connect_timeout=1")
	if err == nil {
		t.Fatal("expected error for invalid DSN")
	}
}
