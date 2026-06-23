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

	baseTS := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	records := []admin.AuditLogRecord{
		{
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
			CreatedAt:    baseTS.Add(2 * time.Hour),
		},
		{
			ID:           id.New(),
			AdminID:      "admin-1",
			Action:       "product.update",
			ResourceType: "product",
			ResourceID:   "prod-2",
			Result:       "success",
			CreatedAt:    baseTS.Add(1 * time.Hour),
		},
		{
			ID:           id.New(),
			AdminID:      "admin-1",
			Action:       "product.update",
			ResourceType: "product",
			ResourceID:   "prod-3",
			Result:       "success",
			CreatedAt:    baseTS,
		},
	}
	for _, record := range records {
		if err := repo.Insert(ctx, record); err != nil {
			t.Fatalf("Insert(%s): %v", record.ResourceID, err)
		}
	}

	filter := admin.AuditLogFilter{Offset: 0, Limit: 10, Action: "product.update"}
	entries, err := repo.List(ctx, filter)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("entries len = %d, want 3", len(entries))
	}
	got := entries[0]
	if got.ResourceID != "prod-1" {
		t.Fatalf("first entry resource_id = %q, want prod-1", got.ResourceID)
	}
	if got.StoreID != "store-eu" || got.Language != "en" || got.Currency != "EUR" {
		t.Fatalf("scope triad = %+v", got)
	}
	if got.Metadata["name"] != "Updated" {
		t.Fatalf("metadata = %#v", got.Metadata)
	}

	var pagedIDs []string
	for offset := 0; offset < len(records); offset++ {
		page, err := repo.List(ctx, admin.AuditLogFilter{
			Action: "product.update",
			Offset: offset,
			Limit:  1,
		})
		if err != nil {
			t.Fatalf("List page offset=%d: %v", offset, err)
		}
		if len(page) != 1 {
			t.Fatalf("page offset=%d len = %d, want 1", offset, len(page))
		}
		pagedIDs = append(pagedIDs, page[0].ID)
	}
	if len(pagedIDs) != len(records) {
		t.Fatalf("paged IDs len = %d, want %d", len(pagedIDs), len(records))
	}
	seen := make(map[string]struct{}, len(pagedIDs))
	for _, pageID := range pagedIDs {
		if _, ok := seen[pageID]; ok {
			t.Fatalf("duplicate ID across pages: %s", pageID)
		}
		seen[pageID] = struct{}{}
	}

	first, err := repo.List(ctx, filter)
	if err != nil {
		t.Fatalf("List repeat: %v", err)
	}
	second, err := repo.List(ctx, filter)
	if err != nil {
		t.Fatalf("List repeat: %v", err)
	}
	for i := range first {
		if first[i].ID != second[i].ID {
			t.Fatalf("order changed between calls at index %d: %s vs %s", i, first[i].ID, second[i].ID)
		}
	}
}
