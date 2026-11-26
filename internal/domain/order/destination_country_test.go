package order_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/domain/order"
)

func TestValidateDestinationCountry_UKCanonicalizesToGB(t *testing.T) {
	got, err := order.ValidateDestinationCountry("uk")
	if err != nil {
		t.Fatalf("ValidateDestinationCountry: %v", err)
	}
	if got != "GB" {
		t.Fatalf("got %q, want GB", got)
	}
}
