package cache

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/akarso/shopanda/internal/platform/metrics"
)

// FPCObserver records full-page cache hit/miss/bypass and purge activity for
// Prometheus (via Recorder) and the cache admin GUI (process-local snapshot).
// Safe for concurrent use. A nil observer is a no-op at every call site.
//
// Purge counters count confirmed value deletions (not mere observations or
// tag-membership snapshot size). Soft-TTL expiry is not a purge — it
// surfaces as miss after the entry is deleted.
// Backend Get failures use NoteBackendGetError (scrapable series), not a
// request outcome label.
//
// PagesStored is derived from keys this process has stored, each with an
// expiry deadline: hard TTL refill of the same key does not inflate the
// count. Past-deadline entries are pruned on PageStored/Request (throttled)
// and on Snapshot so the map does not grow unboundedly when nobody opens
// cache stats. Tag purges that only report a deletion count do not remove
// map entries — those keys drop out at their tracked expiry (best-effort
// until then).
type FPCObserver struct {
	rec metrics.Recorder

	routes sync.Map // string → *fpcRouteAccum
	pages  sync.Map // string → time.Time (expiresAt; zero = no auto-expiry)

	lastPrune atomic.Int64 // unix nano of last expired-key sweep

	purgeTag         atomic.Int64
	purgeURL         atomic.Int64
	backendGetErrors atomic.Int64
}

// pagesPruneInterval bounds how often PageStored/Request scan pages for
// expired entries. Snapshot always prunes.
const pagesPruneInterval = time.Second

type fpcRouteAccum struct {
	hits, misses, bypasses atomic.Int64
}

// FPCRouteSnapshot is one allowlist template's process-local counters.
type FPCRouteSnapshot struct {
	Route    string  `json:"route"`
	Hits     int64   `json:"hits"`
	Misses   int64   `json:"misses"`
	Bypasses int64   `json:"bypasses"`
	HitRate  float64 `json:"hit_rate"` // hits / (hits+misses); 0 when none
}

// FPCPurgeSnapshot is process-local keys-deleted totals by trigger.
// Tag counts on the serve process exclude worker-bus invalidation — scrape
// shopanda_fpc_purge_total on both serve and worker for the full picture.
type FPCPurgeSnapshot struct {
	TagInvalidation int64 `json:"tag_invalidation"`
	ManualURL       int64 `json:"manual_url"`
}

// FPCSnapshot is the admin-GUI / stats payload for full-page cache (PR-1047).
// Counters are lifetime for this process. PagesStored is a best-effort
// count of tracked keys whose expiry has not elapsed (reset on FlushAll).
// BackendGetErrors mirrors shopanda_fpc_backend_get_errors_total for this
// process.
type FPCSnapshot struct {
	Routes           []FPCRouteSnapshot `json:"routes"`
	PagesStored      int64              `json:"pages_stored"`
	BackendGetErrors int64              `json:"backend_get_errors"`
	Purges           FPCPurgeSnapshot   `json:"purges"`
}

// NewFPCObserver creates an observer. rec may be nil (treated as Noop).
func NewFPCObserver(rec metrics.Recorder) *FPCObserver {
	if rec == nil {
		rec = metrics.Noop()
	}
	return &FPCObserver{rec: rec}
}

// Request records one FPC decision for an allowlist route template.
func (o *FPCObserver) Request(route, outcome string) {
	if o == nil {
		return
	}
	if route == "" {
		route = "unknown"
	}
	switch outcome {
	case metrics.FPCOutcomeHit, metrics.FPCOutcomeMiss, metrics.FPCOutcomeBypass:
	default:
		outcome = metrics.FPCOutcomeBypass
	}
	o.rec.FPCRequest(route, outcome)

	acc := o.routeAccum(route)
	switch outcome {
	case metrics.FPCOutcomeHit:
		acc.hits.Add(1)
	case metrics.FPCOutcomeMiss:
		acc.misses.Add(1)
	default:
		acc.bypasses.Add(1)
	}
	o.maybePruneExpired()
}

func (o *FPCObserver) routeAccum(route string) *fpcRouteAccum {
	if v, ok := o.routes.Load(route); ok {
		return v.(*fpcRouteAccum)
	}
	acc := &fpcRouteAccum{}
	actual, _ := o.routes.LoadOrStore(route, acc)
	return actual.(*fpcRouteAccum)
}

// Render records miss-path render duration for route.
func (o *FPCObserver) Render(route string, d time.Duration) {
	if o == nil {
		return
	}
	if route == "" {
		route = "unknown"
	}
	if d < 0 {
		d = 0
	}
	o.rec.FPCRenderDuration(route, d)
}

