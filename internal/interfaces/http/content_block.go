package http

import (
	"net/http"

	cmsApp "github.com/akarso/shopanda/internal/application/cms"
	"github.com/akarso/shopanda/internal/domain/cms"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

// ContentBlockHandler serves public content block read endpoints.
type ContentBlockHandler struct {
	blocks   cms.ContentBlockRepository
	pages    cms.PageRepository
	resolver *cmsApp.BlockResolver
}

// NewContentBlockHandler creates a ContentBlockHandler.
func NewContentBlockHandler(blocks cms.ContentBlockRepository, pages cms.PageRepository, resolver *cmsApp.BlockResolver) *ContentBlockHandler {
	if blocks == nil {
		panic("ContentBlockHandler: blocks repository must not be nil")
	}
	if pages == nil {
		panic("ContentBlockHandler: pages repository must not be nil")
	}
	if resolver == nil {
		panic("ContentBlockHandler: resolver must not be nil")
	}
	return &ContentBlockHandler{blocks: blocks, pages: pages, resolver: resolver}
}

type publicContentBlockResponse struct {
	ID    string                 `json:"id"`
	Type  string                 `json:"type"`
	Title string                 `json:"title"`
	Data  map[string]interface{} `json:"data"`
}

type publicContentBlockTargetResponse struct {
	TargetType string                       `json:"target_type"`
	TargetKey  string                       `json:"target_key"`
	Blocks     []publicContentBlockResponse `json:"blocks"`
}

func toPublicContentBlocks(blocks []cmsApp.ResolvedContentBlock) []publicContentBlockResponse {
	out := make([]publicContentBlockResponse, 0, len(blocks))
	for _, block := range blocks {
		out = append(out, publicContentBlockResponse{
			ID:    block.ID,
			Type:  block.Type,
			Title: block.Title,
			Data:  block.Data,
		})
	}
	return out
}

// GetByTarget handles GET /api/v1/content-blocks/{targetType}/{targetKey}.
func (h *ContentBlockHandler) GetByTarget() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		targetType := cms.TargetType(r.PathValue("targetType"))
		targetKey := r.PathValue("targetKey")
		if targetKey == "" {
			JSONError(w, apperror.Validation("target key is required"))
			return
		}
		if !cms.ValidTargetType(targetType) {
			JSONError(w, apperror.Validation("invalid target type"))
			return
		}
		if targetType == cms.TargetTypeLayout && !cms.ValidLayoutTarget(targetKey) {
			JSONError(w, apperror.Validation("invalid layout target"))
			return
		}

		resolvedTargetKey := targetKey
		if targetType == cms.TargetTypePage {
			page, err := h.pages.FindActiveBySlug(r.Context(), targetKey)
			if err != nil {
				JSONError(w, err)
				return
			}
			if page == nil {
				JSONError(w, apperror.NotFound("page not found"))
				return
			}
			resolvedTargetKey = page.ID()
		}

		blocks, err := h.blocks.FindBlocksByTarget(r.Context(), targetType, resolvedTargetKey)
		if err != nil {
			JSONError(w, err)
			return
		}
		resolved, err := h.resolver.ResolveBlocks(r.Context(), blocks)
		if err != nil {
			JSONError(w, err)
			return
		}
		JSON(w, http.StatusOK, publicContentBlockTargetResponse{
			TargetType: string(targetType),
			TargetKey:  targetKey,
			Blocks:     toPublicContentBlocks(resolved),
		})
	}
}
