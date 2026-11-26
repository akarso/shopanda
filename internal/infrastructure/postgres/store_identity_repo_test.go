package postgres_test

import (
	"context"
	"strings"
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
	err = repo.DeleteTaxIdentifier(ctx, s.ID, "pl_nip")
	if err == nil || !apperror.Is(err, apperror.CodeNotFound) {
		t.Fatalf("second delete: %v", err)
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
	mustExec(t, db, "DELETE FROM config WHERE key = 'store.address' OR key LIKE 'store::%::store.address'")
	t.Cleanup(func() {
		mustExec(t, db, "DELETE FROM store_addresses")
		mustExec(t, db, "DELETE FROM stores")
		mustExec(t, db, "DELETE FROM config WHERE key = 'store.address' OR key LIKE 'store::%::store.address'")
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

	// Bare key only (admin wrote without store scope).
	bare, err := store.NewStore(id.New(), "bare-shop", "Bare Shop", "PLN", "PL", "pl", "bare.example.com")
	if err != nil {
		t.Fatalf("NewStore bare: %v", err)
	}
	if err := storeRepo.Create(ctx, &bare); err != nil {
		t.Fatalf("Create bare: %v", err)
	}
	mustExec(t, db, `INSERT INTO config (key, value) VALUES ('store.address', to_jsonb($1::text))
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, "Bare free-text address")

	// Scoped key wins over bare for this store.
	scoped, err := store.NewStore(id.New(), "scoped-shop", "Scoped Shop", "PLN", "uk", "en", "scoped.example.com")
	if err != nil {
		// NewStore may already map UK→GB via NormalizeCountry — use GB then force UK in DB.
		scoped, err = store.NewStore(id.New(), "scoped-shop", "Scoped Shop", "PLN", "GB", "en", "scoped.example.com")
		if err != nil {
			t.Fatalf("NewStore scoped: %v", err)
		}
	}
	if err := storeRepo.Create(ctx, &scoped); err != nil {
		t.Fatalf("Create scoped: %v", err)
	}
	mustExec(t, db, `UPDATE stores SET country = 'UK' WHERE id = $1`, scoped.ID)
	mustExec(t, db, `INSERT INTO config (key, value) VALUES ($1, to_jsonb($2::text))
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`,
		"store::"+scoped.ID+"::store.address", "Scoped free-text address")

	// Bad rows: newline, over-long, invalid country — must be skipped without failing the statement.
	badNL, err := store.NewStore(id.New(), "bad-nl", "Bad NL", "PLN", "PL", "pl", "badnl.example.com")
	if err != nil {
		t.Fatalf("NewStore badNL: %v", err)
	}
	if err := storeRepo.Create(ctx, &badNL); err != nil {
		t.Fatalf("Create badNL: %v", err)
	}
	mustExec(t, db, `INSERT INTO config (key, value) VALUES ($1, to_jsonb($2::text))
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`,
		"store::"+badNL.ID+"::store.address", "line1\nline2")

	badLen, err := store.NewStore(id.New(), "bad-len", "Bad Len", "PLN", "PL", "pl", "badlen.example.com")
	if err != nil {
		t.Fatalf("NewStore badLen: %v", err)
	}
	if err := storeRepo.Create(ctx, &badLen); err != nil {
		t.Fatalf("Create badLen: %v", err)
	}
	mustExec(t, db, `INSERT INTO config (key, value) VALUES ($1, to_jsonb($2::text))
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`,
		"store::"+badLen.ID+"::store.address", strings.Repeat("x", 256))

	badCty, err := store.NewStore(id.New(), "bad-cty", "Bad Cty", "PLN", "PL", "pl", "badcty.example.com")
	if err != nil {
		t.Fatalf("NewStore badCty: %v", err)
	}
	if err := storeRepo.Create(ctx, &badCty); err != nil {
		t.Fatalf("Create badCty: %v", err)
	}
	mustExec(t, db, `UPDATE stores SET country = 'Poland' WHERE id = $1`, badCty.ID)
	mustExec(t, db, `INSERT INTO config (key, value) VALUES ($1, to_jsonb($2::text))
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`,
		"store::"+badCty.ID+"::store.address", "Some street")

	mustExec(t, db, postgres.StoreAddressBackfillSQL)

	addr, err := repo.GetAddress(ctx, bare.ID, store.AddressRoleGeneral)
	if err != nil || addr == nil || addr.Line1 != "Bare free-text address" || addr.Country != "PL" {
		t.Fatalf("bare backfill: %#v err=%v", addr, err)
	}

	addr, err = repo.GetAddress(ctx, scoped.ID, store.AddressRoleGeneral)
	if err != nil || addr == nil || addr.Line1 != "Scoped free-text address" || addr.Country != "GB" {
		t.Fatalf("scoped+UK backfill: %#v err=%v", addr, err)
	}

	for _, sid := range []string{badNL.ID, badLen.ID, badCty.ID} {
		addr, err = repo.GetAddress(ctx, sid, store.AddressRoleGeneral)
		if err != nil {
			t.Fatalf("skip lookup %s: %v", sid, err)
		}
		if addr != nil {
			t.Fatalf("expected skip for %s, got %#v", sid, addr)
		}
	}
}
