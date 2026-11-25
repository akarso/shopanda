package store_test

import (
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/domain/store"
)

func TestParseAddressRole(t *testing.T) {
	for _, role := range store.AddressRoles {
		got, err := store.ParseAddressRole("  " + string(role) + " ")
		if err != nil {
			t.Fatalf("ParseAddressRole(%q): %v", role, err)
		}
		if got != role {
			t.Fatalf("got %q want %q", got, role)
		}
	}
	if _, err := store.ParseAddressRole("warehouse"); err == nil {
		t.Fatal("expected error for unknown role")
	}
}

func TestNewAddress(t *testing.T) {
	a, err := store.NewAddress(
		"s-1",
		store.AddressRoleInvoice,
		" ul. Test 1 ",
		" lok. 2 ",
		" Warszawa ",
		" MZ ",
		" 00-001 ",
		" pl ",
		" +48 22 ",
	)
	if err != nil {
		t.Fatalf("NewAddress: %v", err)
	}
	if a.Role != store.AddressRoleInvoice {
		t.Errorf("Role = %q", a.Role)
	}
	if a.Country != "PL" {
		t.Errorf("Country = %q, want PL", a.Country)
	}
	if a.Line1 != "ul. Test 1" || a.City != "Warszawa" || a.Postcode != "00-001" {
		t.Errorf("fields not trimmed: %+v", a)
	}
}

func TestNewAddress_Validation(t *testing.T) {
	tests := []struct {
		name    string
		storeID string
		role    store.AddressRole
		country string
		want    string
	}{
		{name: "empty store", storeID: " ", role: store.AddressRoleGeneral, country: "PL", want: "store id"},
		{name: "bad role", storeID: "s-1", role: store.AddressRole("nope"), country: "PL", want: "invalid role"},
		{name: "empty country", storeID: "s-1", role: store.AddressRoleGeneral, country: "", want: "country"},
		{name: "bad country", storeID: "s-1", role: store.AddressRoleGeneral, country: "POL", want: "country"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := store.NewAddress(tt.storeID, tt.role, "", "", "", "", "", tt.country, "")
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error %q, want substring %q", err.Error(), tt.want)
			}
		})
	}
}

func TestAddressRoles_Order(t *testing.T) {
	want := []store.AddressRole{
		store.AddressRoleGeneral,
		store.AddressRoleInvoice,
		store.AddressRoleShippingOrigin,
		store.AddressRoleReturns,
	}
	if len(store.AddressRoles) != len(want) {
		t.Fatalf("len = %d", len(store.AddressRoles))
	}
	for i := range want {
		if store.AddressRoles[i] != want[i] {
			t.Fatalf("AddressRoles[%d] = %q, want %q", i, store.AddressRoles[i], want[i])
		}
	}
}
