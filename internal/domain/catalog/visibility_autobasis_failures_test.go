package catalog_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/akarso/shopanda/internal/domain/catalog"
)

// TestAxisExplanation_NoJSONTags locks in the PR-1059 CR fix: AxisExplanation
// is a domain type and must not carry an HTTP wire shape. Default
// encoding/json field-name behavior (no tags) always serializes exported Go
// field names verbatim (PascalCase); a stray json tag would change this.
func TestAxisExplanation_NoJSONTags(t *testing.T) {
	exp := catalog.AxisExplanation{
		Mode: catalog.VisibilityModeAuto, Override: false, Resolved: true, Effective: true,
		FailureCodes: []string{catalog.AutoBasisCodeNoStock},
	}
	b, err := json.Marshal(exp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, field := range []string{"Mode", "Override", "Resolved", "Effective", "FailureCodes"} {
		if _, ok := out[field]; !ok {
			t.Errorf("expected PascalCase field %q in %s (no json tags on AxisExplanation)", field, b)
		}
	}
}

func TestAutoBasisFailureCodes_AllPass(t *testing.T) {
	in := catalog.VisibilityInputs{
		QuantityPositive:   true,
		HasPrice:           true,
		HasStoreAssignment: true,
	}
	got := catalog.AutoBasisFailureCodes(catalog.StatusActive, in, catalog.VisibilityOptions{})
	if len(got) != 0 {
		t.Fatalf("codes = %v, want none", got)
	}
	if !catalog.AutoBasis(catalog.StatusActive, in, catalog.VisibilityOptions{}) {
		t.Fatal("sanity: AutoBasis should be true for the same inputs")
	}
}

func TestAutoBasisFailureCodes_EachCondition(t *testing.T) {
	okIn := catalog.VisibilityInputs{
		QuantityPositive:   true,
		HasPrice:           true,
		HasStoreAssignment: true,
	}
	tests := []struct {
		name   string
		status catalog.Status
		mut    func(*catalog.VisibilityInputs)
		opts   catalog.VisibilityOptions
		want   string
	}{
		{"draft status", catalog.StatusDraft, nil, catalog.VisibilityOptions{}, catalog.AutoBasisCodeStatusInactive},
		{"archived status", catalog.StatusArchived, nil, catalog.VisibilityOptions{}, catalog.AutoBasisCodeStatusInactive},
		{"no qty", catalog.StatusActive, func(in *catalog.VisibilityInputs) { in.QuantityPositive = false }, catalog.VisibilityOptions{}, catalog.AutoBasisCodeNoStock},
		{"no price", catalog.StatusActive, func(in *catalog.VisibilityInputs) { in.HasPrice = false }, catalog.VisibilityOptions{}, catalog.AutoBasisCodeNoPrice},
		{"no store", catalog.StatusActive, func(in *catalog.VisibilityInputs) { in.HasStoreAssignment = false }, catalog.VisibilityOptions{}, catalog.AutoBasisCodeNoStoreAssignment},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := okIn
			if tc.mut != nil {
				tc.mut(&in)
			}
			got := catalog.AutoBasisFailureCodes(tc.status, in, tc.opts)
			if len(got) != 1 || got[0] != tc.want {
				t.Fatalf("codes = %v, want [%q]", got, tc.want)
			}
		})
	}
}

func TestAutoBasisFailureCodes_CategoryOnlyWhenRequired(t *testing.T) {
	in := catalog.VisibilityInputs{
		QuantityPositive:   true,
		HasPrice:           true,
		HasStoreAssignment: true,
		HasCategory:        false,
	}
	if got := catalog.AutoBasisFailureCodes(catalog.StatusActive, in, catalog.VisibilityOptions{}); len(got) != 0 {
		t.Fatalf("codes = %v, want none (category optional by default)", got)
	}
	req := catalog.VisibilityOptions{RequireCategory: true}
	got := catalog.AutoBasisFailureCodes(catalog.StatusActive, in, req)
	if len(got) != 1 || got[0] != catalog.AutoBasisCodeNoCategory {
		t.Fatalf("codes = %v, want [%q]", got, catalog.AutoBasisCodeNoCategory)
	}
}

func TestAutoBasisFailureCodes_CollectsEveryFailure(t *testing.T) {
	// AutoBasis itself short-circuits on the first failing check, but the
	// admin GUI needs the complete list — fixing one condition must not make
	// the explanation disappear while the axis is still hidden.
	in := catalog.VisibilityInputs{
		QuantityPositive:   false,
		HasPrice:           false,
		HasStoreAssignment: false,
		HasCategory:        false,
	}
	opts := catalog.VisibilityOptions{RequireCategory: true}
	got := catalog.AutoBasisFailureCodes(catalog.StatusDraft, in, opts)
	want := []string{
		catalog.AutoBasisCodeStatusInactive,
		catalog.AutoBasisCodeNoStock,
		catalog.AutoBasisCodeNoPrice,
		catalog.AutoBasisCodeNoStoreAssignment,
		catalog.AutoBasisCodeNoCategory,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("codes = %v, want %v (stable order mirroring AutoBasis)", got, want)
	}
}

