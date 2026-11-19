package harness_test

import (
	"context"
	"fmt"
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

type needsJobsPlugin struct{}

func (needsJobsPlugin) Name() string { return "harness/needsjobs" }
func (needsJobsPlugin) Needs() []pluginsdk.Capability {
	return []pluginsdk.Capability{pluginsdk.CapJobs}
}
func (p *needsJobsPlugin) Init(h pluginsdk.Host) error {
	h.Require(pluginsdk.CapJobs)
	if _, ok := h.Jobs(); !ok {
		return fmt.Errorf("Jobs() unavailable during Init after CapJobs bind")
	}
	return nil
}

func TestAdaptInit_CapJobsBindsPort(t *testing.T) {
	res, err := harness.AdaptInit(&needsJobsPlugin{}, harness.Options{
		Satisfy: []pluginsdk.Capability{pluginsdk.CapJobs},
	})
	if err != nil {
		t.Fatalf("AdaptInit: %v", err)
	}
	jp, ok := res.Host.Jobs()
	if !ok || jp == nil {
		t.Fatal("Host.Jobs() must be available after Satisfy CapJobs")
	}
	miss, err := jp.Get(context.Background(), "missing")
	if err != nil || miss != nil {
		t.Fatalf("stub Get miss = (%v, %v), want (nil, nil)", miss, err)
	}
}

type needsMediaPlugin struct{}

func (needsMediaPlugin) Name() string { return "harness/needsmedia" }
func (needsMediaPlugin) Needs() []pluginsdk.Capability {
	return []pluginsdk.Capability{pluginsdk.CapMedia}
}
func (p *needsMediaPlugin) Init(h pluginsdk.Host) error {
	h.Require(pluginsdk.CapMedia)
	if _, ok := h.Media(); !ok {
		return fmt.Errorf("Media() unavailable during Init after CapMedia bind")
	}
	return nil
}

func TestAdaptInit_CapMediaBindsPort(t *testing.T) {
	res, err := harness.AdaptInit(&needsMediaPlugin{}, harness.Options{
		Satisfy: []pluginsdk.Capability{pluginsdk.CapMedia},
	})
	if err != nil {
		t.Fatalf("AdaptInit: %v", err)
	}
	mp, ok := res.Host.Media()
	if !ok || mp == nil || mp.Name() == "" {
		t.Fatal("Host.Media() must be available after Satisfy CapMedia")
	}
}
