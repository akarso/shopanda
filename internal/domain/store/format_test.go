package store_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/domain/store"
)

func TestPreferContactEmail(t *testing.T) {
	if got := store.PreferContactEmail("sales@x.com", "general@x.com"); got != "sales@x.com" {
		t.Fatalf("prefer sales: %q", got)
	}
	if got := store.PreferContactEmail("", "general@x.com"); got != "general@x.com" {
		t.Fatalf("fallback general: %q", got)
	}
	if got := store.PreferContactEmail("  ", "  "); got != "" {
		t.Fatalf("both blank: %q", got)
	}
}

func TestFormatAddressDisplay(t *testing.T) {
	got := store.FormatAddressDisplay("Ul. Krotka 1", "lok. 2", "Warszawa", "Mazowieckie", "00-001", "PL")
	want := "Ul. Krotka 1, lok. 2, Mazowieckie, 00-001 Warszawa, PL"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if got := store.FormatAddressDisplay("", "", "", "", "", ""); got != "" {
		t.Fatalf("empty: %q", got)
	}
}

func TestAddressFormatDisplayAndLines(t *testing.T) {
	a := store.NewAddressFromDB("s1", store.AddressRoleGeneral, "Main 1", "Suite B", "City", "Region", "12-345", "PL", "")
	if got := a.FormatDisplay(); got != "Main 1, Suite B, Region, 12-345 City, PL" {
		t.Fatalf("FormatDisplay: %q", got)
	}
	lines := a.FormatLines()
	if len(lines) != 5 || lines[0] != "Main 1" || lines[2] != "12-345 City" {
		t.Fatalf("FormatLines: %#v", lines)
	}
	empty := store.NewAddressFromDB("s1", store.AddressRoleInvoice, "", "", "", "", "", "PL", "555")
	if empty.FormatDisplay() != "" {
		t.Fatal("phone-only must not format")
	}
	if empty.FormatLines() != nil {
		t.Fatal("phone-only lines must be nil")
	}
	var nilAddr *store.Address
	if nilAddr.FormatDisplay() != "" || nilAddr.FormatLines() != nil {
		t.Fatal("nil address")
	}
}

func TestFormatTaxIDLine(t *testing.T) {
	if got := store.FormatTaxIDLine("NIP", "5252344022"); got != "NIP: 5252344022" {
		t.Fatalf("got %q", got)
	}
	if got := store.FormatTaxIDLine("", "5252344022"); got != "5252344022" {
		t.Fatalf("no label: %q", got)
	}
	if got := store.JoinTaxIDLines([]string{"NIP: 1", "", "VAT: 2"}); got != "NIP: 1, VAT: 2" {
		t.Fatalf("join: %q", got)
	}
}
