package plugin_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/platform/plugin"
)

func TestSortEntriesByDependsOn(t *testing.T) {
	entries := []plugin.Entry{
		{Name: "b/dep"},
		{Name: "a/base"},
	}
	sorted, err := plugin.SortEntriesByDependsOn(entries, map[string][]string{
		"b/dep": {"a/base"},
	})
	if err != nil {
		t.Fatalf("SortEntriesByDependsOn: %v", err)
	}
	if sorted[0].Name != "a/base" || sorted[1].Name != "b/dep" {
		t.Fatalf("order = %v, %v", sorted[0].Name, sorted[1].Name)
	}
}

func TestSortEntriesByDependsOn_Cycle(t *testing.T) {
	entries := []plugin.Entry{{Name: "a"}, {Name: "b"}}
	_, err := plugin.SortEntriesByDependsOn(entries, map[string][]string{
		"a": {"b"},
		"b": {"a"},
	})
	if err == nil {
		t.Fatal("expected cycle error")
	}
}
