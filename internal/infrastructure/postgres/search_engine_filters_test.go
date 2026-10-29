package postgres

import (
	"fmt"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/domain/search"
)

func TestAppendSharedProductFilters_CategoryAttrVisibilityType(t *testing.T) {
	var joins, wheres []string
	args := []interface{}{}
	argN := 0
	nextArg := func(v interface{}) string {
		argN++
		args = append(args, v)
		return fmt.Sprintf("$%d", argN)
	}

	appendSharedProductFilters(map[string]interface{}{
		"category":                    "cat-1",
		"attr_color":                  "red",
		search.FilterVisibleInSearch:  true,
		search.FilterVisibleInCatalog: true,
		search.FilterPurchasable:      true,
		search.FilterType:             "simple",
		"attr_":                       "ignored",
		"attr_BAD":                    "ignored",
		"price":                       map[string]interface{}{"gte": 100}, // Search-only; ignored here
	}, nextArg, &joins, &wheres)

	if len(joins) != 1 || !strings.Contains(joins[0], "product_categories") {
		t.Fatalf("joins = %#v, want one product_categories join", joins)
	}
	joined := strings.Join(wheres, " AND ")
	for _, want := range []string{
		"p.attributes->>",
		"p.search_visible_in_search = true",
		"p.search_visible_in_catalog = true",
		"p.search_purchasable = true",
		"p.type = $",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("wheres = %q, want substring %q", joined, want)
		}
	}
	if len(args) != 4 {
		t.Fatalf("args = %#v, want 4 entries", args)
	}
	if args[0] != "cat-1" {
		t.Fatalf("args[0] = %v, want cat-1", args[0])
	}
	foundColor, foundRed, foundType := false, false, false
	for _, a := range args[1:] {
		switch a {
		case "color":
			foundColor = true
		case "red":
			foundRed = true
		case "simple":
			foundType = true
		}
	}
	if !foundColor || !foundRed || !foundType {
		t.Fatalf("args = %#v, want color/red/simple among remaining", args)
	}
}

func TestAppendSharedProductFilters_NilAndEmpty(t *testing.T) {
	var joins, wheres []string
	nextArg := func(v interface{}) string { t.Fatalf("nextArg called with %v", v); return "" }

	appendSharedProductFilters(nil, nextArg, &joins, &wheres)
	appendSharedProductFilters(map[string]interface{}{
		"category":    "",
		"attr_color":  "  ",
		"attr_color2": 123,
	}, nextArg, &joins, &wheres)
	if len(joins) != 0 || len(wheres) != 0 {
		t.Fatalf("joins=%v wheres=%v, want empty", joins, wheres)
	}
}
