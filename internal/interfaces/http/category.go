package http

import (
	"net/http"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

// CategoryHandler serves category read endpoints.
type CategoryHandler struct {
	categories catalog.CategoryRepository
	products   catalog.ProductRepository
}

// NewCategoryHandler creates a CategoryHandler with the given dependencies.
func NewCategoryHandler(
	categories catalog.CategoryRepository,
	products catalog.ProductRepository,
) *CategoryHandler {
	if categories == nil {
		panic("CategoryHandler: categories repository must not be nil")
	}
	if products == nil {
		panic("CategoryHandler: products repository must not be nil")
	}
	return &CategoryHandler{categories: categories, products: products}
}

// categoryNode is the JSON shape for a category in the tree response.
type categoryNode struct {
	ID       string                 `json:"id"`
	ParentID *string                `json:"parent_id,omitempty"`
	Name     string                 `json:"name"`
	Slug     string                 `json:"slug"`
	Position int                    `json:"position"`
	Meta     map[string]interface{} `json:"meta"`
	Children []*categoryNode        `json:"children"`
}

// Tree handles GET /api/v1/categories.
func (h *CategoryHandler) Tree() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		all, err := h.categories.FindAll(r.Context())
		if err != nil {
			JSONError(w, err)
			return
		}

		JSON(w, http.StatusOK, map[string]interface{}{
			"categories": buildTree(all),
		})
	}
}

// Get handles GET /api/v1/categories/{id}.
func (h *CategoryHandler) Get() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			JSONError(w, apperror.Validation("category id is required"))
			return
		}

		cat, err := h.categories.FindByID(r.Context(), id)
		if err != nil {
			JSONError(w, err)
			return
		}
		if cat == nil {
			JSONError(w, apperror.NotFound("category not found"))
			return
		}

		JSON(w, http.StatusOK, map[string]interface{}{
			"category": cat,
		})
	}
}

// Products handles GET /api/v1/categories/{id}/products.
func (h *CategoryHandler) Products() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			JSONError(w, apperror.Validation("category id is required"))
			return
		}

		// Verify category exists.
		cat, err := h.categories.FindByID(r.Context(), id)
		if err != nil {
			JSONError(w, err)
			return
		}
		if cat == nil {
			JSONError(w, apperror.NotFound("category not found"))
			return
		}

		offset, limit, err := parsePagination(r)
		if err != nil {
			JSONError(w, err)
			return
		}

		products, err := h.products.FindByCategoryID(r.Context(), id, offset, limit)
		if err != nil {
			JSONError(w, err)
			return
		}

		JSON(w, http.StatusOK, map[string]interface{}{
			"products": products,
		})
	}
}

// buildTree assembles a flat slice of categories into a nested tree.
func buildTree(all []catalog.Category) []*categoryNode {
	nodes := make(map[string]*categoryNode, len(all))
	var roots []string

	// First pass: create nodes.
	for _, c := range all {
		nodes[c.ID] = &categoryNode{
			ID:       c.ID,
			ParentID: c.ParentID,
			Name:     c.Name,
			Slug:     c.Slug,
			Position: c.Position,
			Meta:     c.Meta,
			Children: []*categoryNode{},
		}
		if c.ParentID == nil {
			roots = append(roots, c.ID)
		}
	}

	// Second pass: attach children to parents.
	for _, c := range all {
		if c.ParentID != nil {
			if parent, ok := nodes[*c.ParentID]; ok {
				parent.Children = append(parent.Children, nodes[c.ID])
			}
		}
	}

	// Collect root nodes.
	tree := make([]*categoryNode, 0, len(roots))
	for _, id := range roots {
		tree = append(tree, nodes[id])
	}
	return tree
}
