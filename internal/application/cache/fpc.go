package cache

import (
	"context"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Full-page cache route templates (PR-1044). A template must be on this
// allowlist to be stored; new storefront routes stay uncached until added.
const (
	RouteHome       = "/"
	RoutePDP        = "/products/{slug}"
	RoutePLP        = "/products"
	RouteCategory   = "/categories/{slug}"
	RouteCategories = "/categories"
	RouteSearch     = "/search"
	RouteCMS        = "/pages/{slug}"

	AuthGuest         = "guest"
	AuthAuthenticated = "authenticated"

	DefaultPageTTL = 5 * time.Minute
	// MaxPageBytes is a safety cap so one huge HTML document cannot dominate L2.
	MaxPageBytes = 1 << 20
)

const fpcKeyPrefix = "fpc:v1:"

var allowlist = map[string]struct{}{
	RouteHome:       {},
	RoutePDP:        {},
	RoutePLP:        {},
	RouteCategory:   {},
	RouteCategories: {},
	RouteSearch:     {},
	RouteCMS:        {},
}

// Paths that must never be cached, even if someone adds the template to
// the allowlist. Checked independently of the allowlist (fail closed).
var denyPrefixes = []string{
	"/cart",
	"/checkout",
	"/account",
	"/admin",
	"/fragments",
	"/api",
	"/setup",
}

// listingQueryKeys are the only query params that change listing HTML.
// Tracking params (utm_*, fbclid, …) are dropped from the key.
var listingQueryKeys = map[string]struct{}{
	"q":        {},
	"page":     {},
	"per_page": {},
	"sort":     {},
	"view":     {},
	"category": {},
}

// Vary is the coarse cache dimension set. AuthState is guest vs
// authenticated — never a session or customer ID.
type Vary struct {
	Store     string
	Language  string
	Currency  string
	AuthState string
}

// PageEntry is the JSON value stored under an FPC key.
type PageEntry struct {
	HTML     string    `json:"html"`
	CSP      string    `json:"csp,omitempty"`
	Nonce    string    `json:"nonce,omitempty"`
	StoredAt time.Time `json:"stored_at"`
	TTLNanos int64     `json:"ttl_nanos"`
}

// Allowlisted reports whether routeTemplate is an explicit cacheable route.
func Allowlisted(routeTemplate string) bool {
	_, ok := allowlist[routeTemplate]
	return ok
}

// DeniedPath reports whether path is in a structurally uncacheable group.
func DeniedPath(path string) bool {
	if path == "" {
		return true
	}
	for _, prefix := range denyPrefixes {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}

// Cacheable is true only when the template is allowlisted AND the path
// is not denylisted. Both checks are required; neither is sufficient.
func Cacheable(routeTemplate, path string) bool {
	return Allowlisted(routeTemplate) && !DeniedPath(path)
}

// Key builds a stable FPC cache key. path and filtered query identify the
// page instance; vary must not include session or customer identity.
// extraQueryKeys are additional listing params (configured attr_* codes)
// kept besides the built-in listingQueryKeys set.
func Key(routeTemplate, path, rawQuery string, v Vary, extraQueryKeys ...string) string {
	if v.AuthState != AuthAuthenticated {
		v.AuthState = AuthGuest
	}
	return fpcKeyPrefix + strings.Join([]string{
		routeTemplate,
		path,
		FilterQuery(routeTemplate, rawQuery, extraQueryKeys),
		v.Store,
		v.Language,
		v.Currency,
		v.AuthState,
	}, "\x1f")
}

// FilterQuery keeps only params that affect the rendered page for the
// route template. Home/PDP/CMS drop query entirely — a new query that
// changes those pages' HTML needs an explicit extraQueryKeys / listing
// allowlist hook, not an ad-hoc RawQuery read in the handler.
// extraQueryKeys is the layered-nav / advanced-search attr_* set from
// the storefront; unknown attr_* codes are dropped (cache-fill).
// Repeated values keep the original first value (url.Values.Get).
func FilterQuery(routeTemplate, raw string, extraQueryKeys []string) string {
	// ParseQuery may return both values and an error (invalid escapes).
	// Keep any successfully parsed pairs; only empty results drop the query.
	values, _ := url.ParseQuery(raw)
	if len(values) == 0 {
		return ""
	}
	extra := make(map[string]struct{}, len(extraQueryKeys))
	for _, k := range extraQueryKeys {
		if k != "" {
			extra[k] = struct{}{}
		}
	}
	keys := make([]string, 0, len(values))
	for k := range values {
		if queryKeyAllowed(routeTemplate, k, extra) {
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		return ""
	}
	sort.Strings(keys)
	var b strings.Builder
	first := true
	for _, k := range keys {
		vs := values[k]
		if len(vs) == 0 {
			continue
		}
		// Keep the original first value. Listing fields (page, sort, …)
		// use url.Values.Get, so sorting repeats would change the page
		// the handler renders (page=2&page=1 must stay page 2).
		if !first {
			b.WriteByte('&')
		}
		first = false
		b.WriteString(url.QueryEscape(k))
		b.WriteByte('=')
		b.WriteString(url.QueryEscape(vs[0]))
	}
	return b.String()
}

func queryKeyAllowed(routeTemplate, key string, extra map[string]struct{}) bool {
	switch routeTemplate {
	case RoutePLP, RouteSearch, RouteCategory, RouteCategories:
		if _, ok := listingQueryKeys[key]; ok {
			return true
		}
		_, ok := extra[key]
		return ok
	}
	return false
}

// ProductTag / CategoryTag / CMSTag / PageTag are DeleteByTag names
// PR-1046 fires. ListingTag / NavigationTag are shared shells: a newly
// created product/category cannot appear under product:/category: tags
// that no cached page has yet, so listings and nav trees carry these
// shared tags and creation (and membership-changing updates) purge them.
func ProductTag(id string) string  { return tagged("product", id) }
func CategoryTag(id string) string { return tagged("category", id) }
func CMSTag(id string) string      { return tagged("cms", id) }
func PageTag(id string) string     { return tagged("page", id) }

// ListingTag marks PLP / search / category listing HTML.
func ListingTag() string { return "fpc:listing" }

// NavigationTag marks pages whose chrome rendered the category tree.
func NavigationTag() string { return "fpc:navigation" }

func tagged(kind, id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	return kind + ":" + id
}

type pageTagBagKey struct{}

type pageTagBag struct {
	tags []string
	skip bool
}

// ContextWithPageTagBag installs a bag handlers append tags into.
func ContextWithPageTagBag(ctx context.Context) context.Context {
	return context.WithValue(ctx, pageTagBagKey{}, &pageTagBag{})
}

func pageTagBagFrom(ctx context.Context) *pageTagBag {
	bag, ok := ctx.Value(pageTagBagKey{}).(*pageTagBag)
	if !ok {
		return nil
	}
	return bag
}

// HasPageTagBag reports whether ctx is collecting tags for an FPC miss.
func HasPageTagBag(ctx context.Context) bool {
	return pageTagBagFrom(ctx) != nil
}

// AddPageTags records entity tags for the in-flight cacheable render.
func AddPageTags(ctx context.Context, tags ...string) {
	bag := pageTagBagFrom(ctx)
	if bag == nil {
		return
	}
	bag.tags = append(bag.tags, tags...)
}

// SkipStore marks a best-effort 200 as uncacheable (incomplete content).
func SkipStore(ctx context.Context) {
	bag := pageTagBagFrom(ctx)
	if bag == nil {
		return
	}
	bag.skip = true
}

// StoreSkipped reports whether SkipStore was called on ctx.
func StoreSkipped(ctx context.Context) bool {
	bag := pageTagBagFrom(ctx)
	return bag != nil && bag.skip
}

// PageTags returns tags recorded on ctx (may be empty).
func PageTags(ctx context.Context) []string {
	bag := pageTagBagFrom(ctx)
	if bag == nil {
		return nil
	}
	return bag.tags
}

// RemainingTTL is the Cache-Control max-age for a stored entry.
func RemainingTTL(entry PageEntry, now time.Time) time.Duration {
	if entry.TTLNanos <= 0 {
		return 0
	}
	left := time.Duration(entry.TTLNanos) - now.Sub(entry.StoredAt)
	if left < 0 {
		return 0
	}
	return left
}

// TTLForRoute returns route-specific TTL or the default.
func TTLForRoute(routeTemplate string, defaultTTL time.Duration, routeTTL map[string]time.Duration) time.Duration {
	if routeTTL != nil {
		if d, ok := routeTTL[routeTemplate]; ok && d > 0 {
			return d
		}
	}
	if defaultTTL > 0 {
		return defaultTTL
	}
	return DefaultPageTTL
}
