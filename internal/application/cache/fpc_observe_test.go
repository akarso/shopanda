package cache_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	cacheApp "github.com/akarso/shopanda/internal/application/cache"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/metrics"
)

type recordingFPCMetrics struct {
	mu          sync.Mutex
	reqs        [][2]string
	backendGets int
	purges      []struct {
		trigger string
		n       int64
	}
}

func (m *recordingFPCMetrics) HTTPRequest(string, string, string, time.Duration) {}
func (m *recordingFPCMetrics) CheckoutResult(string)                             {}
func (m *recordingFPCMetrics) JobFailure(string)                                 {}
func (m *recordingFPCMetrics) WebhookDelivery(string)                            {}
func (m *recordingFPCMetrics) RateLimitBackendError(string, string)              {}
func (m *recordingFPCMetrics) FPCRenderDuration(string, time.Duration)           {}
func (m *recordingFPCMetrics) FPCBackendGetError() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.backendGets++
}
func (m *recordingFPCMetrics) FPCRequest(route, outcome string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reqs = append(m.reqs, [2]string{route, outcome})
}
func (m *recordingFPCMetrics) FPCPurgeKeys(trigger string, n int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.purges = append(m.purges, struct {
		trigger string
		n       int64
	}{trigger, n})
}

func TestFPCObserver_HitRateExcludesBypass(t *testing.T) {
	obs := cacheApp.NewFPCObserver(metrics.Noop())
	obs.Request(cacheApp.RoutePDP, metrics.FPCOutcomeHit)
	obs.Request(cacheApp.RoutePDP, metrics.FPCOutcomeMiss)
	obs.Request(cacheApp.RoutePDP, metrics.FPCOutcomeBypass)
	snap := obs.Snapshot()
	if len(snap.Routes) != 1 {
		t.Fatalf("routes = %d", len(snap.Routes))
	}
	r := snap.Routes[0]
	if r.Hits != 1 || r.Misses != 1 || r.Bypasses != 1 {
		t.Fatalf("counters = %+v", r)
	}
	if r.HitRate < 0.49 || r.HitRate > 0.51 {
		t.Fatalf("hit_rate = %v, want 0.5 (hits/(hits+misses))", r.HitRate)
	}
}

func TestFPCObserver_NoteBackendGetErrorForwards(t *testing.T) {
	rec := &recordingFPCMetrics{}
	obs := cacheApp.NewFPCObserver(rec)
	obs.NoteBackendGetError()
	obs.NoteBackendGetError()
	if obs.Snapshot().BackendGetErrors != 2 {
		t.Fatalf("local = %d", obs.Snapshot().BackendGetErrors)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if rec.backendGets != 2 {
		t.Fatalf("prom forwards = %d", rec.backendGets)
	}
}

func TestFPCObserver_PageStoredTracksKeyExpiry(t *testing.T) {
	rec := &recordingFPCMetrics{}
	obs := cacheApp.NewFPCObserver(rec)
	exp := time.Now().UTC().Add(time.Hour)
	obs.PageStored("k1", exp)
	obs.PageStored("k2", exp)
	obs.PageStored("k1", exp) // refill same key — no inflate
	obs.Purge(metrics.FPCPurgeTagInvalidation, 0)
	obs.Purge(metrics.FPCPurgeManualURL, 0)
	obs.Purge("bogus-trigger", 2)

	snap := obs.Snapshot()
	if snap.PagesStored != 2 {
		t.Fatalf("pages_stored = %d, want 2", snap.PagesStored)
	}
	if snap.Purges.TagInvalidation != 0 || snap.Purges.ManualURL != 0 {
		t.Fatalf("known purges = %+v, want zeros", snap.Purges)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.purges) != 1 || rec.purges[0].trigger != metrics.FPCPurgeUnknown || rec.purges[0].n != 2 {
		t.Fatalf("recorder purges = %#v", rec.purges)
	}
}

func TestFPCObserver_PageStoredPrunesExpired(t *testing.T) {
	obs := cacheApp.NewFPCObserver(metrics.Noop())
	obs.PageStored("live", time.Now().UTC().Add(time.Hour))
	obs.PageStored("gone", time.Now().UTC().Add(-time.Second))
	if got := obs.Snapshot().PagesStored; got != 1 {
		t.Fatalf("pages_stored = %d, want 1 (expired pruned)", got)
	}
}

func TestFPCObserver_ResetPagesStored(t *testing.T) {
	obs := cacheApp.NewFPCObserver(metrics.Noop())
	obs.PageStored("k", time.Now().UTC().Add(time.Hour))
	obs.ResetPagesStored()
	if obs.Snapshot().PagesStored != 0 {
		t.Fatal("expected reset")
	}
}

func TestFPCObserver_ForwardsToRecorder(t *testing.T) {
	rec := &recordingFPCMetrics{}
	obs := cacheApp.NewFPCObserver(rec)
	obs.Request(cacheApp.RoutePLP, metrics.FPCOutcomeHit)
	obs.Purge(metrics.FPCPurgeTagInvalidation, 4)
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.reqs) != 1 || rec.reqs[0] != [2]string{cacheApp.RoutePLP, metrics.FPCOutcomeHit} {
		t.Fatalf("reqs = %#v", rec.reqs)
	}
	if len(rec.purges) != 1 || rec.purges[0].n != 4 {
		t.Fatalf("purges = %#v", rec.purges)
	}
}

