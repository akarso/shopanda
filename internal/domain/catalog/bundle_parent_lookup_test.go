package catalog_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/akarso/shopanda/internal/domain/catalog"
)

type expandLookup struct {
	byComponent map[string][]string
	err         error
	batchCalls  int
	lastBatch   []string
}

func (f *expandLookup) ListBundleIDsByComponentID(_ context.Context, componentProductID string) ([]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	return append([]string(nil), f.byComponent[componentProductID]...), nil
}

func (f *expandLookup) ListBundleIDsByComponentIDs(ctx context.Context, ids []string) (map[string][]string, error) {
	f.batchCalls++
	f.lastBatch = append([]string(nil), ids...)
	if f.err != nil {
		return nil, f.err
	}
	return catalog.CollectBundleIDsByComponentIDs(ctx, f, ids)
}

func TestExpandTouchedWithBundleParents_MergesParents(t *testing.T) {
	touched := map[string]struct{}{"leaf-a": {}, "leaf-b": {}}
	lookup := &expandLookup{byComponent: map[string][]string{
		"leaf-a": {"kit-1"},
		"leaf-b": {"kit-1", "kit-2"},
	}}
	if err := catalog.ExpandTouchedWithBundleParents(context.Background(), lookup, touched); err != nil {
		t.Fatalf("Expand: %v", err)
	}
	if lookup.batchCalls != 1 {
		t.Fatalf("batch calls = %d, want 1", lookup.batchCalls)
	}
	for _, id := range []string{"leaf-a", "leaf-b", "kit-1", "kit-2"} {
		if _, ok := touched[id]; !ok {
			t.Fatalf("missing %s in %v", id, touched)
		}
	}
}

func TestExpandTouchedWithBundleParents_Chunks(t *testing.T) {
	touched := make(map[string]struct{}, catalog.BundleParentLookupChunkSize+1)
	byComponent := make(map[string][]string, catalog.BundleParentLookupChunkSize+1)
	for i := 0; i < catalog.BundleParentLookupChunkSize+1; i++ {
		id := fmt.Sprintf("leaf-%03d", i)
		touched[id] = struct{}{}
		byComponent[id] = []string{"kit"}
	}
	lookup := &expandLookup{byComponent: byComponent}
	if err := catalog.ExpandTouchedWithBundleParents(context.Background(), lookup, touched); err != nil {
		t.Fatalf("Expand: %v", err)
	}
	if lookup.batchCalls != 2 {
		t.Fatalf("batch calls = %d, want 2", lookup.batchCalls)
	}
	if _, ok := touched["kit"]; !ok {
		t.Fatal("expected parent kit")
	}
}

func TestExpandTouchedWithBundleParents_LookupErrorStillMergesOtherChunks(t *testing.T) {
	touched := map[string]struct{}{"leaf": {}}
	lookup := &expandLookup{err: errors.New("db down")}
	err := catalog.ExpandTouchedWithBundleParents(context.Background(), lookup, touched)
	if err == nil || err.Error() != "db down" {
		t.Fatalf("err = %v, want db down", err)
	}
	if _, ok := touched["leaf"]; !ok {
		t.Fatal("leaf must stay in the set after a lookup error")
	}
}

func TestExpandTouchedWithBundleParents_CanceledContext(t *testing.T) {
	touched := map[string]struct{}{"leaf": {}}
	lookup := &expandLookup{byComponent: map[string][]string{"leaf": {"kit"}}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := catalog.ExpandTouchedWithBundleParents(ctx, lookup, touched)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if lookup.batchCalls != 0 {
		t.Fatalf("batch calls = %d, want 0 after cancel", lookup.batchCalls)
	}
	if _, ok := touched["kit"]; ok {
		t.Fatal("canceled expand must not merge parents")
	}
}

func TestExpandTouchedWithBundleParents_NilLookup(t *testing.T) {
	touched := map[string]struct{}{"leaf": {}}
	if err := catalog.ExpandTouchedWithBundleParents(context.Background(), nil, touched); err != nil {
		t.Fatalf("nil lookup: %v", err)
	}
	if len(touched) != 1 {
		t.Fatalf("touched = %v", touched)
	}
}
