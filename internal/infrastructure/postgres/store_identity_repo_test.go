package postgres_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/domain/store"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"
)

func TestStoreIdentityRepo_CRUD(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	mustExec(t, db, "DELETE FROM store_tax_identifiers")
	mustExec(t, db, "DELETE FROM store_addresses")
	mustExec(t, db, "DELETE FROM store_identities")
	mustExec(t, db, "DELETE FROM stores")
	t.Cleanup(func() {
		mustExec(t, db, "DELETE FROM store_tax_identifiers")
		mustExec(t, db, "DELETE FROM store_addresses")
		mustExec(t, db, "DELETE FROM store_identities")
		mustExec(t, db, "DELETE FROM stores")
	})

	storeRepo, err := postgres.NewStoreRepo(db)
	if err != nil {
		t.Fatalf("NewStoreRepo: %v", err)
	}
	repo, err := postgres.NewStoreIdentityRepo(db)
	if err != nil {
		t.Fatalf("NewStoreIdentityRepo: %v", err)
	}
	ctx := context.Background()

	s := mustNewStore(t, "id-main", "Identity Main")
	if err := storeRepo.Create(ctx, &s); err != nil {
		t.Fatalf("Create store: %v", err)
	}

	got, err := repo.GetIdentity(ctx, s.ID)
	if err != nil || got != nil {
		t.Fatalf("GetIdentity empty: got=%v err=%v", got, err)
	}

	idty, err := store.NewIdentity(s.ID, "Acme Sp. z o.o.", "+48123", "Mon-Fri", "info@acme.test", "sales@acme.test", "help@acme.test")
	if err != nil {
		t.Fatalf("NewIdentity: %v", err)
	}
	if err := repo.SaveIdentity(ctx, idty); err != nil {
		t.Fatalf("SaveIdentity: %v", err)
	}
	got, err = repo.GetIdentity(ctx, s.ID)
	if err != nil || got == nil {
		t.Fatalf("GetIdentity: got=%v err=%v", got, err)
	}
	if got.LegalName != "Acme Sp. z o.o." || got.EmailGeneral != "info@acme.test" {
		t.Fatalf("identity mismatch: %#v", got)
	}

	addr, err := store.NewAddress(s.ID, store.AddressRoleGeneral, "ul. Test 1", "", "Warsaw", "", "00-001", "PL", "")
	if err != nil {
		t.Fatalf("NewAddress: %v", err)
	}
	if err := repo.SaveAddress(ctx, addr); err != nil {
		t.Fatalf("SaveAddress: %v", err)
	}
	list, err := repo.ListAddresses(ctx, s.ID)
	if err != nil || len(list) != 1 {
		t.Fatalf("ListAddresses: %v len=%d", err, len(list))
	}

	ti, err := store.NewTaxIdentifier(s.ID, "pl_nip", "1234567890")
	if err != nil {
		t.Fatalf("NewTaxIdentifier: %v", err)
	}
	if err := repo.UpsertTaxIdentifier(ctx, ti); err != nil {
		t.Fatalf("UpsertTaxIdentifier: %v", err)
	}
	tis, err := repo.ListTaxIdentifiers(ctx, s.ID)
	if err != nil || len(tis) != 1 || tis[0].Value != "1234567890" {
		t.Fatalf("ListTaxIdentifiers: %#v err=%v", tis, err)
	}
	if err := repo.DeleteTaxIdentifier(ctx, s.ID, "pl_nip"); err != nil {
		t.Fatalf("DeleteTaxIdentifier: %v", err)
	}
	tis, err = repo.ListTaxIdentifiers(ctx, s.ID)
	if err != nil || len(tis) != 0 {
		t.Fatalf("after delete: %#v err=%v", tis, err)
	}
}

func TestStoreIdentityRepo_UnknownStore(t *testing.T) {
	db := testDB(t)
	repo, err := postgres.NewStoreIdentityRepo(db)
	if err != nil {
		t.Fatalf("NewStoreIdentityRepo: %v", err)
	}
	ctx := context.Background()
	idty, err := store.NewIdentity(id.New(), "X", "", "", "", "", "")
	if err != nil {
		t.Fatalf("NewIdentity: %v", err)
	}
	err = repo.SaveIdentity(ctx, idty)
	if err == nil || !apperror.Is(err, apperror.CodeNotFound) {
		t.Fatalf("SaveIdentity unknown store: err=%v", err)
	}
}

func TestStoreIdentityRepo_BackfillFromConfig(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	mustExec(t, db, "DELETE FROM store_addresses")
	mustExec(t, db, "DELETE FROM stores")
	mustExec(t, db, "DELETE FROM config WHERE key LIKE 'store::%::store.address'")
	t.Cleanup(func() {
		mustExec(t, db, "DELETE FROM store_addresses")
		mustExec(t, db, "DELETE FROM stores")
		mustExec(t, db, "DELETE FROM config WHERE key LIKE 'store::%::store.address'")
	})

	storeRepo, err := postgres.NewStoreRepo(db)
	if err != nil {
		t.Fatalf("NewStoreRepo: %v", err)
	}
	ctx := context.Background()
	s, err := store.NewStore(id.New(), "bf-shop", "Backfill Shop", "PLN", "PL", "pl", "bf.example.com")
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := storeRepo.Create(ctx, &s); err != nil {
		t.Fatalf("Create: %v", err)
	}

	key := "store::" + s.ID + "::store.address"
	mustExec(t, db, `INSERT INTO config (key, value) VALUES ($1, to_jsonb($2::text))
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, key, "Legacy free-text address")

	// Re-run the backfill statement from migration 117 (tables already exist).
	mustExec(t, db, `
INSERT INTO store_addresses (store_id, role, line1, country)
SELECT
    s.id,
    'general',
    left(btrim(c.value #>> '{}'), 255),
    s.country
FROM config c
JOIN stores s
  ON c.key = 'store::' || s.id || '::store.address'
WHERE jsonb_typeof(c.value) = 'string'
  AND btrim(c.value #>> '{}') <> ''
ON CONFLICT (store_id, role) DO NOTHING`)

	repo, err := postgres.NewStoreIdentityRepo(db)
	if err != nil {
		t.Fatalf("NewStoreIdentityRepo: %v", err)
	}
	addr, err := repo.GetAddress(ctx, s.ID, store.AddressRoleGeneral)
	if err != nil || addr == nil {
		t.Fatalf("GetAddress: %v %#v", err, addr)
	}
	if addr.Line1 != "Legacy free-text address" {
		t.Fatalf("line1 = %q", addr.Line1)
	}
	if addr.Country != "PL" {
		t.Fatalf("country = %q", addr.Country)
	}
}
