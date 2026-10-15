package catalog_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/domain/catalog"
)

type mapFacts map[string]catalog.VisibilitySnapshot

func (m mapFacts) LoadMany(_ context.Context, ids []string) (map[string]catalog.VisibilitySnapshot, error) {
	out := make(map[string]catalog.VisibilitySnapshot, len(ids))
	for _, id := range ids {
		if s, ok := m[id]; ok {
			out[id] = s
		}
	}
	return out, nil
}

func sellableSnap(status catalog.Status, modes catalog.VisibilityAxes) catalog.VisibilitySnapshot {
	return catalog.VisibilitySnapshot{
		Status: status,
		Modes:  modes,
		Inputs: catalog.VisibilityInputs{
			QuantityPositive:   true,
			HasPrice:           true,
			HasStoreAssignment: true,
		},
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func listSlice(all []catalog.Product) func(context.Context, int, int) ([]catalog.Product, error) {
	return func(_ context.Context, dbOffset, dbLimit int) ([]catalog.Product, error) {
		if dbOffset >= len(all) {
			return nil, nil
		}
		end := dbOffset + dbLimit
		if end > len(all) {
			end = len(all)
		}
		return all[dbOffset:end], nil
	}
}

func afterParams(cursor string) catalog.ListVisiblePageParams {
	o, s, err := catalog.DecodeListCursor(cursor)
	if err != nil {
		panic(err)
	}
	return catalog.ListVisiblePageParams{AfterSet: true, AfterOffset: o, AfterSkip: s}
}

func TestEncodeDecodeListCursor(t *testing.T) {
	s := catalog.EncodeListCursor(5020, 2500)
	o, skip, err := catalog.DecodeListCursor(s)
	if err != nil || o != 5020 || skip != 2500 {
		t.Fatalf("got o=%d skip=%d err=%v", o, skip, err)
	}
	if catalog.NextAfterCursor(catalog.MaxListCursorOffset+1, 0) != "" {
		t.Fatal("NextAfterCursor must not emit past cap")
	}
	if catalog.NextAfterCursor(0, catalog.MaxListCursorOffset+1) != "" {
		t.Fatal("NextAfterCursor must not emit skip past cap")
	}
	over := catalog.EncodeListCursor(catalog.MaxListCursorOffset+1, 0)
	_, _, err = catalog.DecodeListCursor(over)
	if err != catalog.ErrListCursorTooDeep {
		t.Fatalf("want ErrListCursorTooDeep, got %v", err)
	}
	overSkip := catalog.EncodeListCursor(0, catalog.MaxListCursorOffset+1)
	_, _, err = catalog.DecodeListCursor(overSkip)
	if err != catalog.ErrListCursorTooDeep {
		t.Fatalf("want ErrListCursorTooDeep, got %v", err)
	}
	_, _, err = catalog.DecodeListCursor("42")
	if err != catalog.ErrInvalidListCursor {
		t.Fatalf("bare integer: want ErrInvalidListCursor, got %v", err)
	}
}

func TestFilterIDsWithFacts(t *testing.T) {
	modes := catalog.DefaultVisibilityAxes()
	hidden := modes
	hidden.Catalog = catalog.VisibilityModeHidden
	facts := mapFacts{
		"a": sellableSnap(catalog.StatusActive, modes),
		"b": sellableSnap(catalog.StatusActive, hidden),
	}
	got, err := catalog.FilterIDsWithFacts(context.Background(), []string{"a", "b", "c"}, catalog.VisibilitySurfaceCatalog, facts, catalog.VisibilityOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "a" {
		t.Fatalf("got %v, want [a]", got)
	}
}

func TestFilterIDsWithFacts_EmptyInput(t *testing.T) {
	got, err := catalog.FilterIDsWithFacts(context.Background(), nil, catalog.VisibilitySurfaceCatalog, mapFacts{}, catalog.VisibilityOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("got %#v, want non-nil empty slice", got)
	}
}

func TestFilterIDsWithFacts_NilFactsPassesThrough(t *testing.T) {
	got, err := catalog.FilterIDsWithFacts(context.Background(), []string{"a", "b"}, catalog.VisibilitySurfaceCatalog, nil, catalog.VisibilityOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("got %v", got)
	}
}

func TestFilterWithFacts_PreservesOrder(t *testing.T) {
	modes := catalog.DefaultVisibilityAxes()
	facts := mapFacts{
		"a": sellableSnap(catalog.StatusActive, modes),
		"b": sellableSnap(catalog.StatusActive, modes),
	}
	products := []catalog.Product{
		{ID: "a", Status: catalog.StatusActive, VisibilityModes: modes},
		{ID: "b", Status: catalog.StatusActive, VisibilityModes: modes},
	}
	got, err := catalog.FilterWithFacts(context.Background(), products, catalog.VisibilitySurfaceCatalog, facts, catalog.VisibilityOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "b" {
		t.Fatalf("got %+v", got)
	}
}

func TestFilterWithFacts_MissingSnapshotDenied(t *testing.T) {
	modes := catalog.DefaultVisibilityAxes()
	forced := modes
	forced.Catalog = catalog.VisibilityModeVisible
	products := []catalog.Product{
		{ID: "gone", Status: catalog.StatusActive, VisibilityModes: forced},
	}
	got, err := catalog.FilterWithFacts(context.Background(), products, catalog.VisibilitySurfaceCatalog, mapFacts{}, catalog.VisibilityOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("missing snapshot must deny, got %+v", got)
	}
}

func TestAllowWithFacts_MissingSnapshotDenied(t *testing.T) {
	modes := catalog.DefaultVisibilityAxes()
	modes.Individually = catalog.VisibilityModeVisible
	p := &catalog.Product{ID: "gone", Status: catalog.StatusActive, VisibilityModes: modes}
	ok, err := catalog.AllowWithFacts(context.Background(), p, catalog.VisibilitySurfaceIndividually, mapFacts{}, catalog.VisibilityOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("missing snapshot must deny")
	}
}

func TestAllowWithFacts_NilFactsPasses(t *testing.T) {
	p := &catalog.Product{ID: "a", Status: catalog.StatusActive}
	ok, err := catalog.AllowWithFacts(context.Background(), p, catalog.VisibilitySurfaceIndividually, nil, catalog.VisibilityOptions{})
	if err != nil || !ok {
		t.Fatalf("nil facts must pass (gate off), ok=%v err=%v", ok, err)
	}
}

func TestListVisiblePage_SkipsHidden(t *testing.T) {
	modes := catalog.DefaultVisibilityAxes()
	hidden := modes
	hidden.Catalog = catalog.VisibilityModeHidden
	all := []catalog.Product{
		{ID: "h1", Status: catalog.StatusActive, VisibilityModes: hidden},
		{ID: "v1", Status: catalog.StatusActive, VisibilityModes: modes},
		{ID: "v2", Status: catalog.StatusActive, VisibilityModes: modes},
	}
	facts := mapFacts{
		"h1": sellableSnap(catalog.StatusActive, hidden),
		"v1": sellableSnap(catalog.StatusActive, modes),
		"v2": sellableSnap(catalog.StatusActive, modes),
	}
	page, err := catalog.ListVisiblePage(context.Background(), listSlice(all), catalog.VisibilitySurfaceCatalog, facts, catalog.VisibilityOptions{}, catalog.ListVisiblePageParams{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if page.Truncated || page.HasMore {
		t.Fatalf("truncated=%v has_more=%v", page.Truncated, page.HasMore)
	}
	if len(page.Products) != 2 || page.Products[0].ID != "v1" || page.Products[1].ID != "v2" {
		t.Fatalf("got %+v", page.Products)
	}
}

func TestListVisiblePage_DeepOffset(t *testing.T) {
	modes := catalog.DefaultVisibilityAxes()
	all := make([]catalog.Product, 0, 600)
	facts := mapFacts{}
	for i := 0; i < 600; i++ {
		id := "p" + itoa(i)
		all = append(all, catalog.Product{ID: id, Status: catalog.StatusActive, VisibilityModes: modes})
		facts[id] = sellableSnap(catalog.StatusActive, modes)
	}
	page, err := catalog.ListVisiblePage(context.Background(), listSlice(all), catalog.VisibilitySurfaceCatalog, facts, catalog.VisibilityOptions{}, catalog.ListVisiblePageParams{
		VisibleSkip: 500,
		Limit:       20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Products) != 20 {
		t.Fatalf("len=%d want 20", len(page.Products))
	}
	if page.Products[0].ID != "p500" || page.Products[19].ID != "p519" {
		t.Fatalf("got first=%s last=%s", page.Products[0].ID, page.Products[19].ID)
	}
	if !page.HasMore || page.NextAfter == "" {
		t.Fatalf("has_more=%v next=%q", page.HasMore, page.NextAfter)
	}
}

func TestListVisiblePage_OffsetBudgetPreservesRemainingSkip(t *testing.T) {
	modes := catalog.DefaultVisibilityAxes()
	hidden := modes
	hidden.Catalog = catalog.VisibilityModeHidden
	// Dense enough that MaxScan=30 only skips ~15 visible products of a 3000 skip request.
	all := make([]catalog.Product, 0, 80)
	facts := mapFacts{}
	for i := 0; i < 80; i++ {
		id := "p" + itoa(i)
		all = append(all, catalog.Product{ID: id, Status: catalog.StatusActive, VisibilityModes: modes})
		facts[id] = sellableSnap(catalog.StatusActive, modes)
	}
	page1, err := catalog.ListVisiblePage(context.Background(), listSlice(all), catalog.VisibilitySurfaceCatalog, facts, catalog.VisibilityOptions{}, catalog.ListVisiblePageParams{
		VisibleSkip: 50,
		Limit:       10,
		MaxScan:     30, // skips only ~30 visible before budget
	})
	if err != nil {
		t.Fatal(err)
	}
	if !page1.Truncated || !page1.HasMore || page1.NextAfter == "" {
		t.Fatalf("page1 truncated=%v has_more=%v next=%q products=%d", page1.Truncated, page1.HasMore, page1.NextAfter, len(page1.Products))
	}
	_, rem, err := catalog.DecodeListCursor(page1.NextAfter)
	if err != nil {
		t.Fatal(err)
	}
	if rem <= 0 {
		t.Fatalf("remaining skip must be preserved in cursor, got %d", rem)
	}

	p2 := afterParams(page1.NextAfter)
	p2.Limit = 10
	p2.MaxScan = 100
	page2, err := catalog.ListVisiblePage(context.Background(), listSlice(all), catalog.VisibilitySurfaceCatalog, facts, catalog.VisibilityOptions{}, p2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page2.Products) == 0 {
		t.Fatal("resume with remaining skip must eventually return products")
	}
	// First product should be around index 50 (original VisibleSkip), not ~30.
	if page2.Products[0].ID != "p50" {
		t.Fatalf("first=%s want p50 (skip preserved across budget stop)", page2.Products[0].ID)
	}
}

func TestListVisiblePage_ResumeCursorMakesProgress(t *testing.T) {
	modes := catalog.DefaultVisibilityAxes()
	hidden := modes
	hidden.Catalog = catalog.VisibilityModeHidden
	all := make([]catalog.Product, 0, 40)
	facts := mapFacts{}
	for i := 0; i < 30; i++ {
		id := "h" + itoa(i)
		all = append(all, catalog.Product{ID: id, Status: catalog.StatusActive, VisibilityModes: hidden})
		facts[id] = sellableSnap(catalog.StatusActive, hidden)
	}
	for i := 0; i < 5; i++ {
		id := "v" + itoa(i)
		all = append(all, catalog.Product{ID: id, Status: catalog.StatusActive, VisibilityModes: modes})
		facts[id] = sellableSnap(catalog.StatusActive, modes)
	}

	page1, err := catalog.ListVisiblePage(context.Background(), listSlice(all), catalog.VisibilitySurfaceCatalog, facts, catalog.VisibilityOptions{}, catalog.ListVisiblePageParams{
		Limit:   10,
		MaxScan: 25,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !page1.Truncated || !page1.HasMore {
		t.Fatalf("page1 truncated=%v has_more=%v products=%d", page1.Truncated, page1.HasMore, len(page1.Products))
	}

	p2 := afterParams(page1.NextAfter)
	p2.Limit = 10
	p2.MaxScan = 25
	page2, err := catalog.ListVisiblePage(context.Background(), listSlice(all), catalog.VisibilitySurfaceCatalog, facts, catalog.VisibilityOptions{}, p2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page2.Products) == 0 {
		t.Fatal("resume cursor must reach visible products")
	}
	if page2.Products[0].ID != "v0" {
		t.Fatalf("page2 first=%s want v0", page2.Products[0].ID)
	}
}

func TestListVisiblePage_ScanBound(t *testing.T) {
	modes := catalog.DefaultVisibilityAxes()
	hidden := modes
	hidden.Catalog = catalog.VisibilityModeHidden
	all := make([]catalog.Product, 0, 5000)
	facts := mapFacts{}
	for i := 0; i < 5000; i++ {
		id := "h" + itoa(i)
		all = append(all, catalog.Product{ID: id, Status: catalog.StatusActive, VisibilityModes: hidden})
		facts[id] = sellableSnap(catalog.StatusActive, hidden)
	}
	const maxScan = 400
	rowsRead := 0
	listFn := func(ctx context.Context, dbOffset, dbLimit int) ([]catalog.Product, error) {
		rowsRead += dbLimit
		return listSlice(all)(ctx, dbOffset, dbLimit)
	}
	page, err := catalog.ListVisiblePage(context.Background(), listFn, catalog.VisibilitySurfaceCatalog, facts, catalog.VisibilityOptions{}, catalog.ListVisiblePageParams{
		Limit:   20,
		MaxScan: maxScan,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !page.Truncated || !page.HasMore {
		t.Fatalf("truncated=%v has_more=%v", page.Truncated, page.HasMore)
	}
	// Budget batches + one has-more probe of 1 row.
	if rowsRead > maxScan+1 {
		t.Fatalf("rows requested=%d exceeds MaxScan+1=%d", rowsRead, maxScan+1)
	}
}

func TestListVisiblePage_NilFactsHonoursAfter(t *testing.T) {
	modes := catalog.DefaultVisibilityAxes()
	all := make([]catalog.Product, 0, 30)
	for i := 0; i < 30; i++ {
		all = append(all, catalog.Product{ID: "p" + itoa(i), Status: catalog.StatusActive, VisibilityModes: modes})
	}
	p := afterParams(catalog.EncodeListCursor(10, 0))
	p.Limit = 5
	page, err := catalog.ListVisiblePage(context.Background(), listSlice(all), catalog.VisibilitySurfaceCatalog, nil, catalog.VisibilityOptions{}, p)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Products) != 5 || page.Products[0].ID != "p10" {
		t.Fatalf("got %+v", page.Products)
	}
}

func deepListFn(modes catalog.VisibilityAxes) func(context.Context, int, int) ([]catalog.Product, error) {
	return func(_ context.Context, dbOffset, dbLimit int) ([]catalog.Product, error) {
		out := make([]catalog.Product, 0, dbLimit)
		for i := 0; i < dbLimit; i++ {
			id := "p" + itoa(dbOffset+i)
			out = append(out, catalog.Product{ID: id, Status: catalog.StatusActive, VisibilityModes: modes})
		}
		return out, nil
	}
}

func TestListVisiblePage_DepthCapStopsCursor(t *testing.T) {
	modes := catalog.DefaultVisibilityAxes()
	listFn := deepListFn(modes)
	factsSrc := factsSourceFunc(func(_ context.Context, ids []string) (map[string]catalog.VisibilitySnapshot, error) {
		out := make(map[string]catalog.VisibilitySnapshot, len(ids))
		for _, id := range ids {
			out[id] = sellableSnap(catalog.StatusActive, modes)
		}
		return out, nil
	})

	start := catalog.MaxListCursorOffset - 5
	page, err := catalog.ListVisiblePage(context.Background(), listFn, catalog.VisibilitySurfaceCatalog, factsSrc, catalog.VisibilityOptions{}, catalog.ListVisiblePageParams{
		AfterSet:    true,
		AfterOffset: start,
		Limit:       10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Products) != 10 {
		t.Fatalf("len=%d want 10", len(page.Products))
	}
	if page.HasMore || page.NextAfter != "" || !page.DepthLimited {
		t.Fatalf("depth cap: has_more=%v next=%q depth_limited=%v", page.HasMore, page.NextAfter, page.DepthLimited)
	}
	if page.Truncated {
		t.Fatal("full page at depth limit should not set truncated")
	}
}

func TestListVisiblePage_DepthCap_EnforceOff(t *testing.T) {
	modes := catalog.DefaultVisibilityAxes()
	listFn := deepListFn(modes)
	start := catalog.MaxListCursorOffset - 5
	page, err := catalog.ListVisiblePage(context.Background(), listFn, catalog.VisibilitySurfaceCatalog, nil, catalog.VisibilityOptions{}, catalog.ListVisiblePageParams{
		AfterSet:    true,
		AfterOffset: start,
		Limit:       10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Products) != 10 {
		t.Fatalf("len=%d want 10", len(page.Products))
	}
	if page.HasMore || page.NextAfter != "" || !page.DepthLimited {
		t.Fatalf("enforce-off depth cap: has_more=%v next=%q depth_limited=%v", page.HasMore, page.NextAfter, page.DepthLimited)
	}
}

type factsSourceFunc func(context.Context, []string) (map[string]catalog.VisibilitySnapshot, error)

func (f factsSourceFunc) LoadMany(ctx context.Context, ids []string) (map[string]catalog.VisibilitySnapshot, error) {
	return f(ctx, ids)
}

func TestListVisiblePage_Bypass(t *testing.T) {
	modes := catalog.DefaultVisibilityAxes()
	hidden := modes
	hidden.Catalog = catalog.VisibilityModeHidden
	all := []catalog.Product{{ID: "h", Status: catalog.StatusActive, VisibilityModes: hidden}}
	facts := mapFacts{"h": sellableSnap(catalog.StatusActive, hidden)}
	ctx := catalog.WithBypassProductVisibility(context.Background())
	page, err := catalog.ListVisiblePage(ctx, listSlice(all), catalog.VisibilitySurfaceCatalog, facts, catalog.VisibilityOptions{}, catalog.ListVisiblePageParams{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Products) != 1 {
		t.Fatalf("bypass must skip axis gate, got %+v", page.Products)
	}
}