func TestAutoBasisInputFailureCodes_ExcludesStatus(t *testing.T) {
	in := catalog.VisibilityInputs{
		QuantityPositive:   false,
		HasPrice:           false,
		HasStoreAssignment: false,
		HasCategory:        false,
	}
	opts := catalog.VisibilityOptions{RequireCategory: true}
	got := catalog.AutoBasisInputFailureCodes(in, opts)
	want := []string{
		catalog.AutoBasisCodeNoStock,
		catalog.AutoBasisCodeNoPrice,
		catalog.AutoBasisCodeNoStoreAssignment,
		catalog.AutoBasisCodeNoCategory,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("codes = %v, want %v (status excluded)", got, want)
	}
	for _, c := range got {
		if c == catalog.AutoBasisCodeStatusInactive {
			t.Fatalf("AutoBasisInputFailureCodes must never include %q", catalog.AutoBasisCodeStatusInactive)
		}
	}
}

func TestAutoBasisInputFailureCodes_AllPassIsEmpty(t *testing.T) {
	in := catalog.VisibilityInputs{QuantityPositive: true, HasPrice: true, HasStoreAssignment: true, HasCategory: true}
	got := catalog.AutoBasisInputFailureCodes(in, catalog.VisibilityOptions{RequireCategory: true})
	if len(got) != 0 {
		t.Fatalf("codes = %v, want none", got)
	}
}

// AutoBasisFailureCodes must equal AutoBasisCodeStatusInactive (when
// inactive) prepended to AutoBasisInputFailureCodes — the live-preview
// combination the admin SPA performs client-side must match the server's
// combined list exactly.
func TestAutoBasisFailureCodes_MatchesStatusPlusInputCodes(t *testing.T) {
	in := catalog.VisibilityInputs{QuantityPositive: false, HasPrice: true, HasStoreAssignment: true}
	opts := catalog.VisibilityOptions{}

	draft := catalog.AutoBasisFailureCodes(catalog.StatusDraft, in, opts)
	wantDraft := append([]string{catalog.AutoBasisCodeStatusInactive}, catalog.AutoBasisInputFailureCodes(in, opts)...)
	if !reflect.DeepEqual(draft, wantDraft) {
		t.Fatalf("codes = %v, want %v", draft, wantDraft)
	}

	active := catalog.AutoBasisFailureCodes(catalog.StatusActive, in, opts)
	wantActive := catalog.AutoBasisInputFailureCodes(in, opts)
	if !reflect.DeepEqual(active, wantActive) {
		t.Fatalf("codes = %v, want %v", active, wantActive)
	}
}

func TestExplainAxis_FailureCodesIsIndependentCopy(t *testing.T) {
	shared := []string{catalog.AutoBasisCodeNoStock, catalog.AutoBasisCodeNoPrice}

	a := catalog.ExplainAxis(catalog.VisibilityModeAuto, catalog.StatusActive, false, shared)
	b := catalog.ExplainAxis(catalog.VisibilityModeAuto, catalog.StatusActive, false, shared)

	a.FailureCodes[0] = "mutated"

	if b.FailureCodes[0] != catalog.AutoBasisCodeNoStock {
		t.Fatalf("b.FailureCodes was mutated via a shared backing array: %v", b.FailureCodes)
	}
	if shared[0] != catalog.AutoBasisCodeNoStock {
		t.Fatalf("the original failureCodes slice passed to ExplainAxis was mutated: %v", shared)
	}
}

func TestExplainAxes_FailureCodesIsIndependentPerAxis(t *testing.T) {
	axes := catalog.VisibilityAxes{
		Catalog:      catalog.VisibilityModeAuto,
		Search:       catalog.VisibilityModeAuto,
		Individually: catalog.VisibilityModeAuto,
		Purchasable:  catalog.VisibilityModeAuto,
	}
	in := catalog.VisibilityInputs{QuantityPositive: false, HasPrice: false, HasStoreAssignment: true}

	autoBasis, codes, axesExp := catalog.ExplainAxes(axes, catalog.StatusActive, in, catalog.VisibilityOptions{})
	if autoBasis {
		t.Fatal("autoBasis should be false")
	}

	axesExp.Catalog.FailureCodes[0] = "mutated"

	if axesExp.Search.FailureCodes[0] != catalog.AutoBasisCodeNoStock {
		t.Fatalf("Search.FailureCodes was mutated via Catalog's backing array: %v", axesExp.Search.FailureCodes)
	}
	if codes[0] != catalog.AutoBasisCodeNoStock {
		t.Fatalf("ExplainAxes' returned codes slice was mutated via an axis's backing array: %v", codes)
	}
}

