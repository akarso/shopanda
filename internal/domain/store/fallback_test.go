package store_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/domain/store"
)

func addr(role store.AddressRole, line1, country string) *store.Address {
	return store.NewAddressFromDB("s1", role, line1, "", "", "", "", country, "")
}

func TestResolveInvoiceAddress(t *testing.T) {
	general := addr(store.AddressRoleGeneral, "Main St", "PL")
	invoice := addr(store.AddressRoleInvoice, "Invoice Rd", "PL")
	emptyInvoice := addr(store.AddressRoleInvoice, "", "PL")

	if got := store.ResolveInvoiceAddress(invoice, general); got != invoice {
		t.Fatalf("prefer invoice: got %#v", got)
	}
	if got := store.ResolveInvoiceAddress(emptyInvoice, general); got != general {
		t.Fatalf("empty invoice → general: got %#v", got)
	}
	if got := store.ResolveInvoiceAddress(nil, general); got != general {
		t.Fatalf("nil invoice → general: got %#v", got)
	}
	if got := store.ResolveInvoiceAddress(nil, nil); got != nil {
		t.Fatalf("both nil: got %#v", got)
	}
}

func TestResolveReturnsAddress(t *testing.T) {
	general := addr(store.AddressRoleGeneral, "Main St", "PL")
	origin := addr(store.AddressRoleShippingOrigin, "Warehouse", "PL")
	returns := addr(store.AddressRoleReturns, "Returns Desk", "PL")
	emptyReturns := addr(store.AddressRoleReturns, "", "PL")

	if got := store.ResolveReturnsAddress(returns, origin, general); got != returns {
		t.Fatalf("prefer returns: got %#v", got)
	}
	if got := store.ResolveReturnsAddress(emptyReturns, origin, general); got != origin {
		t.Fatalf("empty returns → origin: got %#v", got)
	}
	if got := store.ResolveReturnsAddress(nil, nil, general); got != general {
		t.Fatalf("nil → general: got %#v", got)
	}
	if got := store.ResolveReturnsAddress(nil, nil, nil); got != nil {
		t.Fatalf("all nil: got %#v", got)
	}
}
