package storeidentity_test

import (
	"context"
	"strings"
	"testing"

	storeidentity "github.com/akarso/shopanda/internal/application/storeidentity"
	"github.com/akarso/shopanda/internal/domain/store"
)

type memConfig map[string]interface{}

func (m memConfig) Get(_ context.Context, key string) (interface{}, error) {
	return m[key], nil
}

func TestEmailBrandSource_FillsGeneralAddress(t *testing.T) {
	st := testStore()
	st.Name = "DBA Shop"
	mem := newMemIdentity()
	general, _ := store.NewAddress(st.ID, store.AddressRoleGeneral, "Ul. Test 1", "", "Warszawa", "", "00-001", "PL", "")
	_ = mem.SaveAddress(context.Background(), general)

	svc := storeidentity.NewService(
		mem,
		&memStoreRepo{byID: map[string]*store.Store{st.ID: st}},
		store.DefaultTaxIdentifierTypeRegistry(),
	)
	// FindDefault for brand source
	stores := &memStoreRepoDefault{memStoreRepo: memStoreRepo{byID: map[string]*store.Store{st.ID: st}}, def: st}

	brand := storeidentity.EmailBrandSource{
		Identity: svc,
		Stores:   stores,
		Config:   memConfig{"store::" + st.ID + "::store.logo": "https://cdn.example/logo.png"},
	}
	ed := brand.BrandEmailData(context.Background(), "https://shop.example")
	if ed.StoreName != "DBA Shop" {
		t.Fatalf("StoreName=%q", ed.StoreName)
	}
	if ed.StoreURL != "https://shop.example" {
		t.Fatalf("StoreURL=%q", ed.StoreURL)
	}
	if !strings.Contains(ed.StoreAddress, "Ul. Test 1") || !strings.Contains(ed.StoreAddress, "00-001 Warszawa") {
		t.Fatalf("StoreAddress=%q", ed.StoreAddress)
	}
	if ed.LogoURL != "https://cdn.example/logo.png" {
		t.Fatalf("LogoURL=%q", ed.LogoURL)
	}
}

type memStoreRepoDefault struct {
	memStoreRepo
	def *store.Store
}

func (m *memStoreRepoDefault) FindDefault(context.Context) (*store.Store, error) {
	return m.def, nil
}