func TestEffectiveAxis_NonActiveAlwaysFalse(t *testing.T) {
	// Even a forced-visible axis never shows on the public storefront for a
	// non-active product — LoadMany (active-only) omits the row entirely.
	if catalog.EffectiveAxis(catalog.VisibilityModeVisible, catalog.StatusDraft, true) {
		t.Error("forced visible + draft should be effective=false")
	}
	if catalog.EffectiveAxis(catalog.VisibilityModeVisible, catalog.StatusArchived, true) {
		t.Error("forced visible + archived should be effective=false")
	}
}

func TestEffectiveAxis_ActiveMatchesResolveAxis(t *testing.T) {
	for _, mode := range catalog.AllVisibilityModes() {
		for _, auto := range []bool{true, false} {
			want := catalog.ResolveAxis(mode, auto)
			got := catalog.EffectiveAxis(mode, catalog.StatusActive, auto)
			if got != want {
				t.Errorf("EffectiveAxis(%q, active, %v) = %v, want %v (ResolveAxis)", mode, auto, got, want)
			}
		}
	}
}

func TestExplainAxis_Auto(t *testing.T) {
	codes := []string{catalog.AutoBasisCodeNoStock}

	hidden := catalog.ExplainAxis(catalog.VisibilityModeAuto, catalog.StatusActive, false, codes)
	if hidden.Override {
		t.Error("auto mode is never an override")
	}
	if hidden.Resolved {
		t.Error("resolved should be false")
	}
	if hidden.Effective {
		t.Error("effective should be false")
	}
	if !reflect.DeepEqual(hidden.FailureCodes, codes) {
		t.Errorf("FailureCodes = %v, want %v (auto + hidden surfaces failure codes)", hidden.FailureCodes, codes)
	}

	visible := catalog.ExplainAxis(catalog.VisibilityModeAuto, catalog.StatusActive, true, codes)
	if visible.FailureCodes != nil {
		t.Errorf("FailureCodes = %v, want nil (auto + visible has nothing to explain)", visible.FailureCodes)
	}
	if !visible.Resolved || !visible.Effective {
		t.Error("auto + active + auto-basis-true should resolve and be effective")
	}
}

func TestExplainAxis_Forced(t *testing.T) {
	codes := []string{catalog.AutoBasisCodeNoStock}

	forcedVisible := catalog.ExplainAxis(catalog.VisibilityModeVisible, catalog.StatusActive, true, codes)
	if !forcedVisible.Override {
		t.Error("forced visible mode is an override")
	}
	if forcedVisible.FailureCodes != nil {
		t.Errorf("FailureCodes = %v, want nil (forced axis has nothing to explain)", forcedVisible.FailureCodes)
	}
	if !forcedVisible.Effective {
		t.Error("forced visible + active should be effective")
	}

	forcedHidden := catalog.ExplainAxis(catalog.VisibilityModeHidden, catalog.StatusActive, false, codes)
	if !forcedHidden.Override {
		t.Error("forced hidden mode is an override")
	}
	if forcedHidden.FailureCodes != nil {
		t.Errorf("FailureCodes = %v, want nil (forced axis has nothing to explain, even when resolved false)", forcedHidden.FailureCodes)
	}
}

func TestExplainAxis_ForcedVisibleInactiveNotEffective(t *testing.T) {
	// The key scenario this PR fixes: a merchant forces an axis visible on a
	// draft product. Resolved (domain-level) is true, but Effective (public
	// gate) must stay false — the product is not active, so it is never on
	// the storefront regardless of the forced mode.
	exp := catalog.ExplainAxis(catalog.VisibilityModeVisible, catalog.StatusDraft, false, nil)
	if !exp.Resolved {
		t.Error("Resolved should be true (ResolveAxis ignores status)")
	}
	if exp.Effective {
		t.Error("Effective should be false (draft status always denies)")
	}
}

func TestExplainAxes(t *testing.T) {
	axes := catalog.VisibilityAxes{
		Catalog:      catalog.VisibilityModeVisible,
		Search:       catalog.VisibilityModeAuto,
		Individually: catalog.VisibilityModeAuto,
		Purchasable:  catalog.VisibilityModeHidden,
	}
	in := catalog.VisibilityInputs{
		QuantityPositive:   false,
		HasPrice:           true,
		HasStoreAssignment: true,
	}
	autoBasis, codes, axesExp := catalog.ExplainAxes(axes, catalog.StatusActive, in, catalog.VisibilityOptions{})
	if autoBasis {
		t.Fatal("autoBasis should be false (no stock)")
	}
	wantCodes := []string{catalog.AutoBasisCodeNoStock}
	if !reflect.DeepEqual(codes, wantCodes) {
		t.Fatalf("codes = %v, want %v", codes, wantCodes)
	}
	if !axesExp.Catalog.Effective {
		t.Error("axesExp.Catalog.Effective should be true (forced visible + active)")
	}
	if axesExp.Search.Effective {
		t.Error("axesExp.Search.Effective should be false (auto + no stock)")
	}
	if axesExp.Purchasable.Effective {
		t.Error("axesExp.Purchasable.Effective should be false (forced hidden)")
	}
}
