package pluginsdk_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/pluginsdk"
)

func TestHost_PortGettersAndRequire(t *testing.T) {
	app := testApp()
	app.MarkCapabilitySatisfied(pluginsdk.CapDB)
	if err := app.BindCatalogPort(hostFakeCatalog{}); err != nil {
		t.Fatalf("bind: %v", err)
	}
	h := pluginsdk.AsHost(app)
	if _, ok := h.Catalog(); !ok {
		t.Fatal("expected catalog port")
	}
	if _, ok := h.Cart(); ok {
		t.Fatal("cart not bound yet")
	}
	h.Require(pluginsdk.CapOrders)
	if err := app.AssertNeedsMet(); err == nil {
		t.Fatal("expected missing orders")
	}
}

type hostFakeCatalog struct{}

func (hostFakeCatalog) FindVariant(context.Context, string) (*extapi.CatalogVariant, error) {
	return nil, nil
}
func (hostFakeCatalog) FindProduct(context.Context, string) (*extapi.CatalogProduct, error) {
	return nil, nil
}
