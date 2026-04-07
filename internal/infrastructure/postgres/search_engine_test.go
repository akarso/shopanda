package postgres_test

import (
	"context"
	"testing"

	"fmt"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/search"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/id"
)

func TestSearchEngine_Name(t *testing.T) {
	db := testDB(t)
	engine := postgres.NewSearchEngine(db)
	if got := engine.Name(); got != "postgres" {
		t.Errorf("Name() = %q, want %q", got, "postgres")
	}
}

func TestSearchEngine_IndexProduct(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	engine := postgres.NewSearchEngine(db)
	ctx := context.Background()

	// Create a product first.
	p := mustNewProduct(t, "Indexable Shoe", "indexable-shoe-"+id.New()[:8])
	repo := postgres.NewProductRepo(db)
	if err := repo.Create(ctx, &p); err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Publish it so it appears in search.
	p.Status = catalog.StatusActive
	if err := repo.Update(ctx, &p); err != nil {
		t.Fatalf("Update: %v", err)
	}

	sp := search.Product{
		ID:          p.ID,
		Name:        p.Name,
		Slug:        p.Slug,
		Description: "comfortable running shoe",
	}

	err := engine.IndexProduct(ctx, sp)
	if err != nil {
		t.Fatalf("IndexProduct: %v", err)
	}

	// The product should now be searchable.
	result, err := engine.Search(ctx, search.SearchQuery{Text: "running shoe"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if result.Total == 0 {
		t.Error("expected at least 1 result after IndexProduct")
	}
}

func TestSearchEngine_IndexProduct_NotFound(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	engine := postgres.NewSearchEngine(db)

	sp := search.Product{ID: id.New(), Name: "Ghost", Description: "none"}
	err := engine.IndexProduct(context.Background(), sp)
	if err == nil {
		t.Fatal("expected error for non-existent product")
	}
}

func TestSearchEngine_RemoveProduct(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	engine := postgres.NewSearchEngine(db)
	ctx := context.Background()

	// Create and publish a product.
	p := mustNewProduct(t, "Removable Widget", "removable-widget-"+id.New()[:8])
	repo := postgres.NewProductRepo(db)
	if err := repo.Create(ctx, &p); err != nil {
		t.Fatalf("Create: %v", err)
	}
	p.Status = catalog.StatusActive
	if err := repo.Update(ctx, &p); err != nil {
		t.Fatalf("Update: %v", err)
	}

	// Index it explicitly.
	sp := search.Product{ID: p.ID, Name: p.Name, Description: "unique removable item"}
	if err := engine.IndexProduct(ctx, sp); err != nil {
		t.Fatalf("IndexProduct: %v", err)
	}

	// Remove from search index.
	if err := engine.RemoveProduct(ctx, p.ID); err != nil {
		t.Fatalf("RemoveProduct: %v", err)
	}

	// Should no longer appear in search.
	result, err := engine.Search(ctx, search.SearchQuery{Text: "unique removable item"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if result.Total != 0 {
		t.Errorf("Total = %d, want 0 after RemoveProduct", result.Total)
	}
}

func TestSearchEngine_Search_TextMatch(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	engine := postgres.NewSearchEngine(db)
	ctx := context.Background()
	repo := postgres.NewProductRepo(db)

	// Create two published products.
	p1 := mustNewProduct(t, "Leather Boots", "leather-boots-"+id.New()[:8])
	p1.Description = "premium leather boots for hiking"
	p1.Status = catalog.StatusActive
	if err := repo.Create(ctx, &p1); err != nil {
		t.Fatalf("Create p1: %v", err)
	}
	if err := repo.Update(ctx, &p1); err != nil {
		t.Fatalf("Update p1: %v", err)
	}

	p2 := mustNewProduct(t, "Cotton T-Shirt", "cotton-tshirt-"+id.New()[:8])
	p2.Description = "comfortable cotton t-shirt"
	p2.Status = catalog.StatusActive
	if err := repo.Create(ctx, &p2); err != nil {
		t.Fatalf("Create p2: %v", err)
	}
	if err := repo.Update(ctx, &p2); err != nil {
		t.Fatalf("Update p2: %v", err)
	}

	// Search for leather.
	result, err := engine.Search(ctx, search.SearchQuery{Text: "leather"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if result.Total != 1 {
		t.Errorf("Total = %d, want 1", result.Total)
	}
	if len(result.Products) != 1 {
		t.Fatalf("len(Products) = %d, want 1", len(result.Products))
	}
	if result.Products[0].ID != p1.ID {
		t.Errorf("Product ID = %q, want %q", result.Products[0].ID, p1.ID)
	}
}

func TestSearchEngine_Search_EmptyText(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	engine := postgres.NewSearchEngine(db)
	ctx := context.Background()
	repo := postgres.NewProductRepo(db)

	p := mustNewProduct(t, "Visible Product", "visible-"+id.New()[:8])
	p.Status = catalog.StatusActive
	if err := repo.Create(ctx, &p); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.Update(ctx, &p); err != nil {
		t.Fatalf("Update: %v", err)
	}

	// Empty text returns all active products with search vectors.
	result, err := engine.Search(ctx, search.SearchQuery{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if result.Total < 1 {
		t.Error("expected at least 1 result for empty text query")
	}
}

func TestSearchEngine_Search_Pagination(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	engine := postgres.NewSearchEngine(db)
	ctx := context.Background()
	repo := postgres.NewProductRepo(db)

	suffix := id.New()[:6]
	for i := 0; i < 3; i++ {
		p := mustNewProduct(t, fmt.Sprintf("Paginated Item %d", i), fmt.Sprintf("paginated-%d-%s", i, suffix))
		p.Description = "paginated searchable item"
		p.Status = catalog.StatusActive
		if err := repo.Create(ctx, &p); err != nil {
			t.Fatalf("Create p%d: %v", i, err)
		}
		if err := repo.Update(ctx, &p); err != nil {
			t.Fatalf("Update p%d: %v", i, err)
		}
	}

	// Fetch first page (limit 2).
	r1, err := engine.Search(ctx, search.SearchQuery{Text: "paginated", Limit: 2})
	if err != nil {
		t.Fatalf("Search page 1: %v", err)
	}
	if r1.Total < 3 {
		t.Errorf("Total = %d, want >= 3", r1.Total)
	}
	if len(r1.Products) != 2 {
		t.Errorf("len(Products) = %d, want 2", len(r1.Products))
	}

	// Fetch second page.
	r2, err := engine.Search(ctx, search.SearchQuery{Text: "paginated", Limit: 2, Offset: 2})
	if err != nil {
		t.Fatalf("Search page 2: %v", err)
	}
	if len(r2.Products) < 1 {
		t.Errorf("len(Products) = %d, want >= 1", len(r2.Products))
	}
}

func TestSearchEngine_Search_SortByName(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	engine := postgres.NewSearchEngine(db)
	ctx := context.Background()
	repo := postgres.NewProductRepo(db)

	suffix := id.New()[:6]
	names := []string{"Alpha Sortable " + suffix, "Charlie Sortable " + suffix, "Bravo Sortable " + suffix}
	for _, name := range names {
		p := mustNewProduct(t, name, "sort-"+id.New()[:8])
		p.Description = "sortable product " + suffix
		p.Status = catalog.StatusActive
		if err := repo.Create(ctx, &p); err != nil {
			t.Fatalf("Create %s: %v", name, err)
		}
		if err := repo.Update(ctx, &p); err != nil {
			t.Fatalf("Update %s: %v", name, err)
		}
	}

	result, err := engine.Search(ctx, search.SearchQuery{Text: "sortable " + suffix, Sort: "name"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(result.Products) < 3 {
		t.Fatalf("len(Products) = %d, want >= 3", len(result.Products))
	}
	if result.Products[0].Name > result.Products[1].Name {
		t.Errorf("expected ascending order: %q > %q", result.Products[0].Name, result.Products[1].Name)
	}
}

func TestSearchEngine_Search_ValidationError(t *testing.T) {
	db := testDB(t)
	engine := postgres.NewSearchEngine(db)

	_, err := engine.Search(context.Background(), search.SearchQuery{Offset: -1})
	if err == nil {
		t.Fatal("expected validation error for negative offset")
	}
}
