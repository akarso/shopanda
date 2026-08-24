package storefront

import (
	"context"
	"html/template"
	"net/http"

	cmsApp "github.com/akarso/shopanda/internal/application/cms"
	"github.com/akarso/shopanda/internal/domain/cms"
	"github.com/akarso/shopanda/internal/domain/theme"
)

// StorefrontContentBlock is a rendered CMS block for SSR templates.
type StorefrontContentBlock struct {
	Type          string
	Title         string
	Headline      string
	Subheadline   string
	CTALabel      string
	CTAURL        string
	ImageURL      string
	Body          template.HTML
	CarouselTitle string
	Products      []StorefrontProductCard
}

// StorefrontCMSPageData is SSR data for a CMS page.
type StorefrontCMSPageData struct {
	Layout  StorefrontLayoutData
	Theme   theme.Theme
	Title   string
	Content template.HTML
	Blocks  []StorefrontContentBlock
}

// WithContentBlocks enables CMS block rendering on storefront pages.
func (h *StorefrontHandler) WithContentBlocks(blocks cms.ContentBlockRepository, resolver *cmsApp.BlockResolver, pages cms.PageRepository) *StorefrontHandler {
	h.contentBlocks = blocks
	h.blockResolver = resolver
	h.pages = pages
	return h
}

// CMSPage handles GET /pages/{slug} and renders a CMS page with assigned blocks.
func (h *StorefrontHandler) CMSPage() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		slug := r.PathValue("slug")
		if slug == "" || h.pages == nil {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}
		page, err := h.pages.FindActiveBySlug(r.Context(), slug)
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		if page == nil {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}
		if !h.engine.HasTemplate("page") {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}

		blocks, err := h.loadStorefrontBlocks(r.Context(), cms.TargetTypePage, page.ID())
		if err != nil {
			h.log.Warn("storefront.content_blocks.load_failed", map[string]interface{}{
				"path":  r.URL.Path,
				"error": err.Error(),
			})
		}
		data := StorefrontCMSPageData{
			Layout:  h.layoutDataBestEffort(r),
			Theme:   h.engine.Theme(),
			Title:   page.Title(),
			Content: cms.SanitizeHTML(page.Content()),
			Blocks:  blocks,
		}
		h.renderPage(w, "page", data)
	}
}

func (h *StorefrontHandler) loadStorefrontBlocks(ctx context.Context, targetType cms.TargetType, targetKey string) ([]StorefrontContentBlock, error) {
	if h.contentBlocks == nil || h.blockResolver == nil {
		return nil, nil
	}
	blocks, err := h.contentBlocks.FindActiveBlocksByTarget(ctx, targetType, targetKey)
	if err != nil {
		return nil, err
	}
	resolved, err := h.blockResolver.ResolveBlocks(ctx, blocks)
	if err != nil {
		return nil, err
	}
	return storefrontBlocksFromResolved(resolved), nil
}

func storefrontBlocksFromResolved(blocks []cmsApp.ResolvedContentBlock) []StorefrontContentBlock {
	out := make([]StorefrontContentBlock, 0, len(blocks))
	for _, block := range blocks {
		item := StorefrontContentBlock{
			Type:  block.Type,
			Title: block.Title,
		}
		switch block.Type {
		case string(cms.BlockTypeHero):
			item.Headline = stringValue(block.Data, "headline")
			item.Subheadline = stringValue(block.Data, "subheadline")
			item.CTALabel = stringValue(block.Data, "cta_label")
			item.CTAURL = stringValue(block.Data, "cta_url")
			item.ImageURL = stringValue(block.Data, "image_url")
		case string(cms.BlockTypeRichText):
			item.Body = cms.SanitizeHTML(stringValue(block.Data, "body"))
		case string(cms.BlockTypeProductCarousel):
			item.CarouselTitle = stringValue(block.Data, "title")
			item.Products = storefrontCarouselProducts(block.Data["products"])
		}
		out = append(out, item)
	}
	return out
}

func storefrontCarouselProducts(raw interface{}) []StorefrontProductCard {
	items, ok := raw.([]map[string]interface{})
	if !ok {
		return nil
	}
	out := make([]StorefrontProductCard, 0, len(items))
	for _, item := range items {
		out = append(out, StorefrontProductCard{
			Name:        stringValue(item, "name"),
			Slug:        stringValue(item, "slug"),
			Description: stringValue(item, "description"),
			ImageURL:    stringValue(item, "image_url"),
		})
	}
	return out
}

func stringValue(data map[string]interface{}, key string) string {
	if data == nil {
		return ""
	}
	value, ok := data[key]
	if !ok || value == nil {
		return ""
	}
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}
