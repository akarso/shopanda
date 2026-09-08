package postgres_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/infrastructure/postgres"
)

func TestSearchProductSource_CountAllAndListAll(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	productRepo, err := postgres.NewProductRepo(db)
	if err != nil {
		t.Fatalf("NewProductRepo: %v", err)
	}
	ctx := context.Background()
	for _, name := range []string{"Widget", "Gadget", "Gizmo"} {
		p := mustNewProduct(t, name, name)
		if err := productRepo.Create(ctx, &p); err != nil {
			t.Fatalf("Create(%s): %v", name, err)
		}
	}

	source, err := postgres.NewSearchProductSource(db)
	if err != nil {
		t.Fatalf("NewSearchProductSource: %v", err)
	}

	count, err := source.CountAll(ctx)
	if err != nil {
		t.Fatalf("CountAll: %v", err)
	}
	if count != 3 {
		t.Fatalf("CountAll = %d, want 3", count)
	}

	var seen []string
	const pageSize = 2
	for offset := 0; ; offset += pageSize {
		page, err := source.ListAll(ctx, offset, pageSize)
		if err != nil {
			t.Fatalf("ListAll(offset=%d): %v", offset, err)
		}
		if len(page) == 0 {
			break
		}
		for _, p := range page {
			seen = append(seen, p.ID)
		}
	}
	if len(seen) != 3 {
		t.Fatalf("paged through %d products, want 3", len(seen))
	}
}

func TestSearchProductSource_ListAll_RejectsInvalidArgs(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	source, _ := postgres.NewSearchProductSource(db)
	ctx := context.Background()

	if _, err := source.ListAll(ctx, -1, 10); err == nil {
		t.Error("expected error for negative offset")
	}
	if _, err := source.ListAll(ctx, 0, 0); err == nil {
		t.Error("expected error for non-positive limit")
	}
}

func TestNewSearchProductSource_NilDB(t *testing.T) {
	if _, err := postgres.NewSearchProductSource(nil); err == nil {
		t.Fatal("expected error for nil *sql.DB")
	}
}
