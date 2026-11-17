package harness_test

import (
	"testing"

	"github.com/akarso/shopanda/pkg/pluginsdk"
	"github.com/akarso/shopanda/pkg/pluginsdk/harness"
	"github.com/akarso/shopanda/plugins/extref"
)

func TestAdaptInit_ExtRef(t *testing.T) {
	res, err := harness.AdaptInit(extref.New(), harness.Options{
		Satisfy: []pluginsdk.Capability{pluginsdk.CapCart},
	})
	if err != nil {
		t.Fatalf("AdaptInit: %v", err)
	}
	if res.Host == nil {
		t.Fatal("expected Host")
	}
}

func TestAdaptInit_CartPortMarkSatisfied(t *testing.T) {
	// MarkCapabilitySatisfied (no *cartPort) lets Needs(CapCart) pass Init.
	// Attributed Host.Cart() stays closed — owner scoping requires NewCartPort
	// (covered in pkg/pluginsdk cart_port_test).
	res, err := harness.AdaptInit(extref.New(), harness.Options{
		Satisfy: []pluginsdk.Capability{pluginsdk.CapCart},
	})
	if err != nil {
		t.Fatalf("AdaptInit: %v", err)
	}
	if _, ok := res.Host.Cart(); ok {
		t.Fatal("MarkCapabilitySatisfied alone must not expose Cart()")
	}
}
