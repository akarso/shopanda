package cache_test

import (
	"context"
	"testing"
	"time"

	cacheapp "github.com/akarso/shopanda/internal/application/cache"
)

func TestFPC_AllowlistFailClosed(t *testing.T) {
	if cacheapp.Cacheable("/cart", "/cart") {
		t.Fatal("cart must not be cacheable")
	}
	if cacheapp.Cacheable("/account/orders", "/account/orders") {
		t.Fatal("account must not be cacheable")
	}
	if cacheapp.Allowlisted("/products/{slug}/reviews") {
		t.Fatal("unlisted template must not be allowlisted")
	}
	if !cacheapp.Cacheable(cacheapp.RoutePDP, "/products/oak-desk") {
		t.Fatal("PDP should be cacheable")
	}
	if cacheapp.Cacheable(cacheapp.RoutePDP, "/cart") {
		t.Fatal("allowlisted template + denylisted path must fail closed")
	}
}

func TestFPC_KeyIgnoresCustomerIdentity(t *testing.T) {
	v := cacheapp.Vary{Store: "s1", Language: "en", Currency: "EUR", AuthState: cacheapp.AuthGuest}
	a := cacheapp.Key(cacheapp.RoutePDP, "/products/oak-desk", "", v)
	b := cacheapp.Key(cacheapp.RoutePDP, "/products/oak-desk", "", v)
	if a != b {
		t.Fatalf("identical vary produced different keys:\n%s\n%s", a, b)
	}
	auth := v
	auth.AuthState = cacheapp.AuthAuthenticated
	if cacheapp.Key(cacheapp.RoutePDP, "/products/oak-desk", "", auth) == a {
		t.Fatal("auth_state must change the key")
	}
	otherStore := v
	otherStore.Store = "s2"
	if cacheapp.Key(cacheapp.RoutePDP, "/products/oak-desk", "", otherStore) == a {
		t.Fatal("store must change the key")
	}
	if cacheapp.Key(cacheapp.RouteSearch, "/search", "q=desk", v) == cacheapp.Key(cacheapp.RouteSearch, "/search", "", v) {
		t.Fatal("search q must change the key")
	}
	if cacheapp.Key(cacheapp.RouteSearch, "/search", "q=desk&page=2", v) != cacheapp.Key(cacheapp.RouteSearch, "/search", "page=2&q=desk", v) {
		t.Fatal("listing query keys should canonicalize")
	}
}

func TestFPC_FilterQueryDropsTrackingParams(t *testing.T) {
	v := cacheapp.Vary{Store: "s1", Language: "en", Currency: "EUR", AuthState: cacheapp.AuthGuest}
	clean := cacheapp.Key(cacheapp.RoutePDP, "/products/oak-desk", "", v)
	noisy := cacheapp.Key(cacheapp.RoutePDP, "/products/oak-desk", "utm_source=ad&fbclid=abc&gclid=1", v)
	if clean != noisy {
		t.Fatal("PDP tracking query must not create a distinct key")
	}
	searchClean := cacheapp.Key(cacheapp.RouteSearch, "/search", "q=oak", v)
	searchNoisy := cacheapp.Key(cacheapp.RouteSearch, "/search", "q=oak&utm_source=ad&fbclid=abc", v)
	if searchClean != searchNoisy {
		t.Fatal("listing tracking params must be stripped from the key")
	}
	if got := cacheapp.FilterQuery(cacheapp.RoutePLP, "page=2&attr_color=red&utm_source=x", nil); got != "page=2" {
		t.Fatalf("FilterQuery without attr allowlist = %q, want page=2", got)
	}
	if got := cacheapp.FilterQuery(cacheapp.RoutePLP, "page=2&attr_color=red&attr_notreal=x&utm_source=x", []string{"attr_color"}); got != "attr_color=red&page=2" {
		t.Fatalf("FilterQuery with attr allowlist = %q", got)
	}
	if got := cacheapp.FilterQuery(cacheapp.RoutePDP, "variant=abc&preview=1", nil); got != "" {
		t.Fatalf("PDP query must be dropped until an explicit hook exists, got %q", got)
	}
}

func TestFPC_PageTagsBag(t *testing.T) {
	ctx := cacheapp.ContextWithPageTagBag(context.Background())
	cacheapp.AddPageTags(ctx, cacheapp.ProductTag("p1"), cacheapp.CategoryTag("c1"))
	got := cacheapp.PageTags(ctx)
	if len(got) != 2 || got[0] != "product:p1" || got[1] != "category:c1" {
		t.Fatalf("tags = %#v", got)
	}
	if cacheapp.PageTags(context.Background()) != nil {
		t.Fatal("missing bag should not panic")
	}
}

func TestFPC_RemainingTTL(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	entry := cacheapp.PageEntry{StoredAt: now.Add(-time.Minute), TTLNanos: int64(5 * time.Minute)}
	left := cacheapp.RemainingTTL(entry, now)
	if left < 3*time.Minute+50*time.Second || left > 4*time.Minute+10*time.Second {
		t.Fatalf("remaining = %s", left)
	}
}
