package postgres_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/domain/customer"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"
)

func seedAddressCustomer(t *testing.T, repo *postgres.CustomerRepo, email string) string {
	t.Helper()
	c := mustNewCustomer(t, email)
	if err := repo.Create(context.Background(), &c); err != nil {
		t.Fatalf("seed customer: %v", err)
	}
	return c.ID
}

func mustNewSavedAddress(t *testing.T, customerID, recipient string, isDefault bool) customer.Address {
	t.Helper()
	a, err := customer.NewAddress(id.New(), customerID, "Home", recipient, "1 Logic Lane", "Berlin", "10115", "DE")
	if err != nil {
		t.Fatalf("NewAddress: %v", err)
	}
	a.IsDefault = isDefault
	return a
}

func TestCustomerAddressRepo_CreateFirstBecomesDefault(t *testing.T) {
	db := testDB(t)
	ensureMigrations(t, db)
	t.Cleanup(func() {
		db.Exec("DELETE FROM customer_addresses")
		db.Exec("DELETE FROM customers")
	})

	customerRepo, err := postgres.NewCustomerRepo(db)
	if err != nil {
		t.Fatalf("NewCustomerRepo: %v", err)
	}
	repo, err := postgres.NewCustomerAddressRepo(db)
	if err != nil {
		t.Fatalf("NewCustomerAddressRepo: %v", err)
	}
	ctx := context.Background()
	customerID := seedAddressCustomer(t, customerRepo, "addr-default@example.com")

	a := mustNewSavedAddress(t, customerID, "Ada Lovelace", false)
	if err := repo.Create(ctx, &a); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !a.IsDefault {
		t.Fatal("first address should be marked default")
	}

	def, err := repo.FindDefault(ctx, customerID)
	if err != nil {
		t.Fatalf("FindDefault: %v", err)
	}
	if def == nil || def.ID != a.ID {
		t.Fatalf("default = %+v, want id %s", def, a.ID)
	}
}

func TestCustomerAddressRepo_SetDefaultIsExclusive(t *testing.T) {
	db := testDB(t)
	ensureMigrations(t, db)
	t.Cleanup(func() {
		db.Exec("DELETE FROM customer_addresses")
		db.Exec("DELETE FROM customers")
	})

	customerRepo, _ := postgres.NewCustomerRepo(db)
	repo, _ := postgres.NewCustomerAddressRepo(db)
	ctx := context.Background()
	customerID := seedAddressCustomer(t, customerRepo, "addr-exclusive@example.com")

	first := mustNewSavedAddress(t, customerID, "Ada Home", false)
	if err := repo.Create(ctx, &first); err != nil {
		t.Fatalf("Create first: %v", err)
	}
	second := mustNewSavedAddress(t, customerID, "Ada Office", false)
	if err := repo.Create(ctx, &second); err != nil {
		t.Fatalf("Create second: %v", err)
	}

	if err := repo.SetDefault(ctx, customerID, second.ID); err != nil {
		t.Fatalf("SetDefault: %v", err)
	}

	list, err := repo.ListByCustomer(ctx, customerID)
	if err != nil {
		t.Fatalf("ListByCustomer: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("addresses = %d, want 2", len(list))
	}
	defaults := 0
	for _, a := range list {
		if a.IsDefault {
			defaults++
			if a.ID != second.ID {
				t.Fatalf("default id = %s, want %s", a.ID, second.ID)
			}
		}
	}
	if defaults != 1 {
		t.Fatalf("default count = %d, want exactly 1", defaults)
	}
	// Default first ordering.
	if !list[0].IsDefault {
		t.Fatal("ListByCustomer should return default first")
	}
}

func TestCustomerAddressRepo_UpdateAndDelete(t *testing.T) {
	db := testDB(t)
	ensureMigrations(t, db)
	t.Cleanup(func() {
		db.Exec("DELETE FROM customer_addresses")
		db.Exec("DELETE FROM customers")
	})

	customerRepo, _ := postgres.NewCustomerRepo(db)
	repo, _ := postgres.NewCustomerAddressRepo(db)
	ctx := context.Background()
	customerID := seedAddressCustomer(t, customerRepo, "addr-update@example.com")

	a := mustNewSavedAddress(t, customerID, "Ada Lovelace", true)
	if err := repo.Create(ctx, &a); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := a.Apply("Office", "Ada L.", "2 Babbage Blvd", "Munich", "80331", "DE"); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if err := repo.Update(ctx, &a); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, err := repo.FindByID(ctx, a.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got == nil || got.City != "Munich" || got.Label != "Office" {
		t.Fatalf("address not updated: %+v", got)
	}

	if err := repo.Delete(ctx, customerID, a.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if got, _ := repo.FindByID(ctx, a.ID); got != nil {
		t.Fatalf("address should be deleted, got %+v", got)
	}
}

func TestCustomerAddressRepo_DeleteForeignAddressNotFound(t *testing.T) {
	db := testDB(t)
	ensureMigrations(t, db)
	t.Cleanup(func() {
		db.Exec("DELETE FROM customer_addresses")
		db.Exec("DELETE FROM customers")
	})

	customerRepo, _ := postgres.NewCustomerRepo(db)
	repo, _ := postgres.NewCustomerAddressRepo(db)
	ctx := context.Background()
	ownerID := seedAddressCustomer(t, customerRepo, "addr-owner@example.com")
	otherID := seedAddressCustomer(t, customerRepo, "addr-other@example.com")

	a := mustNewSavedAddress(t, ownerID, "Ada Lovelace", true)
	if err := repo.Create(ctx, &a); err != nil {
		t.Fatalf("Create: %v", err)
	}

	err := repo.Delete(ctx, otherID, a.ID)
	if !apperror.Is(err, apperror.CodeNotFound) {
		t.Fatalf("Delete foreign = %v, want NotFound", err)
	}
}
