package postgres_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/domain/store"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"
)

func mustNewStore(t *testing.T, code, name string) store.Store {
	t.Helper()
	s, err := store.NewStore(id.New(), code, name, "USD", "US", "en", code+".example.com")
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return s
}

func TestStoreRepo_NilDB(t *testing.T) {
	_, err := postgres.NewStoreRepo(nil)
	if err == nil {
		t.Fatal("expected error for nil db")
	}
}

func TestStoreRepo_CreateAndFindByID(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	db.Exec("DELETE FROM stores")
	t.Cleanup(func() { db.Exec("DELETE FROM stores") })

	repo, err := postgres.NewStoreRepo(db)
	if err != nil {
		t.Fatalf("NewStoreRepo: %v", err)
	}
	ctx := context.Background()

	s := mustNewStore(t, "us-main", "US Main")
	if err := repo.Create(ctx, &s); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.FindByID(ctx, s.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got == nil {
		t.Fatal("FindByID returned nil")
	}
	if got.Code != "us-main" {
		t.Errorf("Code: got %q, want %q", got.Code, "us-main")
	}
	if got.Name != "US Main" {
		t.Errorf("Name: got %q, want %q", got.Name, "US Main")
	}
	if got.Currency != "USD" {
		t.Errorf("Currency: got %q, want %q", got.Currency, "USD")
	}
}

func TestStoreRepo_FindByCode(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	db.Exec("DELETE FROM stores")
	t.Cleanup(func() { db.Exec("DELETE FROM stores") })

	repo, err := postgres.NewStoreRepo(db)
	if err != nil {
		t.Fatalf("NewStoreRepo: %v", err)
	}
	ctx := context.Background()

	s := mustNewStore(t, "eu-shop", "EU Shop")
	if err := repo.Create(ctx, &s); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.FindByCode(ctx, "eu-shop")
	if err != nil {
		t.Fatalf("FindByCode: %v", err)
	}
	if got == nil {
		t.Fatal("FindByCode returned nil")
	}
	if got.ID != s.ID {
		t.Errorf("ID: got %q, want %q", got.ID, s.ID)
	}
}

func TestStoreRepo_FindByDomain(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	db.Exec("DELETE FROM stores")
	t.Cleanup(func() { db.Exec("DELETE FROM stores") })

	repo, err := postgres.NewStoreRepo(db)
	if err != nil {
		t.Fatalf("NewStoreRepo: %v", err)
	}
	ctx := context.Background()

	s := mustNewStore(t, "dom-test", "Domain Test")
	if err := repo.Create(ctx, &s); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.FindByDomain(ctx, s.Domain)
	if err != nil {
		t.Fatalf("FindByDomain: %v", err)
	}
	if got == nil {
		t.Fatal("FindByDomain returned nil")
	}
	if got.ID != s.ID {
		t.Errorf("ID: got %q, want %q", got.ID, s.ID)
	}
}

func TestStoreRepo_FindDefault(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	db.Exec("DELETE FROM stores")
	t.Cleanup(func() { db.Exec("DELETE FROM stores") })

	repo, err := postgres.NewStoreRepo(db)
	if err != nil {
		t.Fatalf("NewStoreRepo: %v", err)
	}
	ctx := context.Background()

	s := mustNewStore(t, "default-store", "Default Store")
	s.IsDefault = true
	if err := repo.Create(ctx, &s); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.FindDefault(ctx)
	if err != nil {
		t.Fatalf("FindDefault: %v", err)
	}
	if got == nil {
		t.Fatal("FindDefault returned nil")
	}
	if got.ID != s.ID {
		t.Errorf("ID: got %q, want %q", got.ID, s.ID)
	}
	if !got.IsDefault {
		t.Error("expected IsDefault=true")
	}
}

func TestStoreRepo_FindAll(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	db.Exec("DELETE FROM stores")
	t.Cleanup(func() { db.Exec("DELETE FROM stores") })

	repo, err := postgres.NewStoreRepo(db)
	if err != nil {
		t.Fatalf("NewStoreRepo: %v", err)
	}
	ctx := context.Background()

	s1 := mustNewStore(t, "store-a", "Store A")
	s2 := mustNewStore(t, "store-b", "Store B")
	for _, s := range []*store.Store{&s1, &s2} {
		if err := repo.Create(ctx, s); err != nil {
			t.Fatalf("Create %q: %v", s.Code, err)
		}
	}

	all, err := repo.FindAll(ctx)
	if err != nil {
		t.Fatalf("FindAll: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("FindAll: got %d, want 2", len(all))
	}
}

func TestStoreRepo_Update(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	db.Exec("DELETE FROM stores")
	t.Cleanup(func() { db.Exec("DELETE FROM stores") })

	repo, err := postgres.NewStoreRepo(db)
	if err != nil {
		t.Fatalf("NewStoreRepo: %v", err)
	}
	ctx := context.Background()

	s := mustNewStore(t, "upd-store", "Before")
	if err := repo.Create(ctx, &s); err != nil {
		t.Fatalf("Create: %v", err)
	}

	s.Name = "After"
	s.Language = "de"
	if err := repo.Update(ctx, &s); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := repo.FindByID(ctx, s.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.Name != "After" {
		t.Errorf("Name: got %q, want %q", got.Name, "After")
	}
	if got.Language != "de" {
		t.Errorf("Language: got %q, want %q", got.Language, "de")
	}
}

func TestStoreRepo_Update_NotFound(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo, err := postgres.NewStoreRepo(db)
	if err != nil {
		t.Fatalf("NewStoreRepo: %v", err)
	}

	s := mustNewStore(t, "ghost-store", "Ghost")
	err = repo.Update(context.Background(), &s)
	if err == nil {
		t.Fatal("expected error updating non-existent store")
	}
	if !apperror.Is(err, apperror.CodeNotFound) {
		t.Errorf("expected NotFound, got: %v", err)
	}
}

func TestStoreRepo_Create_DuplicateCode(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	db.Exec("DELETE FROM stores")
	t.Cleanup(func() { db.Exec("DELETE FROM stores") })

	repo, err := postgres.NewStoreRepo(db)
	if err != nil {
		t.Fatalf("NewStoreRepo: %v", err)
	}
	ctx := context.Background()

	s1 := mustNewStore(t, "dup-code", "First")
	if err := repo.Create(ctx, &s1); err != nil {
		t.Fatalf("Create: %v", err)
	}

	s2 := mustNewStore(t, "dup-code", "Second")
	s2.Domain = "other.example.com" // different domain to isolate code conflict
	err = repo.Create(ctx, &s2)
	if err == nil {
		t.Fatal("expected conflict for duplicate code")
	}
	if !apperror.Is(err, apperror.CodeConflict) {
		t.Errorf("expected Conflict, got: %v", err)
	}
}

func TestStoreRepo_FindByID_EmptyID(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo, err := postgres.NewStoreRepo(db)
	if err != nil {
		t.Fatalf("NewStoreRepo: %v", err)
	}

	_, err = repo.FindByID(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty id")
	}
}

func TestStoreRepo_FindByID_NotFound(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo, err := postgres.NewStoreRepo(db)
	if err != nil {
		t.Fatalf("NewStoreRepo: %v", err)
	}

	got, err := repo.FindByID(context.Background(), id.New())
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil for non-existent store")
	}
}

func TestStoreRepo_Create_Nil(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo, err := postgres.NewStoreRepo(db)
	if err != nil {
		t.Fatalf("NewStoreRepo: %v", err)
	}

	if err := repo.Create(context.Background(), nil); err == nil {
		t.Fatal("expected error for nil store")
	}
}
