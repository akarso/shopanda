package store_test

import (
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/domain/store"
)

func TestParseAddressRole(t *testing.T) {
	for _, role := range store.AllAddressRoles() {
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

func TestParseAddressRole_ErrorSanitized(t *testing.T) {
	_, err := store.ParseAddressRole("general\nBcc: x")
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "\n") {
		t.Fatalf("error must not contain raw newline: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "invalid role") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestAllAddressRoles_IsCopy(t *testing.T) {
	a := store.AllAddressRoles()
	b := store.AllAddressRoles()
	a[0] = store.AddressRole("mutated")
	if b[0] != store.AddressRoleGeneral {
		t.Fatal("AllAddressRoles must return an independent copy")
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
	if a.Role != store.AddressRoleInvoice || a.Country != "PL" {
		t.Fatalf("unexpected %+v", a)
	}
	if a.Line1 != "ul. Test 1" || a.Line2 != "lok. 2" || a.City != "Warszawa" || a.Postcode != "00-001" {
		t.Fatalf("fields not trimmed: %+v", a)
	}
	if a.IsEmpty() {
		t.Fatal("filled address must not be empty")
	}
}

func TestAddress_IsEmpty(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		var a *store.Address
		if !a.IsEmpty() {
			t.Fatal("nil must be empty")
		}
	})
	t.Run("country only", func(t *testing.T) {
		a, err := store.NewAddress("s-1", store.AddressRoleGeneral, "", "", "", "", "", "DE", "")
		if err != nil {
			t.Fatalf("NewAddress: %v", err)
		}
		if !a.IsEmpty() {
			t.Fatal("country-only must be empty")
		}
	})
	t.Run("phone only", func(t *testing.T) {
		a, err := store.NewAddress("s-1", store.AddressRoleInvoice, "", "", "", "", "", "PL", "+48 22")
		if err != nil {
			t.Fatalf("NewAddress: %v", err)
		}
		if !a.IsEmpty() {
			t.Fatal("phone-only invoice row must fall back to general")
		}
		if a.Phone != "+48 22" {
			t.Fatalf("phone = %q", a.Phone)
		}
	})
	t.Run("region only", func(t *testing.T) {
		a, err := store.NewAddress("s-1", store.AddressRoleGeneral, "", "", "", "MZ", "", "PL", "")
		if err != nil {
			t.Fatalf("NewAddress: %v", err)
		}
		if a.IsEmpty() {
			t.Fatal("region alone is postal content")
		}
	})
	t.Run("whitespace postal", func(t *testing.T) {
		a := store.Address{Line1: "  ", City: "\t", Country: "PL"}
		// tabs in constructed value — IsEmpty trims; tab-only city is empty after trim?
		// "\t" TrimSpace is "" — empty. Good.
		if !a.IsEmpty() {
			t.Fatal("whitespace-only postal fields must be empty")
		}
	})
}

func TestNewAddress_Validation(t *testing.T) {
	tests := []struct {
		name    string
		storeID string
		role    store.AddressRole
		line1   string
		country string
		want    string
	}{
		{name: "empty store", storeID: " ", role: store.AddressRoleGeneral, country: "PL", want: "store id"},
		{name: "newline store", storeID: "s\n1", role: store.AddressRoleGeneral, country: "PL", want: "store id"},
		{name: "bad role", storeID: "s-1", role: store.AddressRole("nope"), country: "PL", want: "invalid role"},
		{name: "empty country", storeID: "s-1", role: store.AddressRoleGeneral, country: "", want: "country"},
		{name: "digits", storeID: "s-1", role: store.AddressRoleGeneral, country: "12", want: "country"},
		{name: "alnum", storeID: "s-1", role: store.AddressRoleGeneral, country: "A1", want: "country"},
		{name: "two-byte rune", storeID: "s-1", role: store.AddressRoleGeneral, country: "Å", want: "country"},
		{name: "newline line1", storeID: "s-1", role: store.AddressRoleGeneral, line1: "a\nb", country: "PL", want: "line breaks or control"},
		{name: "ls line1", storeID: "s-1", role: store.AddressRoleGeneral, line1: "a\u2028b", country: "PL", want: "line breaks or control"},
		{name: "nul line1", storeID: "s-1", role: store.AddressRoleGeneral, line1: "a\x00b", country: "PL", want: "line breaks or control"},
		{name: "long line1", storeID: "s-1", role: store.AddressRoleGeneral, line1: strings.Repeat("x", store.MaxAddressLineLen+1), country: "PL", want: "max length"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := store.NewAddress(tt.storeID, tt.role, tt.line1, "", "", "", "", tt.country, "")
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error %q, want substring %q", err.Error(), tt.want)
			}
		})
	}
}

func TestAllAddressRoles_Order(t *testing.T) {
	want := []store.AddressRole{
		store.AddressRoleGeneral,
		store.AddressRoleInvoice,
		store.AddressRoleShippingOrigin,
		store.AddressRoleReturns,
	}
	got := store.AllAddressRoles()
	if len(got) != len(want) {
		t.Fatalf("len = %d", len(got))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("AllAddressRoles[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestNewAddressFromDB_UnvalidatedRoundTrip(t *testing.T) {
	a := store.NewAddressFromDB("s-1", store.AddressRoleGeneral, "l1\nx", "", "", "", "", "12", "")
	if a == nil {
		t.Fatal("nil Address")
	}
	if a.Country != "12" || a.Line1 != "l1\nx" {
		t.Fatalf("FromDB must keep raw bytes, got %+v", a)
	}
}
