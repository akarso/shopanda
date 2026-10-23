package catalog_test

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"

	"github.com/akarso/shopanda/internal/domain/catalog"
)

func TestVisibilityMode_IsValid(t *testing.T) {
	tests := []struct {
		mode catalog.VisibilityMode
		want bool
	}{
		{catalog.VisibilityModeAuto, true},
		{catalog.VisibilityModeVisible, true},
		{catalog.VisibilityModeHidden, true},
		{catalog.VisibilityMode(""), false},
		{catalog.VisibilityMode("forced"), false},
	}
	for _, tc := range tests {
		name := string(tc.mode)
		if name == "" {
			name = "(empty)"
		}
		t.Run(name, func(t *testing.T) {
			if got := tc.mode.IsValid(); got != tc.want {
				t.Errorf("IsValid() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAllVisibilityModes_MatchesMigrationContract(t *testing.T) {
	got := catalog.AllVisibilityModes()
	want := []catalog.VisibilityMode{
		catalog.VisibilityModeAuto,
		catalog.VisibilityModeVisible,
		catalog.VisibilityModeHidden,
	}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestAllVisibilityModesMatchMigrationCheck(t *testing.T) {
	t.Parallel()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	migrationPath := filepath.Join(filepath.Dir(thisFile), "../../../migrations/086_add_products_visibility_modes.sql")
	content, err := os.ReadFile(migrationPath)
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}

	columns := []string{
		"visibility_catalog_mode",
		"visibility_search_mode",
		"visibility_individually_mode",
		"visibility_purchasable_mode",
	}
	re := regexp.MustCompile(`(visibility_\w+_mode) IN \(([^)]+)\)`)
	matches := re.FindAllSubmatch(content, -1)
	if len(matches) != len(columns) {
		t.Fatalf("found %d IN (...) clauses, want %d (one per column)", len(matches), len(columns))
	}

	fromDomain := make(map[string]struct{}, len(catalog.AllVisibilityModes()))
	for _, mode := range catalog.AllVisibilityModes() {
		fromDomain[string(mode)] = struct{}{}
	}

	var firstSet map[string]struct{}
	for i, m := range matches {
		col := string(m[1])
		if col != columns[i] {
			t.Errorf("clause %d column = %q, want %q", i, col, columns[i])
		}
		quoted := regexp.MustCompile(`'([^']+)'`).FindAllStringSubmatch(string(m[2]), -1)
		fromSQL := make(map[string]struct{}, len(quoted))
		for _, q := range quoted {
			fromSQL[q[1]] = struct{}{}
		}
		if len(fromSQL) != len(fromDomain) {
			t.Fatalf("%s CHECK has %d values, AllVisibilityModes has %d", col, len(fromSQL), len(fromDomain))
		}
		for v := range fromDomain {
			if _, ok := fromSQL[v]; !ok {
				t.Errorf("AllVisibilityModes value %q missing from %s CHECK", v, col)
			}
		}
		if firstSet == nil {
			firstSet = fromSQL
			continue
		}
		if len(fromSQL) != len(firstSet) {
			t.Fatalf("%s allowed set size %d != first column %d", col, len(fromSQL), len(firstSet))
		}
		for v := range firstSet {
			if _, ok := fromSQL[v]; !ok {
				t.Errorf("%s missing value %q present on first column", col, v)
			}
		}
	}
}

func TestAutoBasis(t *testing.T) {
	okIn := catalog.VisibilityInputs{
		QuantityPositive:   true,
		HasPrice:           true,
		HasStoreAssignment: true,
	}
	opts := catalog.VisibilityOptions{}
	if !catalog.AutoBasis(catalog.StatusActive, okIn, opts) {
		t.Fatal("expected true for complete active inputs")
	}

	tests := []struct {
		name   string
		status catalog.Status
		mut    func(*catalog.VisibilityInputs)
	}{
		{"draft", catalog.StatusDraft, nil},
		{"archived", catalog.StatusArchived, nil},
		{"no qty", catalog.StatusActive, func(in *catalog.VisibilityInputs) { in.QuantityPositive = false }},
		{"no price", catalog.StatusActive, func(in *catalog.VisibilityInputs) { in.HasPrice = false }},
		{"no store", catalog.StatusActive, func(in *catalog.VisibilityInputs) { in.HasStoreAssignment = false }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := okIn
			if tc.mut != nil {
				tc.mut(&in)
			}
			if catalog.AutoBasis(tc.status, in, opts) {
				t.Fatal("expected false")
			}
		})
	}

	t.Run("category optional by default", func(t *testing.T) {
		in := okIn
		in.HasCategory = false
		if !catalog.AutoBasis(catalog.StatusActive, in, opts) {
			t.Fatal("uncategorized should pass when RequireCategory is false")
		}
	})
	t.Run("category required when toggled", func(t *testing.T) {
		in := okIn
		in.HasCategory = false
		req := catalog.VisibilityOptions{RequireCategory: true}
		if catalog.AutoBasis(catalog.StatusActive, in, req) {
			t.Fatal("expected false without category")
		}
		in.HasCategory = true
		if !catalog.AutoBasis(catalog.StatusActive, in, req) {
			t.Fatal("expected true with category")
		}
	})
	t.Run("HasPrice includes zero amount rows", func(t *testing.T) {
		// HasPrice means a price row exists (PR-1048 amount 0 still counts).
		in := okIn
		in.HasPrice = true
		if !catalog.AutoBasis(catalog.StatusActive, in, opts) {
			t.Fatal("price row with amount 0 must satisfy HasPrice")
		}
	})
}

func TestResolveAxis(t *testing.T) {
	if !catalog.ResolveAxis(catalog.VisibilityModeVisible, false) {
		t.Fatal("visible forces true")
	}
	if catalog.ResolveAxis(catalog.VisibilityModeHidden, true) {
		t.Fatal("hidden forces false")
	}
	if !catalog.ResolveAxis(catalog.VisibilityModeAuto, true) {
		t.Fatal("auto uses basis true")
	}
	if catalog.ResolveAxis(catalog.VisibilityModeAuto, false) {
		t.Fatal("auto uses basis false")
	}
	if catalog.ResolveAxis(catalog.VisibilityMode(""), true) {
		t.Fatal("empty mode fails closed")
	}
	if catalog.ResolveAxis(catalog.VisibilityMode("forced"), false) {
		t.Fatal("unknown mode fails closed when auto=false")
	}
	if catalog.ResolveAxis(catalog.VisibilityMode("forced"), true) {
		t.Fatal("unknown mode fails closed when auto=true")
	}
}

func TestVisibilityAxes_Resolve(t *testing.T) {
	in := catalog.VisibilityInputs{
		QuantityPositive:   true,
		HasPrice:           true,
		HasStoreAssignment: true,
	}
	axes := catalog.VisibilityAxes{
		Catalog:      catalog.VisibilityModeAuto,
		Search:       catalog.VisibilityModeVisible,
		Individually: catalog.VisibilityModeHidden,
		Purchasable:  catalog.VisibilityModeAuto,
	}
	got := axes.Resolve(catalog.StatusDraft, in, catalog.VisibilityOptions{})
	if got.VisibleInCatalog {
		t.Error("catalog auto on draft: want false")
	}
	if !got.VisibleInSearch {
		t.Error("search forced visible: want true")
	}
	if got.VisibleIndividually {
		t.Error("individually forced hidden: want false")
	}
	if got.Purchasable {
		t.Error("purchasable auto on draft: want false")
	}
}

func TestVisibilityAxes_Validate(t *testing.T) {
	if err := catalog.DefaultVisibilityAxes().Validate(); err != nil {
		t.Fatalf("DefaultVisibilityAxes: %v", err)
	}
	bad := catalog.DefaultVisibilityAxes()
	bad.Search = catalog.VisibilityMode("nope")
	if err := bad.Validate(); err == nil {
		t.Fatal("expected error for invalid mode")
	}
}

func TestProduct_Visibility(t *testing.T) {
	p, err := catalog.NewProduct("p1", "P", "p")
	if err != nil {
		t.Fatalf("NewProduct: %v", err)
	}
	p.Status = catalog.StatusActive
	p.VisibilityModes.Purchasable = catalog.VisibilityModeHidden
	got := p.Visibility(catalog.VisibilityInputs{
		QuantityPositive:   true,
		HasPrice:           true,
		HasStoreAssignment: true,
	}, catalog.VisibilityOptions{})
	if !got.VisibleInCatalog || !got.VisibleInSearch || !got.VisibleIndividually {
		t.Fatalf("expected catalog/search/individual true, got %+v", got)
	}
	if got.Purchasable {
		t.Fatal("purchasable forced hidden")
	}
}

func TestProduct_Visibility_UsesReceiverStatus(t *testing.T) {
	p, err := catalog.NewProduct("p1", "P", "p")
	if err != nil {
		t.Fatalf("NewProduct: %v", err)
	}
	// NewProduct leaves Status = draft; facts look "sellable".
	in := catalog.VisibilityInputs{
		QuantityPositive:   true,
		HasPrice:           true,
		HasStoreAssignment: true,
	}
	got := p.Visibility(in, catalog.VisibilityOptions{})
	if got.VisibleInCatalog || got.VisibleInSearch || got.VisibleIndividually || got.Purchasable {
		t.Fatalf("draft product with auto axes must resolve false, got %+v", got)
	}
}