func TestFPCObserver_PurgeDoesNotTouchPagesStored(t *testing.T) {
	obs := cacheApp.NewFPCObserver(metrics.Noop())
	obs.PageStored("k", time.Now().UTC().Add(time.Hour))
	obs.Purge(metrics.FPCPurgeTagInvalidation, 4)
	if obs.Snapshot().PagesStored != 1 {
		t.Fatal("tag purge count must not blindly subtract pages_stored")
	}
}

func TestFPCObserver_ConcurrentRequests(t *testing.T) {
	obs := cacheApp.NewFPCObserver(metrics.Noop())
	var wg sync.WaitGroup
	const n = 200
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				obs.Request(cacheApp.RoutePDP, metrics.FPCOutcomeHit)
			} else {
				obs.Request(cacheApp.RoutePDP, metrics.FPCOutcomeMiss)
			}
		}(i)
	}
	wg.Wait()
	snap := obs.Snapshot()
	r := snap.Routes[0]
	if r.Hits+r.Misses != n {
		t.Fatalf("hits+misses = %d, want %d", r.Hits+r.Misses, n)
	}
}

func TestFPCObserver_NilSafe(t *testing.T) {
	var obs *cacheApp.FPCObserver
	obs.Request(cacheApp.RouteHome, metrics.FPCOutcomeHit)
	obs.Render(cacheApp.RouteHome, time.Millisecond)
	obs.Purge(metrics.FPCPurgeManualURL, 1)
	obs.PageStored("k", time.Now().UTC().Add(time.Hour))
	obs.PageGone("k")
	obs.ResetPagesStored()
	_ = obs.Snapshot()
}

func TestAdminClear_ResetsPagesStored(t *testing.T) {
	backend := newTagMemCache()
	obs := cacheApp.NewFPCObserver(metrics.Noop())
	obs.PageStored("a", time.Now().UTC().Add(time.Hour))
	obs.PageStored("b", time.Now().UTC().Add(time.Hour))
	svc := cacheApp.NewAdminService(backend, nil).WithFPCObserver(obs)
	if _, err := svc.Clear(context.Background(), cacheApp.ClearRequest{All: true}); err != nil {
		t.Fatal(err)
	}
	if obs.Snapshot().PagesStored != 0 {
		t.Fatal("FlushAll must reset pages_stored")
	}
}

