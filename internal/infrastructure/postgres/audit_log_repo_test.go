package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/admin"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/id"
)

func TestAuditLogRepo_InsertAndList(t *testing.T) {
	db := testDB(t)
	t.Cleanup(func() { db.Exec("DELETE FROM admin_audit_log") })

	repo, err := postgres.NewAuditLogRepo(db)
	if err != nil {
		t.Fatalf("NewAuditLogRepo: %v", err)
	}
	ctx := context.Background()

	record := admin.AuditLogRecord{
		ID:           id.New(),
		AdminID:      "admin-1",
		Action:       "product.update",
		ResourceType: "product",
		ResourceID:   "prod-1",
		Result:       "success",
		StoreID:      "store-eu",
		Language:     "en",
		Currency:     "EUR",
		Metadata:     map[string]interface{}{"name": "Updated"},
		CreatedAt:    time.Now().UTC(),
	}
	if err := repo.Insert(ctx, record); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	entries, err := repo.List(ctx, admin.AuditLogFilter{Offset: 0, Limit: 10, Action: "product.update"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries len = %d, want 1", len(entries))
	}
	got := entries[0]
	if got.StoreID != "store-eu" || got.Language != "en" || got.Currency != "EUR" {
		t.Fatalf("scope triad = %+v", got)
	}
	if got.Metadata["name"] != "Updated" {
		t.Fatalf("metadata = %#v", got.Metadata)
	}
}
