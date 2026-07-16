package cron_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/infrastructure/cron"
)

func TestNormalizeSpec_EveryMinutes(t *testing.T) {
	got, err := cron.NormalizeSpec("@every 5m")
	if err != nil {
		t.Fatalf("NormalizeSpec: %v", err)
	}
	if got != "*/5 * * * *" {
		t.Fatalf("got %q", got)
	}
}

func TestNormalizeSpec_EveryHours(t *testing.T) {
	got, err := cron.NormalizeSpec("@every 2h")
	if err != nil {
		t.Fatalf("NormalizeSpec: %v", err)
	}
	if got != "0 */2 * * *" {
		t.Fatalf("got %q", got)
	}
}

func TestNormalizeSpec_StandardCron(t *testing.T) {
	got, err := cron.NormalizeSpec("0 * * * *")
	if err != nil {
		t.Fatalf("NormalizeSpec: %v", err)
	}
	if got != "0 * * * *" {
		t.Fatalf("got %q", got)
	}
}