// Purge records confirmed value deletions for a known trigger. n is how
// many value keys were removed; n <= 0 is a no-op. Does not adjust
// pages_stored — callers that know the keys should PageGone them; tag
// purges without key lists rely on tracked expiry.
// Unknown triggers use metrics.FPCPurgeUnknown rather than aliasing to tag.
func (o *FPCObserver) Purge(trigger string, n int64) {
	if o == nil || n <= 0 {
		return
	}
	switch trigger {
	case metrics.FPCPurgeTagInvalidation, metrics.FPCPurgeManualURL:
	default:
		trigger = metrics.FPCPurgeUnknown
	}
	o.rec.FPCPurgeKeys(trigger, n)
	switch trigger {
	case metrics.FPCPurgeTagInvalidation:
		o.purgeTag.Add(n)
	case metrics.FPCPurgeManualURL:
		o.purgeURL.Add(n)
	}
}

// PageStored records that key is cached until expiresAt (zero = no
// auto-expiry). Re-storing the same key only refreshes expiry — hard TTL
// refill must not inflate pages_stored.
func (o *FPCObserver) PageStored(key string, expiresAt time.Time) {
	if o == nil || key == "" {
		return
	}
	o.pages.Store(key, expiresAt)
	o.maybePruneExpired()
}

// PageGone removes key from the pages_stored estimate (soft-TTL eviction,
// confirmed manual delete). Does not touch shopanda_fpc_purge_total.
func (o *FPCObserver) PageGone(key string) {
	if o == nil || key == "" {
		return
	}
	o.pages.Delete(key)
}

// NoteBackendGetError records a cache backend Get failure on the FPC path
// (process-local + Prometheus). Call sites must invoke at most once per
// request so a blip does not over-count.
func (o *FPCObserver) NoteBackendGetError() {
	if o == nil {
		return
	}
	o.backendGetErrors.Add(1)
	o.rec.FPCBackendGetError()
}

// ResetPagesStored clears the pages_stored estimate (FlushAll / full clear).
func (o *FPCObserver) ResetPagesStored() {
	if o == nil {
		return
	}
	o.pages.Range(func(key, _ any) bool {
		o.pages.Delete(key)
		return true
	})
}

// maybePruneExpired drops past-deadline page keys at most once per
// pagesPruneInterval so traffic (not only admin Snapshot) reclaims memory.
func (o *FPCObserver) maybePruneExpired() {
	now := time.Now().UTC()
	last := o.lastPrune.Load()
	if last != 0 && now.UnixNano()-last < int64(pagesPruneInterval) {
		return
	}
	if !o.lastPrune.CompareAndSwap(last, now.UnixNano()) {
		return
	}
	o.pruneExpired(now)
}

func (o *FPCObserver) pruneExpired(now time.Time) {
	o.pages.Range(func(key, value any) bool {
		exp, _ := value.(time.Time)
		if !exp.IsZero() && !exp.After(now) {
			// CompareAndDelete: a concurrent PageStored may have refreshed
			// expiry after this Range read — do not drop the new deadline.
			o.pages.CompareAndDelete(key, value)
		}
		return true
	})
}

func (o *FPCObserver) livePages(now time.Time) int64 {
	o.lastPrune.Store(now.UnixNano())
	var n int64
	o.pages.Range(func(key, value any) bool {
		exp, _ := value.(time.Time)
		if !exp.IsZero() && !exp.After(now) {
			o.pages.CompareAndDelete(key, value)
			return true
		}
		n++
		return true
	})
	return n
}

// Snapshot returns process-local FPC counters for the admin GUI.
func (o *FPCObserver) Snapshot() FPCSnapshot {
	if o == nil {
		return FPCSnapshot{Routes: []FPCRouteSnapshot{}}
	}
	var routes []FPCRouteSnapshot
	o.routes.Range(func(key, value any) bool {
		route, _ := key.(string)
		acc := value.(*fpcRouteAccum)
		hits := acc.hits.Load()
		misses := acc.misses.Load()
		bypasses := acc.bypasses.Load()
		denom := hits + misses
		var rate float64
		if denom > 0 {
			rate = float64(hits) / float64(denom)
		}
		routes = append(routes, FPCRouteSnapshot{
			Route:    route,
			Hits:     hits,
			Misses:   misses,
			Bypasses: bypasses,
			HitRate:  rate,
		})
		return true
	})
	sort.Slice(routes, func(i, j int) bool { return routes[i].Route < routes[j].Route })
	return FPCSnapshot{
		Routes:           routes,
		PagesStored:      o.livePages(time.Now().UTC()),
		BackendGetErrors: o.backendGetErrors.Load(),
		Purges: FPCPurgeSnapshot{
			TagInvalidation: o.purgeTag.Load(),
			ManualURL:       o.purgeURL.Load(),
		},
	}
}
