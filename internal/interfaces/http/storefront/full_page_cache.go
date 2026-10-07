package storefront

import (
	"bytes"
	"context"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	cacheapp "github.com/akarso/shopanda/internal/application/cache"
	domaincache "github.com/akarso/shopanda/internal/domain/cache"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/store"
	"github.com/akarso/shopanda/internal/domain/translation"
	httpshared "github.com/akarso/shopanda/internal/interfaces/http/shared"
	platformAuth "github.com/akarso/shopanda/internal/platform/auth"
)

const fpcHeader = "X-Shopanda-Cache"

var filledCSRFValue = regexp.MustCompile(`(?i)name=["']csrf_token["'][^>]*value=["']([^"']+)["']|value=["']([^"']+)["'][^>]*name=["']csrf_token["']`)
var logoutFormAction = regexp.MustCompile(`(?i)<form\b[^>]*\baction\s*=\s*(?:"[^"]*/account/logout"|'[^']*/account/logout'|/account/logout)(?:[\s>]|$)`)

// logoutFormBlock captures each logout <form>…</form> so the CSRF hole must
// sit inside that form, not elsewhere on the page.
var logoutFormBlock = regexp.MustCompile(`(?is)<form\b[^>]*\baction\s*=\s*(?:"[^"]*/account/logout"|'[^']*/account/logout'|/account/logout)[^>]*>(.*?)</form>`)

type fpcConfig struct {
	backend      domaincache.Cache
	ttl          time.Duration
	routeTTL     map[string]time.Duration
	exposeHeader bool
}

// WithFullPageCache stores allowlisted storefront HTML in backend (PR-1044).
// ttl is the safety-net default; routeTTL overrides per template. A nil
// backend disables the cache. exposeHeader adds X-Shopanda-Cache (dev).
func (h *StorefrontHandler) WithFullPageCache(backend domaincache.Cache, ttl time.Duration, exposeHeader bool, routeTTL map[string]time.Duration) *StorefrontHandler {
	if backend == nil {
		h.fpc = nil
		return h
	}
	if ttl <= 0 {
		ttl = cacheapp.DefaultPageTTL
	}
	h.fpc = &fpcConfig{
		backend:      backend,
		ttl:          ttl,
		routeTTL:     routeTTL,
		exposeHeader: exposeHeader,
	}
	return h
}

