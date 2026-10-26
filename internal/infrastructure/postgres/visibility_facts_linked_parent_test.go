package postgres_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
)

func TestVisibilityFactsRepo_RequiresLinkedParent(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	ctx := context.Background()
	prodRepo, err := postgres.NewProductRepo(db)
	if err != nil {
		t.Fatalf("NewProductRepo: %v", err)
	}
	facts, err := postgres.NewVisibilityFactsRepo(db)
	if err != nil {
		t.Fatalf("NewVisibilityFactsRepo: %v", err)
	}

	prod := mustNewProduct(t, "Linked Facts", "linked-facts")
	prod.RequiresLinkedParent = true
	modes := catalog.DefaultVisibilityAxes()
	modes.Catalog = catalog.VisibilityModeVisible
	modes.Purchasable = catalog.VisibilityModeVisible
	prod.VisibilityModes = modes
	if err := prodRepo.Create(productRepoAdminCtx(), &prod); err != nil {
		t.Fatalf("Create: %v", err)
	}

	snaps, err := facts.LoadManyAnyStatus(ctx, []string{prod.ID})
	if err != nil {
		t.Fatalf("LoadManyAnyStatus: %v", err)
	}
	snap, ok := snaps[prod.ID]
	if !ok {
		t.Fatal("snapshot missing")
	}
	if !snap.RequiresLinkedParent {
		t.Fatal("RequiresLinkedParent = false, want true")
	}
	resolved := snap.Resolve(catalog.VisibilityOptions{})
	if !resolved.VisibleInCatalog {
		t.Fatal("VisibleInCatalog = false, want true")
	}
	if resolved.Purchasable {
		t.Fatal("Purchasable = true, want false while assignment context is empty")
	}
}
