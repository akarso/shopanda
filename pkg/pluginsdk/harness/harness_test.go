package harness_test

import (
	"context"
	"testing"

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

func TestAdaptInit_CartPortBound(t *testing.T) {
	rec := &recordingCart{}
	res, err := harness.AdaptInit(extref.New(), harness.Options{
		CartPort: rec,
		Satisfy:  []pluginsdk.Capability{pluginsdk.CapCart},
	})
	if err != nil {
		t.Fatalf("AdaptInit: %v", err)
	}
	if err := extref.AddItemWithGiftNote(context.Background(), res.Host, "c1", "cust", "v1", 1, "hi"); err != nil {
		t.Fatalf("AddItemWithGiftNote: %v", err)
	}
	if !rec.called {
		t.Fatal("expected cart port AddItem")
	}
}