func (h *StorefrontHandler) withFullPageCache(routeTemplate string, inner http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.fpc == nil || r.Method != http.MethodGet || !cacheapp.Cacheable(routeTemplate, r.URL.Path) {
			inner.ServeHTTP(w, r)
			return
		}

		extra, ctx, err := h.fpcQueryContext(r.Context(), routeTemplate)
		if err != nil {
			h.log.Warn("storefront.fpc.attr_allowlist_failed", map[string]interface{}{
				"path":  r.URL.Path,
				"error": err.Error(),
			})
			if h.fpc.exposeHeader {
				w.Header().Set(fpcHeader, "BYPASS")
			}
			inner.ServeHTTP(w, r)
			return
		}
		filtered := cacheapp.FilterQuery(routeTemplate, r.URL.RawQuery, extra)
		r = r.Clone(ctx)
		r.URL.RawQuery = filtered

		key := h.fpcKey(routeTemplate, r, extra)
		ttl := cacheapp.TTLForRoute(routeTemplate, h.fpc.ttl, h.fpc.routeTTL)

		var entry cacheapp.PageEntry
		hit, err := h.fpc.backend.Get(key, &entry)
		if err != nil {
			h.log.Warn("storefront.fpc.get_failed", map[string]interface{}{
				"path":  r.URL.Path,
				"error": err.Error(),
			})
		} else if hit {
			left := cacheapp.RemainingTTL(entry, time.Now().UTC())
			if left > 0 && !fpcHTMLUnsafe(entry.HTML, r) {
				if h.writeFPCHit(w, r, entry, left) {
					return
				}
			}
		}

		buf := &fpcBuffer{header: make(http.Header)}
		inner.ServeHTTP(buf, r)
		html := buf.body.Bytes()
		if buf.code == 0 {
			buf.code = http.StatusOK
		}

		csp := buf.header.Get("Content-Security-Policy")
		nonce := cspNonceFromHeader(csp)
		storeOK := buf.code == http.StatusOK &&
			strings.Contains(strings.ToLower(buf.header.Get("Content-Type")), "text/html") &&
			len(html) > 0 &&
			len(html) <= cacheapp.MaxPageBytes &&
			!fpcHTMLUnsafe(string(html), r) &&
			!cacheapp.StoreSkipped(r.Context()) &&
			fpcCSPNonceStorable(csp, nonce)
		if storeOK {
			now := time.Now().UTC()
			stored := cacheapp.PageEntry{
				HTML:     string(html),
				CSP:      csp,
				Nonce:    nonce,
				StoredAt: now,
				TTLNanos: int64(ttl),
			}
			tags := domaincache.UniqueTags(cacheapp.PageTags(r.Context()))
			if err := h.fpc.backend.SetWithTags(r.Context(), key, stored, ttl, tags...); err != nil {
				h.log.Warn("storefront.fpc.set_failed", map[string]interface{}{
					"path":  r.URL.Path,
					"error": err.Error(),
				})
			}
		}

		copyHeader(w.Header(), buf.header)
		if h.fpc.exposeHeader {
			if storeOK {
				w.Header().Set(fpcHeader, "MISS")
			} else {
				w.Header().Set(fpcHeader, "BYPASS")
			}
		}
		h.applyFPCCacheControl(w, r, ttl, storeOK)
		w.WriteHeader(buf.code)
		_, _ = w.Write(html)
	}
}

