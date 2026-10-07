package storefront

import (
	"bytes"
	"html"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/akarso/shopanda/internal/domain/catalog"
	httpshared "github.com/akarso/shopanda/internal/interfaces/http/shared"
	platformAuth "github.com/akarso/shopanda/internal/platform/auth"
)

const (
	recentlyViewedCookie   = "shopanda_recently_viewed"
	recentlyViewedMaxItems = 8
	recentlyViewedMaxAge   = 30 * 24 * time.Hour
	recentlyViewedMaxIDLen = 64
)

var storefrontCSRFFragmentTemplate = template.Must(template.New("storefront-csrf-fragment").Parse(
	`<input type="hidden" name="csrf_token" value="{{.}}">`,
))

var storefrontGreetingFragmentTemplate = template.Must(template.New("storefront-greeting-fragment").Parse(
	`<strong class="account-greeting">{{.}}</strong>`,
))

var storefrontRecentlyViewedTemplate = template.Must(template.New("storefront-recently-viewed").Parse(`
<section class="recently-viewed" aria-label="Recently viewed">
    <h2>Recently viewed</h2>
    {{ if .Items }}
    <ul>
        {{ range .Items }}
        <li><a href="/products/{{ .Slug }}">{{ .Name }}</a></li>
        {{ end }}
    </ul>
    {{ else }}
    <p class="muted-note">{{ .EmptyMessage }}</p>
    {{ end }}
</section>`))

type recentlyViewedItem struct {
	Name string
	Slug string
}

type recentlyViewedData struct {
	Items        []recentlyViewedItem
	EmptyMessage string
}

// withFragmentCacheControl forces Cache-Control: no-store on every fragment
// response. Fragments are the always-live holes in an otherwise-cacheable page
// (PR-1045); caching them would defeat the mechanism.
func withFragmentCacheControl(inner http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		inner(w, r)
	}
}

// CSRFFragment handles GET /fragments/csrf — per-request token for forms on
// cached shells (never baked into the full-page cache entry). Mints a cookie
// when missing so logout works without visiting /account/* first.
func (h *StorefrontHandler) CSRFFragment() http.HandlerFunc {
	return withFragmentCacheControl(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		token, err := httpshared.EnsureCSRFToken(w, r, h.trustedProxies)
		if err != nil {
			h.log.Warn("storefront.fragment.csrf_ensure_failed", map[string]interface{}{
				"path":  r.URL.Path,
				"error": err.Error(),
			})
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		var buf bytes.Buffer
		if err := storefrontCSRFFragmentTemplate.Execute(&buf, token); err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(buf.Bytes())
	})
}

// GreetingFragment handles GET /fragments/greeting — display name for signed-in
// chrome; guests get a generic label so the shell stays shareable.
func (h *StorefrontHandler) GreetingFragment() http.HandlerFunc {
	return withFragmentCacheControl(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		identity := platformAuth.IdentityFrom(r.Context())
		label := "Account"
		if !identity.IsGuest() {
			label = h.storefrontAccountDisplayName(identity.UserID, identity.DisplayName)
		}
		var buf bytes.Buffer
		if err := storefrontGreetingFragmentTemplate.Execute(&buf, label); err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(buf.Bytes())
	})
}

// RecentlyViewedFragment handles GET /fragments/recently-viewed.
// Optional ?add=<productID> records a view only when the product exists
// (so FPC HIT PDPs still update history without accepting junk IDs).
func (h *StorefrontHandler) RecentlyViewedFragment() http.HandlerFunc {
	return withFragmentCacheControl(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		ids := readRecentlyViewedIDs(r)
		resolved := map[string]recentlyViewedItem{}
		if add := strings.TrimSpace(r.URL.Query().Get("add")); add != "" && len(add) <= recentlyViewedMaxIDLen && h.repo != nil {
			p, err := h.repo.FindByID(r.Context(), add)
			if err == nil && recentlyViewedProductOK(p) {
				h.recordRecentlyViewedID(w, r, p.ID)
				ids = prependRecentlyViewedID(ids, p.ID)
				resolved[p.ID] = recentlyViewedItemFromProduct(p)
			}
		}
		data := recentlyViewedData{EmptyMessage: "No recently viewed products yet."}
		if len(ids) > 0 && h.repo != nil {
			items := make([]recentlyViewedItem, 0, len(ids))
			for _, id := range ids {
				if item, ok := resolved[id]; ok {
					items = append(items, item)
					continue
				}
				p, err := h.repo.FindByID(r.Context(), id)
				if err != nil || !recentlyViewedProductOK(p) {
					continue
				}
				items = append(items, recentlyViewedItemFromProduct(p))
			}
			data.Items = items
			if len(items) == 0 {
				data.EmptyMessage = "Recently viewed products are no longer available."
			}
		}
		var buf bytes.Buffer
		if err := storefrontRecentlyViewedTemplate.Execute(&buf, data); err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(buf.Bytes())
	})
}

func recentlyViewedProductOK(p *catalog.Product) bool {
	return p != nil && p.Status == catalog.StatusActive && strings.TrimSpace(p.Slug) != ""
}

func recentlyViewedItemFromProduct(p *catalog.Product) recentlyViewedItem {
	name := strings.TrimSpace(p.Name)
	if name == "" {
		name = p.Slug
	}
	return recentlyViewedItem{Name: name, Slug: p.Slug}
}

func (h *StorefrontHandler) recordRecentlyViewedID(w http.ResponseWriter, r *http.Request, productID string) {
	productID = strings.TrimSpace(productID)
	if w == nil || r == nil || productID == "" || len(productID) > recentlyViewedMaxIDLen {
		return
	}
	next := prependRecentlyViewedID(readRecentlyViewedIDs(r), productID)
	http.SetCookie(w, &http.Cookie{
		Name:     recentlyViewedCookie,
		Value:    encodeRecentlyViewedIDs(next),
		Path:     "/",
		MaxAge:   int(recentlyViewedMaxAge.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   httpshared.IsRequestSecure(r, h.trustedProxies),
	})
}

func prependRecentlyViewedID(ids []string, productID string) []string {
	productID = strings.TrimSpace(productID)
	if productID == "" {
		return ids
	}
	next := make([]string, 0, recentlyViewedMaxItems)
	next = append(next, productID)
	for _, id := range ids {
		if id == productID {
			continue
		}
		next = append(next, id)
		if len(next) >= recentlyViewedMaxItems {
			break
		}
	}
	return next
}

func readRecentlyViewedIDs(r *http.Request) []string {
	c, err := r.Cookie(recentlyViewedCookie)
	if err != nil || c == nil || strings.TrimSpace(c.Value) == "" {
		return nil
	}
	return decodeRecentlyViewedIDs(c.Value)
}

func encodeRecentlyViewedIDs(ids []string) string {
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		parts = append(parts, url.QueryEscape(id))
	}
	return strings.Join(parts, ",")
}

func decodeRecentlyViewedIDs(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := url.QueryUnescape(part)
		if err != nil {
			id = part
		}
		id = strings.TrimSpace(id)
		if id == "" || len(id) > recentlyViewedMaxIDLen {
			continue
		}
		// Defense: cookie is not a place for HTML.
		id = html.UnescapeString(id)
		if id == "" || len(id) > recentlyViewedMaxIDLen {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
		if len(out) >= recentlyViewedMaxItems {
			break
		}
	}
	return out
}