func TestAdminClear_NonFPCTagDoesNotFeedObserver(t *testing.T) {
	backend := newTagMemCache()
	obs := cacheApp.NewFPCObserver(metrics.Noop())
	obs.PageStored("k", time.Now().UTC().Add(time.Hour))
	ctx := context.Background()
	_ = backend.SetWithTags(ctx, "other:1", cacheApp.PageEntry{HTML: "x"}, time.Minute, "ratelimit:bucket")
	svc := cacheApp.NewAdminService(backend, nil).WithFPCObserver(obs)
	res, err := svc.Clear(ctx, cacheApp.ClearRequest{Tag: "ratelimit:bucket"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Deleted == nil || *res.Deleted != 1 {
		t.Fatalf("deleted = %v", res.Deleted)
	}
	if obs.Snapshot().Purges.TagInvalidation != 0 {
		t.Fatal("non-FPC tag must not increment FPC tag purge")
	}
	if obs.Snapshot().PagesStored != 1 {
		t.Fatal("non-FPC tag clear must not remove tracked FPC pages")
	}
}

func TestAdminClear_KeyMissPropagatesDeleteError(t *testing.T) {
	backend := newTagMemCache()
	backend.deleteErr = errors.New("delete failed")
	obs := cacheApp.NewFPCObserver(metrics.Noop())
	svc := cacheApp.NewAdminService(backend, nil).WithFPCObserver(obs)
	key := cacheApp.Key(cacheApp.RoutePDP, "/products/x", "", cacheApp.Vary{AuthState: cacheApp.AuthGuest})
	_, err := svc.Clear(context.Background(), cacheApp.ClearRequest{Key: key})
	if err == nil {
		t.Fatal("want delete error on FPC key miss")
	}
	if obs.Snapshot().Purges.ManualURL != 0 {
		t.Fatal("failed clear must not bump purge metrics")
	}
}

func TestIsFPCTag(t *testing.T) {
	for _, tag := range []string{cacheApp.ListingTag(), cacheApp.ProductTag("p1"), cacheApp.CategoryTag("c1"), cacheApp.PageTag("pg")} {
		if !cacheApp.IsFPCTag(tag) {
			t.Fatalf("%q should be FPC tag", tag)
		}
	}
	if cacheApp.IsFPCTag("ratelimit:bucket") {
		t.Fatal("unrelated tag must not be FPC")
	}
}

func TestFPCObserver_PageGoneDoesNotPurge(t *testing.T) {
	rec := &recordingFPCMetrics{}
	obs := cacheApp.NewFPCObserver(rec)
	obs.PageStored("k", time.Now().UTC().Add(time.Hour))
	obs.PageGone("k")
	if obs.Snapshot().PagesStored != 0 {
		t.Fatal("PageGone must drop pages_stored")
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.purges) != 0 {
		t.Fatalf("PageGone must not emit purge metrics: %#v", rec.purges)
	}
}

func TestAdminStats_IncludesFPC(t *testing.T) {
	backend := newTagMemCache()
	obs := cacheApp.NewFPCObserver(metrics.Noop())
	obs.Request(cacheApp.RouteHome, metrics.FPCOutcomeHit)
	svc := cacheApp.NewAdminService(backend, nil).WithFPCObserver(obs)
	snap, err := svc.Stats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snap.FPC == nil || len(snap.FPC.Routes) != 1 {
		t.Fatalf("fpc = %#v", snap.FPC)
	}
}

func TestPurgeURL_ObserverCountsConfirmedHitsOnly(t *testing.T) {
	inner := newTagMemCache()
	stores := []cacheApp.StoreVary{{ID: "s1", Language: "en", Currency: "EUR"}}
	path := "/products/widget"
	guest := cacheApp.Key(cacheApp.RoutePDP, path, "", cacheApp.Vary{Store: "s1", Language: "en", Currency: "EUR", AuthState: cacheApp.AuthGuest})
	auth := cacheApp.Key(cacheApp.RoutePDP, path, "", cacheApp.Vary{Store: "s1", Language: "en", Currency: "EUR", AuthState: cacheApp.AuthAuthenticated})
	_ = inner.Set(guest, cacheApp.PageEntry{HTML: "g"}, time.Minute)
	_ = inner.Set(auth, cacheApp.PageEntry{HTML: "a"}, time.Minute)

	rec := &recordingFPCMetrics{}
	obs := cacheApp.NewFPCObserver(rec)
	exp := time.Now().UTC().Add(time.Hour)
	obs.PageStored(guest, exp)
	obs.PageStored(auth, exp)
	backend := &decodeErrCache{tagMemCache: inner, errKeys: map[string]error{guest: errors.New("corrupt")}}
	svc := cacheApp.NewAdminService(backend, nil).WithFPCObserver(obs)
	res, err := svc.PurgeURL(context.Background(), path, stores)
	if err == nil {
		t.Fatal("want decode error preserved")
	}
	if res.Deleted != 1 {
		t.Fatalf("deleted = %d, want 1 (auth hit only)", res.Deleted)
	}
	if obs.Snapshot().PagesStored != 0 {
		t.Fatalf("pages_stored = %d, want 0 (PageGone on hit + corrupt delete)", obs.Snapshot().PagesStored)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.purges) != 1 || rec.purges[0].n != 1 {
		t.Fatalf("purges = %#v, want n=1 (confirmed hit only)", rec.purges)
	}
}

func TestFPCInvalidation_ObserverOnTagDelete(t *testing.T) {
	backend := newTagMemCache()
	rec := &recordingFPCMetrics{}
	obs := cacheApp.NewFPCObserver(rec)
	ctx := context.Background()
	key := cacheApp.Key(cacheApp.RoutePDP, "/products/widget", "", cacheApp.Vary{AuthState: cacheApp.AuthGuest})
	_ = backend.SetWithTags(ctx, key, cacheApp.PageEntry{HTML: "x"}, time.Minute, cacheApp.ProductTag("p1"))
	obs.PageStored(key, time.Now().UTC().Add(time.Hour))

	sub := cacheApp.NewFPCInvalidationSubscriber(backend, &mockLogger{}).WithObserver(obs)
	if err := sub.HandleProductUpdated(ctx, event.New(catalog.EventProductUpdated, "test", catalog.ProductUpdatedData{ProductID: "p1"})); err != nil {
		t.Fatal(err)
	}
	// ProductUpdated purges listing tag (may be 0) then product tag (1).
	if obs.Snapshot().Purges.TagInvalidation < 1 {
		t.Fatalf("tag purge = %d", obs.Snapshot().Purges.TagInvalidation)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	var total int64
	for _, p := range rec.purges {
		if p.trigger != metrics.FPCPurgeTagInvalidation {
			t.Fatalf("unexpected trigger %q", p.trigger)
		}
		total += p.n
	}
	if total < 1 {
		t.Fatalf("recorded keys deleted = %d", total)
	}
}