func (h *StorefrontHandler) writeFPCHit(w http.ResponseWriter, r *http.Request, entry cacheapp.PageEntry, left time.Duration) bool {
	html, csp, ok := rotateStoredCSPNonce(entry, generateCSPNonce)
	if !ok {
		h.log.Warn("storefront.fpc.nonce_rotate_failed", map[string]interface{}{
			"path": r.URL.Path,
		})
		return false
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	h.applyFPCCacheControl(w, r, left, true)
	if csp != "" {
		w.Header().Set("Content-Security-Policy", csp)
	}
	if h.fpc.exposeHeader {
		w.Header().Set(fpcHeader, "HIT")
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(html))
	return true
}

// applyFPCCacheControl refines guest max-age and never emits public for
// authenticated responses — Phase 10 CacheControlMiddleware's no-store stands.
func (h *StorefrontHandler) applyFPCCacheControl(w http.ResponseWriter, r *http.Request, maxAge time.Duration, stored bool) {
	if !platformAuth.IdentityFrom(r.Context()).IsGuest() {
		w.Header().Set("Cache-Control", "no-store")
		return
	}
	if !stored {
		return
	}
	sec := int(maxAge.Seconds())
	if sec < 0 {
		sec = 0
	}
	w.Header().Set("Cache-Control", "public, max-age="+strconv.Itoa(sec))
}

func (h *StorefrontHandler) fpcKey(routeTemplate string, r *http.Request, extra []string) string {
	return cacheapp.Key(routeTemplate, r.URL.Path, r.URL.RawQuery, h.fpcVary(r), extra...)
}

type fpcAttrBagKey struct{}

type fpcAttrBag struct {
	layered        []catalog.Attribute
	advanced       []catalog.Attribute
	extraKeys      []string
	loadedAdvanced bool
}

func fpcAttrBagFrom(ctx context.Context) *fpcAttrBag {
	bag, _ := ctx.Value(fpcAttrBagKey{}).(*fpcAttrBag)
	return bag
}

// fpcQueryContext loads the listing attr allowlist (fail closed on error),
// stashes it on ctx for the inner handler, and returns extra query keys.
func (h *StorefrontHandler) fpcQueryContext(ctx context.Context, routeTemplate string) ([]string, context.Context, error) {
	ctx = cacheapp.ContextWithPageTagBag(ctx)
	switch routeTemplate {
	case cacheapp.RoutePLP, cacheapp.RouteCategory, cacheapp.RouteCategories:
		bag, err := h.fpcLoadAttrBag(ctx, false)
		if err != nil {
			return nil, ctx, err
		}
		return bag.extraKeys, context.WithValue(ctx, fpcAttrBagKey{}, bag), nil
	case cacheapp.RouteSearch:
		bag, err := h.fpcLoadAttrBag(ctx, true)
		if err != nil {
			return nil, ctx, err
		}
		return bag.extraKeys, context.WithValue(ctx, fpcAttrBagKey{}, bag), nil
	default:
		return nil, ctx, nil
	}
}

func (h *StorefrontHandler) fpcLoadAttrBag(ctx context.Context, includeAdvanced bool) (*fpcAttrBag, error) {
	layered, err := h.fetchLayeredNavAttributes(ctx)
	if err != nil {
		return nil, err
	}
	bag := &fpcAttrBag{layered: layered}
	attrs := append([]catalog.Attribute{}, layered...)
	if includeAdvanced {
		adv, err := h.fetchAdvancedSearchAttributes(ctx)
		if err != nil {
			return nil, err
		}
		bag.advanced = adv
		bag.loadedAdvanced = true
		attrs = append(attrs, adv...)
	}
	seen := make(map[string]struct{}, len(attrs))
	keys := make([]string, 0, len(attrs))
	for _, attr := range attrs {
		if !storefrontAttributeCodeValid(attr.Code) {
			continue
		}
		k := storefrontAttributeQueryPrefix + attr.Code
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		keys = append(keys, k)
	}
	bag.extraKeys = keys
	return bag, nil
}

func (h *StorefrontHandler) fpcVary(r *http.Request) cacheapp.Vary {
	v := cacheapp.Vary{
		Language:  translation.LanguageFromContext(r.Context()),
		AuthState: cacheapp.AuthGuest,
	}
	if s := store.FromContext(r.Context()); s != nil {
		v.Store = s.ID
		v.Currency = s.Currency
		if v.Language == "" {
			v.Language = s.Language
		}
	}
	if !platformAuth.IdentityFrom(r.Context()).IsGuest() {
		v.AuthState = cacheapp.AuthAuthenticated
	}
	return v
}

func (h *StorefrontHandler) layoutForCacheablePage(r *http.Request, categories []catalog.Category) StorefrontLayoutData {
	if h.fpc == nil {
		if categories != nil {
			return h.buildLayoutData(r, categories, false)
		}
		return h.layoutDataBestEffort(r)
	}
	if categories == nil {
		cats, err := h.cachedCategories(r.Context())
		if err != nil {
			h.log.Warn("storefront.categories.load_failed", map[string]interface{}{
				"path":  r.URL.Path,
				"error": err.Error(),
			})
			cacheapp.SkipStore(r.Context())
		} else {
			categories = cats
		}
	}
	addCategoryTreeTags(r.Context(), categories)
	return h.buildLayoutData(r, categories, true)
}

func fpcHTMLUnsafe(html string, r *http.Request) bool {
	if r == nil {
		return true
	}
	token := httpshared.CSRFToken(r)
	if token != "" && strings.Contains(html, token) {
		return true
	}
	// Empty csrf_token skeletons + logout forms are OK only when each logout
	// form contains a working htmx CSRF placeholder (hx-get). A matching
	// string elsewhere on the page, or a non-functional marker, does not pass.
	if fpcFilledCSRFInput(html) {
		return true
	}
	if fpcLogoutFormMissingCSRFHole(html) {
		return true
	}
	id := platformAuth.IdentityFrom(r.Context())
	if id.IsGuest() {
		return false
	}
	if id.UserID != "" && strings.Contains(html, id.UserID) {
		return true
	}
	name := strings.TrimSpace(id.DisplayName)
	if len(name) >= 2 && !strings.EqualFold(name, "account") && containsFold(html, name) {
		return true
	}
	return false
}

func fpcFilledCSRFInput(html string) bool {
	m := filledCSRFValue.FindStringSubmatch(html)
	if m == nil {
		return false
	}
	for i := 1; i < len(m); i++ {
		if strings.TrimSpace(m[i]) != "" {
			return true
		}
	}
	return false
}

func fpcLogoutFormMissingCSRFHole(html string) bool {
	matches := logoutFormBlock.FindAllStringSubmatch(html, -1)
	if len(matches) == 0 {
		// Malformed/open logout form still matched by action scan — treat as unsafe.
		return logoutFormAction.MatchString(html)
	}
	for _, m := range matches {
		body := m[1]
		if !fpcFormHasCSRFFragmentHole(body) {
			return true
		}
	}
	return false
}

func fpcFormHasCSRFFragmentHole(formInnerHTML string) bool {
	return strings.Contains(formInnerHTML, `hx-get="/fragments/csrf"`) ||
		strings.Contains(formInnerHTML, `hx-get='/fragments/csrf'`)
}

func containsFold(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

type fpcBuffer struct {
	header http.Header
	code   int
	body   bytes.Buffer
	wrote  bool
}

func (b *fpcBuffer) Header() http.Header { return b.header }

func (b *fpcBuffer) Write(p []byte) (int, error) {
	if !b.wrote {
		b.WriteHeader(http.StatusOK)
	}
	return b.body.Write(p)
}

func (b *fpcBuffer) WriteHeader(code int) {
	if b.wrote {
		return
	}
	b.wrote = true
	b.code = code
}

func copyHeader(dst, src http.Header) {
	for k, vs := range src {
		dst[k] = append([]string(nil), vs...)
	}
}

func cspNonceFromHeader(csp string) string {
	const prefix = "'nonce-"
	i := strings.Index(csp, prefix)
	if i < 0 {
		return ""
	}
	rest := csp[i+len(prefix):]
	end := strings.IndexByte(rest, '\'')
	if end <= 0 {
		return ""
	}
	return rest[:end]
}

func fpcCSPNonceStorable(csp, nonce string) bool {
	if !strings.Contains(csp, "'nonce-") {
		return true
	}
	return nonce != ""
}

func rotateStoredCSPNonce(entry cacheapp.PageEntry, newNonce func() string) (html, csp string, ok bool) {
	html, csp = entry.HTML, entry.CSP
	old := strings.TrimSpace(entry.Nonce)
	if old == "" {
		old = cspNonceFromHeader(csp)
	}
	if old == "" {
		return html, csp, true
	}
	if newNonce == nil {
		return html, csp, false
	}
	n := newNonce()
	if n == "" {
		return html, csp, false
	}
	html, csp = applyCSPNonceRotation(html, csp, old, n)
	return html, csp, true
}

// applyCSPNonceRotation swaps old→new in the CSP header (raw) and in HTML
// using html/template's attribute escaping (+ → &#43;). A raw HTML replace
// covers themes that embed the nonce without that escaping.
func applyCSPNonceRotation(html, csp, old, n string) (string, string) {
	html = strings.ReplaceAll(html, cspNonceHTMLForm(old), cspNonceHTMLForm(n))
	html = strings.ReplaceAll(html, old, n)
	csp = strings.ReplaceAll(csp, old, n)
	return html, csp
}

// cspNonceHTMLForm matches how html/template embeds a Base64 nonce in
// text/attribute contexts (only '+' among the Base64 alphabet is escaped).
func cspNonceHTMLForm(nonce string) string {
	return strings.ReplaceAll(nonce, "+", "&#43;")
}
