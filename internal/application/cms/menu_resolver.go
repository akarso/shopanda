package cms

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/akarso/shopanda/internal/domain/catalog"
	domaincms "github.com/akarso/shopanda/internal/domain/cms"
)

// ResolvedMenuItem is a menu entry with a resolved storefront URL.
type ResolvedMenuItem struct {
	Label    string
	URL      string
	Children []ResolvedMenuItem
}

// MenuResolver resolves menu item targets to URLs.
type MenuResolver struct {
	categories catalog.CategoryRepository
	pages      domaincms.PageRepository
}

// NewMenuResolver creates a MenuResolver.
func NewMenuResolver(categories catalog.CategoryRepository, pages domaincms.PageRepository) *MenuResolver {
	return &MenuResolver{categories: categories, pages: pages}
}

// ResolveTree builds a nested menu from flat active items.
func (r *MenuResolver) ResolveTree(ctx context.Context, items []*domaincms.MenuItem) ([]ResolvedMenuItem, error) {
	active := make([]*domaincms.MenuItem, 0, len(items))
	for _, item := range items {
		if item != nil && item.IsActive() {
			active = append(active, item)
		}
	}
	if len(active) == 0 {
		return nil, nil
	}
	byID := make(map[string]*domaincms.MenuItem, len(active))
	children := make(map[string][]*domaincms.MenuItem)
	var roots []*domaincms.MenuItem
	for _, item := range active {
		byID[item.ID()] = item
	}
	for _, item := range active {
		parentID := item.ParentID()
		if parentID == "" {
			roots = append(roots, item)
			continue
		}
		if _, ok := byID[parentID]; !ok {
			continue
		}
		children[parentID] = append(children[parentID], item)
	}
	sortMenuItems(roots)
	for id := range children {
		sortMenuItems(children[id])
	}
	out := make([]ResolvedMenuItem, 0, len(roots))
	for _, root := range roots {
		resolved, err := r.resolveItem(ctx, root, children)
		if err != nil {
			return nil, err
		}
		if resolved != nil {
			out = append(out, *resolved)
		}
	}
	return out, nil
}

func (r *MenuResolver) resolveItem(ctx context.Context, item *domaincms.MenuItem, children map[string][]*domaincms.MenuItem) (*ResolvedMenuItem, error) {
	url, ok, err := r.resolveURL(ctx, item)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	resolved := ResolvedMenuItem{Label: item.Label(), URL: url}
	for _, child := range children[item.ID()] {
		childResolved, err := r.resolveItem(ctx, child, children)
		if err != nil {
			return nil, err
		}
		if childResolved != nil {
			resolved.Children = append(resolved.Children, *childResolved)
		}
	}
	return &resolved, nil
}

func (r *MenuResolver) resolveURL(ctx context.Context, item *domaincms.MenuItem) (string, bool, error) {
	switch item.LinkType() {
	case domaincms.LinkTypeURL:
		return item.LinkTarget(), true, nil
	case domaincms.LinkTypeCategory:
		if r.categories == nil {
			return "", false, nil
		}
		category, err := r.categories.FindByID(ctx, item.LinkTarget())
		if err != nil {
			return "", false, fmt.Errorf("menu resolver: category %q: %w", item.LinkTarget(), err)
		}
		if category == nil {
			return "", false, nil
		}
		slug := strings.TrimSpace(category.Slug)
		if slug == "" {
			return "", false, nil
		}
		return "/categories/" + slug, true, nil
	case domaincms.LinkTypePage:
		if r.pages == nil {
			return "", false, nil
		}
		page, err := r.pages.FindByID(ctx, item.LinkTarget())
		if err != nil {
			return "", false, fmt.Errorf("menu resolver: page %q: %w", item.LinkTarget(), err)
		}
		if page == nil || !page.IsActive() {
			return "", false, nil
		}
		slug := strings.TrimSpace(page.Slug())
		if slug == "" {
			return "", false, nil
		}
		return "/pages/" + slug, true, nil
	default:
		return "", false, nil
	}
}

func sortMenuItems(items []*domaincms.MenuItem) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Position() != items[j].Position() {
			return items[i].Position() < items[j].Position()
		}
		return items[i].Label() < items[j].Label()
	})
}
