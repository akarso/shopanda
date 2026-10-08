package cache

import (
	"context"
	"net/url"
	"sort"
	"strings"

	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/metrics"
)

// StoreVary is one store's contribution to the FPC vary key.
type StoreVary struct {
	ID       string
	Language string
	Currency string
}

// PurgeURLResult is the POST /admin/cache/purge-url / cache:purge-url payload.
// Deleted is how many of Keys Get reported as a hit and Delete then
// removed — not the expand-list length. Get decode errors still trigger
// Delete (corrupt values) but are not counted here. On a mid-loop
// Get/Delete error, Keys lists the full expand set and Deleted is how
// many confirmed hit+delete pairs succeeded (remaining keys may still be live).
type PurgeURLResult struct {
	Path    string   `json:"path"`
	Keys    []string `json:"keys"`
	Deleted int      `json:"deleted"`
}

// RouteTemplateForPath maps a storefront path to an FPC allowlist template.
// Query strings must be stripped before calling.
func RouteTemplateForPath(path string) (string, bool) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", false
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	path = strings.TrimSuffix(path, "/")
	if path == "" {
		path = "/"
	}
	switch {
	case path == "/":
		return RouteHome, true
	case path == "/products":
		return RoutePLP, true
	case strings.HasPrefix(path, "/products/"):
		return RoutePDP, true
	case path == "/categories":
		return RouteCategories, true
	case strings.HasPrefix(path, "/categories/"):
		return RouteCategory, true
	case path == "/search":
		return RouteSearch, true
	case strings.HasPrefix(path, "/pages/") && path != "/pages":
		return RouteCMS, true
	default:
		return "", false
	}
}

// attrQueryKeysFromRaw returns every attr_* key present in raw so purge-url
// can rebuild the same FilterQuery extras the storefront kept when those
// codes were on the layered-nav / advanced-search allowlist.
func attrQueryKeysFromRaw(raw string) []string {
	values, _ := url.ParseQuery(raw)
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for k := range values {
		if strings.HasPrefix(k, "attr_") && k != "attr_" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

func langFromRawQuery(raw string) string {
	values, _ := url.ParseQuery(raw)
	lang := strings.TrimSpace(values.Get("lang"))
	if lang == "" {
		return ""
	}
	return lang
}

// purgeLanguages collects languages that appear in real FPC vary keys:
// every store default, optional ?lang= from the operator path, and "en"
// (LanguageFromContext fallback). Accept-Language-only variants still need
// an explicit ?lang= on the purge path.
func purgeLanguages(stores []StoreVary, queryLang string) []string {
	seen := make(map[string]struct{})
	var out []string
	add := func(lang string) {
		lang = strings.TrimSpace(lang)
		if lang == "" {
			return
		}
		if _, ok := seen[lang]; ok {
			return
		}
		seen[lang] = struct{}{}
		out = append(out, lang)
	}
	for _, st := range stores {
		add(st.Language)
	}
	add(queryLang)
	add("en")
	if len(out) == 0 {
		add("en")
	}
	return out
}

// PurgeURLKeys builds concrete FPC keys for path across stores × languages ×
// auth states. Listing query params (including attr_* present in the
// operator URL) are kept via FilterQuery and passed through Key's extras so
// attribute filters are not dropped on the second filter pass. Bare
// /products does not expand every page/sort variant — pass the exact query,
// or use DeleteByTag / prefix clear for a whole PLP.
func PurgeURLKeys(rawPath string, stores []StoreVary) (path string, keys []string, err error) {
	rawPath = strings.TrimSpace(rawPath)
	if rawPath == "" {
		return "", nil, apperror.Validation("path is required")
	}
	u, err := url.Parse(rawPath)
	if err != nil || u.Path == "" {
		// Treat as path[+query] without a scheme.
		if !strings.HasPrefix(rawPath, "/") {
			rawPath = "/" + rawPath
		}
		u, err = url.Parse(rawPath)
		if err != nil || u.Path == "" {
			return "", nil, apperror.Validation("invalid path")
		}
	}
	path = u.Path
	if path != "/" {
		path = strings.TrimSuffix(path, "/")
		if path == "" {
			path = "/"
		}
	}
	route, ok := RouteTemplateForPath(path)
	if !ok || !Cacheable(route, path) {
		return "", nil, apperror.Validation("path is not a cacheable storefront route")
	}
	rawQuery := u.RawQuery
	attrExtras := attrQueryKeysFromRaw(rawQuery)
	filtered := FilterQuery(route, rawQuery, attrExtras)

	if len(stores) == 0 {
		// Still purge guest+auth with empty store vary — matches a single
		// default-store deploy that left store ID blank in the key.
		stores = []StoreVary{{}}
	}

	languages := purgeLanguages(stores, langFromRawQuery(rawQuery))
	keys = make([]string, 0, len(stores)*len(languages)*2)
	seen := make(map[string]struct{}, len(stores)*len(languages)*2)
	for _, st := range stores {
		for _, lang := range languages {
			for _, auth := range []string{AuthGuest, AuthAuthenticated} {
				k := Key(route, path, filtered, Vary{
					Store:     st.ID,
					Language:  lang,
					Currency:  st.Currency,
					AuthState: auth,
				}, attrExtras...)
				if _, ok := seen[k]; ok {
					continue
				}
				seen[k] = struct{}{}
				keys = append(keys, k)
			}
		}
	}
	return path, keys, nil
}

// PurgeURL deletes FPC keys for path across the given store vary dimensions.
// Continues after a per-key Get/Delete failure so other vary keys still
// clear; the returned error is the first failure (partial results possible).
// A Get decode/backend error still attempts Delete so corrupt entries do not
// survive; Deleted and purge metrics only count keys where Get hit and
// Delete succeeded (Delete of a missing key is not a confirmed removal).
func (s *AdminService) PurgeURL(ctx context.Context, rawPath string, stores []StoreVary) (PurgeURLResult, error) {
	if s == nil {
		return PurgeURLResult{}, apperror.Internal("cache admin service not configured")
	}
	path, keys, err := PurgeURLKeys(rawPath, stores)
	if err != nil {
		return PurgeURLResult{}, err
	}
	deleted := 0 // Get-hit + Delete success (API Deleted field + purge metric)
	var firstErr error
	for _, key := range keys {
		if err := ctx.Err(); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			break
		}
		var probe PageEntry
		hit, getErr := s.backend.Get(key, &probe)
		if getErr != nil {
			if firstErr == nil {
				firstErr = getErr
			}
			// Key may still exist (corrupt value) — purge for hygiene.
			// Delete success alone is not a confirmed value deletion.
			if err := s.backend.Delete(key); err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			s.fpcObs.PageGone(key)
			continue
		}
		if !hit {
			continue
		}
		if err := s.backend.Delete(key); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		deleted++
		s.fpcObs.PageGone(key)
	}
	// Keys-deleted contract: only confirmed Get-hit + Delete success.
	s.fpcObs.Purge(metrics.FPCPurgeManualURL, int64(deleted))
	res := PurgeURLResult{Path: path, Keys: keys, Deleted: deleted}
	if firstErr != nil {
		return res, firstErr
	}
	return res, nil
}
