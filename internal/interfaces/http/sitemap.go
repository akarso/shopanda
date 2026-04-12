package http

import (
	"encoding/xml"
	"net/http"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/cms"
)

const sitemapPageSize = 100

// SitemapHandler serves GET /sitemap.xml.
type SitemapHandler struct {
	baseURL    string
	products   catalog.ProductRepository
	categories catalog.CategoryRepository
	pages      cms.PageRepository
}

// NewSitemapHandler creates a SitemapHandler.
func NewSitemapHandler(
	baseURL string,
	products catalog.ProductRepository,
	categories catalog.CategoryRepository,
	pages cms.PageRepository,
) *SitemapHandler {
	return &SitemapHandler{
		baseURL:    baseURL,
		products:   products,
		categories: categories,
		pages:      pages,
	}
}

type sitemapURLSet struct {
	XMLName xml.Name     `xml:"urlset"`
	XMLNS   string       `xml:"xmlns,attr"`
	URLs    []sitemapURL `xml:"url"`
}

type sitemapURL struct {
	Loc     string `xml:"loc"`
	LastMod string `xml:"lastmod,omitempty"`
}

// Serve handles GET /sitemap.xml.
func (h *SitemapHandler) Serve() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		var urls []sitemapURL

		// Products (paginated, active only).
		offset := 0
		for {
			products, err := h.products.List(ctx, offset, sitemapPageSize)
			if err != nil {
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}
			for _, p := range products {
				if p.Status != catalog.StatusActive {
					continue
				}
				urls = append(urls, sitemapURL{
					Loc:     h.baseURL + "/products/" + p.Slug,
					LastMod: p.UpdatedAt.Format("2006-01-02"),
				})
			}
			if len(products) < sitemapPageSize {
				break
			}
			offset += sitemapPageSize
		}

		// Categories.
		categories, err := h.categories.FindAll(ctx)
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		for _, c := range categories {
			urls = append(urls, sitemapURL{
				Loc:     h.baseURL + "/categories/" + c.Slug,
				LastMod: c.UpdatedAt.Format("2006-01-02"),
			})
		}

		// Pages (paginated, active only).
		offset = 0
		for {
			pages, err := h.pages.List(ctx, offset, sitemapPageSize)
			if err != nil {
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}
			for _, p := range pages {
				if !p.IsActive() {
					continue
				}
				urls = append(urls, sitemapURL{
					Loc:     h.baseURL + "/pages/" + p.Slug(),
					LastMod: p.UpdatedAt().Format("2006-01-02"),
				})
			}
			if len(pages) < sitemapPageSize {
				break
			}
			offset += sitemapPageSize
		}

		urlSet := sitemapURLSet{
			XMLNS: "http://www.sitemaps.org/schemas/sitemap/0.9",
			URLs:  urls,
		}

		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(xml.Header))
		enc := xml.NewEncoder(w)
		enc.Indent("", "  ")
		enc.Encode(urlSet)
	}
}
