package search

import "testing"

func TestApplyVisibilityAxisFilter_Search(t *testing.T) {
	q := SearchQuery{}
	ApplyVisibilityAxisFilter(&q, VisibilityFilterSearch)
	if q.Filters[FilterVisibleInSearch] != true {
		t.Fatalf("Filters[%q] = %v, want true", FilterVisibleInSearch, q.Filters[FilterVisibleInSearch])
	}
	if _, ok := q.Filters[FilterPurchasable]; ok {
		t.Fatal("storefront search must not force purchasable")
	}
}

func TestApplyVisibilityAxisFilter_Catalog(t *testing.T) {
	q := SearchQuery{Filters: map[string]interface{}{"category": "c1"}}
	ApplyVisibilityAxisFilter(&q, VisibilityFilterCatalog)
	if q.Filters[FilterVisibleInCatalog] != true {
		t.Fatalf("Filters[%q] = %v, want true", FilterVisibleInCatalog, q.Filters[FilterVisibleInCatalog])
	}
	if q.Filters["category"] != "c1" {
		t.Fatal("existing filters must be preserved")
	}
}

func TestApplyVisibilityAxisFilter_UnknownIgnored(t *testing.T) {
	q := SearchQuery{}
	ApplyVisibilityAxisFilter(&q, "individually")
	if len(q.Filters) != 0 {
		t.Fatalf("Filters = %v, want empty", q.Filters)
	}
}

func TestApplyVisibilityAxisFilter_NilQuery(t *testing.T) {
	ApplyVisibilityAxisFilter(nil, VisibilityFilterSearch) // must not panic
}

func TestApplySuggestVisibilityAxisFilter_Search(t *testing.T) {
	q := SuggestQuery{Prefix: "ab"}
	ApplySuggestVisibilityAxisFilter(&q, VisibilityFilterSearch)
	if q.Filters[FilterVisibleInSearch] != true {
		t.Fatalf("Filters[%q] = %v, want true", FilterVisibleInSearch, q.Filters[FilterVisibleInSearch])
	}
}

func TestFilterBoolTrue(t *testing.T) {
	cases := []struct {
		v    interface{}
		want bool
	}{
		{true, true},
		{false, false},
		{"true", true},
		{"TRUE", true},
		{"1", true},
		{"yes", true},
		{"false", false},
		{1, true},
		{0, false},
		{int64(1), true},
		{float64(1), true},
		{float64(0), false},
		{nil, false},
		{"nope", false},
	}
	for _, tc := range cases {
		if got := FilterBoolTrue(tc.v); got != tc.want {
			t.Errorf("FilterBoolTrue(%v) = %v, want %v", tc.v, got, tc.want)
		}
	}
}

func TestSanitizeFilterType(t *testing.T) {
	if got, ok := SanitizeFilterType("simple"); !ok || got != "simple" {
		t.Fatalf("SanitizeFilterType(simple) = %q,%v", got, ok)
	}
	if _, ok := SanitizeFilterType("not-a-type"); ok {
		t.Fatal("expected invalid type rejected")
	}
	if _, ok := SanitizeFilterType(123); ok {
		t.Fatal("expected non-string rejected")
	}
}

func TestMeiliVisibilityEqualsTrue(t *testing.T) {
	got := MeiliVisibilityEqualsTrue(FilterVisibleInSearch)
	want := "(visible_in_search = true OR visible_in_search NOT EXISTS)"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestMeiliStatusActiveOrMissing(t *testing.T) {
	got := MeiliStatusActiveOrMissing()
	want := `(status = "active" OR status NOT EXISTS)`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestSuggestQuery_EffectiveLimit(t *testing.T) {
	if (SuggestQuery{}).EffectiveLimit() != DefaultSuggestLimit {
		t.Fatal("zero limit should default")
	}
	if (SuggestQuery{Limit: 100}).EffectiveLimit() != MaxSuggestLimit {
		t.Fatal("should clamp to max")
	}
	if (SuggestQuery{Limit: 3}).EffectiveLimit() != 3 {
		t.Fatal("passthrough")
	}
}
