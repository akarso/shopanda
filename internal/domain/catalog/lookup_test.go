package catalog_test

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/akarso/shopanda/internal/domain/catalog"
)

type memVariants struct {
	byID      map[string]*catalog.Variant
	findByIDs int
	findByID  int
	batchErr  error
	batchOnly bool
}

func (m *memVariants) FindByID(_ context.Context, id string) (*catalog.Variant, error) {
	m.findByID++
	if m.batchOnly {
		return nil, errors.New("FindByID should not be called")
	}
	return m.byID[id], nil
}
func (m *memVariants) FindBySKU(context.Context, string) (*catalog.Variant, error) {
	return nil, nil
}
func (m *memVariants) FindBySKUs(context.Context, []string) (map[string]*catalog.Variant, error) {
	return nil, nil
}
func (m *memVariants) ListByProductID(context.Context, string, int, int) ([]catalog.Variant, error) {
	return nil, nil
}
func (m *memVariants) ListByProductIDs(context.Context, []string, int) (map[string][]catalog.Variant, error) {
	return nil, nil
}
func (m *memVariants) Create(context.Context, *catalog.Variant) error { return nil }
func (m *memVariants) Update(context.Context, *catalog.Variant) error { return nil }

func (m *memVariants) FindByIDs(_ context.Context, ids []string) (map[string]*catalog.Variant, error) {
	m.findByIDs++
	if m.batchErr != nil {
		return nil, m.batchErr
	}
	out := make(map[string]*catalog.Variant, len(ids))
	for _, id := range ids {
		if v := m.byID[id]; v != nil {
			out[v.ID] = v
		}
	}
	return out, nil
}

func TestLookupVariants_BatchOnce(t *testing.T) {
	repo := &memVariants{byID: map[string]*catalog.Variant{
		"v1": {ID: "v1", ProductID: "p1"},
		"v2": {ID: "v2", ProductID: "p1"},
	}, batchOnly: true}
	got, err := catalog.LookupVariants(context.Background(), repo, []string{"v1", "v2", "v1", " "})
	if err != nil {
		t.Fatal(err)
	}
	if repo.findByIDs != 1 {
		t.Fatalf("FindByIDs calls = %d, want 1", repo.findByIDs)
	}
	if got["v1"] == nil || got["v2"] == nil {
		t.Fatalf("got %+v", got)
	}
}

func TestLookupVariants_BatchErrorFallsBackPerID(t *testing.T) {
	repo := &memVariants{
		byID:     map[string]*catalog.Variant{"v1": {ID: "v1", ProductID: "p1"}},
		batchErr: errors.New("22P02"),
	}
	got, err := catalog.LookupVariants(context.Background(), repo, []string{"v1", "bad"})
	if err != nil {
		t.Fatal(err)
	}
	if repo.findByIDs != 1 {
		t.Fatalf("FindByIDs calls = %d, want 1", repo.findByIDs)
	}
	if repo.findByID < 1 {
		t.Fatal("expected per-id fallback")
	}
	if got["v1"] == nil {
		t.Fatal("expected v1")
	}
}

func TestNormalizeLookupIDs_Cap(t *testing.T) {
	ids := make([]string, catalog.MaxLookupIDs+5)
	for i := range ids {
		ids[i] = "id-" + strconv.Itoa(i)
	}
	got := catalog.NormalizeLookupIDs(ids)
	if len(got) != catalog.MaxLookupIDs {
		t.Fatalf("len = %d, want %d", len(got), catalog.MaxLookupIDs)
	}
}
