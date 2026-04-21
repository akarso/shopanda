package http

import (
	"bytes"
	"errors"
	"net/http"
	"time"

	"github.com/akarso/shopanda/internal/application/composition"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/store"
	"github.com/akarso/shopanda/internal/domain/theme"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

// StorefrontHandler renders SSR pages using the theme engine.
type StorefrontHandler struct {
	engine *theme.Engine
	repo   catalog.ProductRepository
	pdp    *composition.Pipeline[composition.ProductContext]
}

type StorefrontNavLink struct {
	Label string
	URL   string
}

type StorefrontLayoutData struct {
	SiteName     string
	SearchAction string
	CartURL      string
	CartLabel    string
	CurrentYear  int
	Nav          []StorefrontNavLink
}

type StorefrontHomePageData struct {
	Layout StorefrontLayoutData
	Theme  theme.Theme
}

type StorefrontProductPageData struct {
	*composition.ProductContext
	Layout StorefrontLayoutData
	Theme  theme.Theme
}

// NewStorefrontHandler creates a StorefrontHandler.
func NewStorefrontHandler(
	engine *theme.Engine,
	repo catalog.ProductRepository,
	pdp *composition.Pipeline[composition.ProductContext],
) *StorefrontHandler {
	return &StorefrontHandler{engine: engine, repo: repo, pdp: pdp}
}

// Home handles GET / and renders the storefront landing page.
func (h *StorefrontHandler) Home() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !h.engine.HasTemplate("home") {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}

		page := StorefrontHomePageData{
			Layout: h.layoutData(r),
			Theme:  h.engine.Theme(),
		}
		h.renderPage(w, "home", page)
	}
}

// Product handles GET /products/{slug} and renders the product page via SSR.
func (h *StorefrontHandler) Product() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		slug := r.PathValue("slug")
		if slug == "" {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}

		product, err := h.repo.FindBySlug(r.Context(), slug)
		if err != nil {
			var appErr *apperror.Error
			if errors.As(err, &appErr) && appErr.Code == apperror.CodeNotFound {
				http.Error(w, "Not Found", http.StatusNotFound)
				return
			}
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		if product == nil {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}

		ctx := composition.NewProductContext(product)
		ctx.Ctx = r.Context()
		if s := store.FromContext(r.Context()); s != nil {
			ctx.StoreID = s.ID
			if ctx.Currency == "" {
				ctx.Currency = s.Currency
			}
			if ctx.Country == "" {
				ctx.Country = s.Country
			}
		}
		if err := h.pdp.Execute(ctx); err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		page := StorefrontProductPageData{
			ProductContext: ctx,
			Layout:         h.layoutData(r),
			Theme:          h.engine.Theme(),
		}
		h.renderPage(w, "product", page)
	}
}

func (h *StorefrontHandler) renderPage(w http.ResponseWriter, name string, data interface{}) {
	var buf bytes.Buffer
	if err := h.engine.Render(&buf, name, data); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(buf.Bytes())
}

func (h *StorefrontHandler) layoutData(r *http.Request) StorefrontLayoutData {
	themeCfg := h.engine.Theme().Storefront
	siteName := h.engine.Theme().Name
	if s := store.FromContext(r.Context()); s != nil && s.Name != "" {
		siteName = s.Name
	}
	searchAction := themeCfg.SearchAction
	if searchAction == "" {
		searchAction = "/products"
	}
	cartURL := themeCfg.CartURL
	if cartURL == "" {
		cartURL = "/cart"
	}
	cartLabel := themeCfg.CartLabel
	if cartLabel == "" {
		cartLabel = "Cart (0)"
	}
	nav := make([]StorefrontNavLink, 0, len(themeCfg.Nav))
	if len(themeCfg.Nav) > 0 {
		for _, item := range themeCfg.Nav {
			if item.Label == "" || item.URL == "" {
				continue
			}
			nav = append(nav, StorefrontNavLink{Label: item.Label, URL: item.URL})
		}
	}
	if len(nav) == 0 {
		nav = []StorefrontNavLink{
			{Label: "Home", URL: "/"},
			{Label: "Categories", URL: "/categories"},
			{Label: "Account", URL: "/account/login"},
		}
	}
	return StorefrontLayoutData{
		SiteName:     siteName,
		SearchAction: searchAction,
		CartURL:      cartURL,
		CartLabel:    cartLabel,
		CurrentYear:  time.Now().UTC().Year(),
		Nav:          nav,
	}
}
