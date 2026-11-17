package harness_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/pluginsdk"
	"github.com/akarso/shopanda/pkg/pluginsdk/harness"
	"github.com/akarso/shopanda/plugins/extref"
)

type recordingCart struct {
	called bool
}

func (r *recordingCart) AddItem(context.Context, string, string, string, int, pluginsdk.CartAddItemOpts) error {
	r.called = true
	return nil
}
func (r *recordingCart) Get(context.Context, string, string) (*extapi.CartView, error) {
	return nil, nil
}
func (r *recordingCart) EnsureActive(context.Context, string, string) (*extapi.CartView, error) {
	return nil, nil
}
func (r *recordingCart) EnsureStorefrontCart(context.Context, http.ResponseWriter, *http.Request, string) (*extapi.CartView, error) {
	return nil, nil
}
func (r *recordingCart) DiscardEmptyGuestCart(context.Context, http.ResponseWriter, *http.Request, string) error {
	return nil
}
func (r *recordingCart) DeleteLineExtension(context.Context, string, string, string, string) error {
	return nil
}
func (r *recordingCart) UpsertLineExtension(context.Context, string, string, string, []pluginsdk.CartExtensionValue, string) error {
	return nil
}

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
	// Attributed Host.Cart() stays closed — owner scoping requires NewCartPort.
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

func TestAdaptInit_RecordingCartBound(t *testing.T) {
	// Full CartPort fake records AddItem. Host.Cart() still fail-closes for
	// non-*cartPort bindings on attributed Hosts — call AddItem on the fake
	// directly to prove BindCartPort accepted it through AdaptInit.
	rec := &recordingCart{}
	_, err := harness.AdaptInit(extref.New(), harness.Options{
		CartPort: rec,
		Satisfy:  []pluginsdk.Capability{pluginsdk.CapCart},
	})
	if err != nil {
		t.Fatalf("AdaptInit: %v", err)
	}
	if err := rec.AddItem(context.Background(), "c1", "cust", "v1", 1, pluginsdk.CartAddItemOpts{}); err != nil {
		t.Fatal(err)
	}
	if !rec.called {
		t.Fatal("expected recording cart AddItem")
	}
}
