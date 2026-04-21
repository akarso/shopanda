package http

import (
	"testing"

	"github.com/akarso/shopanda/internal/domain/catalog"
)

func TestStorefrontCategoryTree_PreservesDescendants(t *testing.T) {
	parentID := "cat-root"
	childID := "cat-child"
	tree := storefrontCategoryTree([]catalog.Category{
		{ID: "cat-root", Name: "Electronics", Slug: "electronics"},
		{ID: childID, ParentID: &parentID, Name: "Headphones", Slug: "headphones"},
		{ID: "cat-grandchild", ParentID: &childID, Name: "Wireless", Slug: "wireless"},
	})

	if len(tree) != 1 {
		t.Fatalf("roots = %d, want 1", len(tree))
	}
	if len(tree[0].Children) != 1 {
		t.Fatalf("root children = %d, want 1", len(tree[0].Children))
	}
	if len(tree[0].Children[0].Children) != 1 {
		t.Fatalf("grandchildren = %d, want 1", len(tree[0].Children[0].Children))
	}
	if tree[0].Children[0].Children[0].Label != "Wireless" {
		t.Fatalf("grandchild label = %q, want %q", tree[0].Children[0].Children[0].Label, "Wireless")
	}
}

func TestStorefrontBreadcrumbs_StopsOnCycle(t *testing.T) {
	parentA := "cat-b"
	parentB := "cat-a"
	all := []catalog.Category{
		{ID: "cat-a", ParentID: &parentA, Name: "Audio", Slug: "audio"},
		{ID: "cat-b", ParentID: &parentB, Name: "Headphones", Slug: "headphones"},
	}

	trail := storefrontBreadcrumbs(all, &all[0])

	if len(trail) != 3 {
		t.Fatalf("breadcrumb count = %d, want 3", len(trail))
	}
	if trail[0].Label != "Home" {
		t.Fatalf("first breadcrumb = %q, want %q", trail[0].Label, "Home")
	}
	if trail[len(trail)-1].Label != "Audio" {
		t.Fatalf("last breadcrumb = %q, want %q", trail[len(trail)-1].Label, "Audio")
	}
	if !trail[len(trail)-1].Current {
		t.Fatal("last breadcrumb should be current")
	}
}
