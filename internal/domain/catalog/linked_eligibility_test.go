package catalog_test

import (
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/domain/catalog"
)

func TestNewLinkedEligibleParent_TrimsIDs(t *testing.T) {
	p, err := catalog.NewLinkedEligibleParent(" child-1 ", " parent-1 ")
	if err != nil {
		t.Fatalf("NewLinkedEligibleParent: %v", err)
	}
	if p.ChildProductID != "child-1" || p.ParentProductID != "parent-1" {
		t.Errorf("ids not trimmed: %+v", p)
	}
}

func TestNewLinkedEligibleCategory_TrimsIDs(t *testing.T) {
	c, err := catalog.NewLinkedEligibleCategory(" child-1 ", " cat-1 ")
	if err != nil {
		t.Fatalf("NewLinkedEligibleCategory: %v", err)
	}
	if c.ChildProductID != "child-1" || c.CategoryID != "cat-1" {
		t.Errorf("ids not trimmed: %+v", c)
	}
}

func TestLinkedEligibleParent_Validate(t *testing.T) {
	valid := &catalog.LinkedEligibleParent{
		ChildProductID:  "child-1",
		ParentProductID: "parent-1",
	}

	tests := []struct {
		name    string
		p       *catalog.LinkedEligibleParent
		wantErr string
	}{
		{"valid", valid, ""},
		{"nil", nil, "must not be nil"},
		{"empty child id", &catalog.LinkedEligibleParent{ParentProductID: "p"}, "child product id"},
		{"empty parent id", &catalog.LinkedEligibleParent{ChildProductID: "c"}, "parent product id"},
		{"self reference", &catalog.LinkedEligibleParent{ChildProductID: "same", ParentProductID: "same"}, "itself"},
		{"whitespace child id", &catalog.LinkedEligibleParent{ChildProductID: "  ", ParentProductID: "p"}, "child product id"},
		{"whitespace parent id", &catalog.LinkedEligibleParent{ChildProductID: "c", ParentProductID: "  "}, "parent product id"},
		{"padded self reference", &catalog.LinkedEligibleParent{ChildProductID: " same ", ParentProductID: "same"}, "itself"},
		{"padded ids are not trimmed", &catalog.LinkedEligibleParent{ChildProductID: "  c  ", ParentProductID: "  p  "}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var beforeChild, beforeParent string
			if tc.p != nil {
				beforeChild, beforeParent = tc.p.ChildProductID, tc.p.ParentProductID
			}
			err := tc.p.Validate()
			if tc.p != nil && (tc.p.ChildProductID != beforeChild || tc.p.ParentProductID != beforeParent) {
				t.Fatalf("Validate mutated ids: %+v", tc.p)
			}
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Validate() = %v, want error containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestLinkedEligibleCategory_Validate(t *testing.T) {
	valid := &catalog.LinkedEligibleCategory{
		ChildProductID: "child-1",
		CategoryID:     "cat-1",
	}

	tests := []struct {
		name    string
		c       *catalog.LinkedEligibleCategory
		wantErr string
	}{
		{"valid", valid, ""},
		{"nil", nil, "must not be nil"},
		{"empty child id", &catalog.LinkedEligibleCategory{CategoryID: "cat"}, "child product id"},
		{"empty category id", &catalog.LinkedEligibleCategory{ChildProductID: "c"}, "category id"},
		{"whitespace child id", &catalog.LinkedEligibleCategory{ChildProductID: "  ", CategoryID: "cat"}, "child product id"},
		{"whitespace category id", &catalog.LinkedEligibleCategory{ChildProductID: "c", CategoryID: "  "}, "category id"},
		{"padded ids are not trimmed", &catalog.LinkedEligibleCategory{ChildProductID: "  c  ", CategoryID: "  cat  "}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var beforeChild, beforeCat string
			if tc.c != nil {
				beforeChild, beforeCat = tc.c.ChildProductID, tc.c.CategoryID
			}
			err := tc.c.Validate()
			if tc.c != nil && (tc.c.ChildProductID != beforeChild || tc.c.CategoryID != beforeCat) {
				t.Fatalf("Validate mutated ids: %+v", tc.c)
			}
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Validate() = %v, want error containing %q", err, tc.wantErr)
			}
		})
	}
}
