package postgres_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/infrastructure/postgres"
)

func TestStoreAddressBackfillSQL_MatchesMigration(t *testing.T) {
	path := filepath.Join("..", "..", "..", "migrations", "117_create_store_identity.sql")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	re := regexp.MustCompile(`(?s)DO \$backfill\$.*?\$backfill\$;`)
	m := re.Find(raw)
	if m == nil {
		t.Fatal("migration 117 missing DO $backfill$ block")
	}
	norm := func(s string) string {
		s = strings.TrimSpace(s)
		s = strings.TrimSuffix(s, ";")
		return strings.Join(strings.Fields(s), " ")
	}
	got := norm(postgres.StoreAddressBackfillSQL)
	want := norm(string(m))
	if got != want {
		t.Fatalf("StoreAddressBackfillSQL drifted from migrations/117:\n--- const ---\n%s\n--- migration ---\n%s", got, want)
	}
}
