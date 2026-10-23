package catalog_test

import (
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/domain/catalog"
)

func TestNewGroupedMember_TrimsIDs(t *testing.T) {
	m, err := catalog.NewGroupedMember(" group-1 ", " member-1 ")
	if err != nil {
		t.Fatalf("NewGroupedMember: %v", err)
	}
	if m.GroupProductID != "group-1" || m.MemberProductID != "member-1" {
		t.Errorf("ids not trimmed: %+v", m)
	}
}

func TestGroupedMember_Validate(t *testing.T) {
	valid := &catalog.GroupedMember{
		GroupProductID:  "group-1",
		MemberProductID: "member-1",
	}

	tests := []struct {
		name    string
		m       *catalog.GroupedMember
		wantErr string
	}{
		{"valid", valid, ""},
		{"nil", nil, "must not be nil"},
		{"empty group id", &catalog.GroupedMember{MemberProductID: "m"}, "group product id"},
		{"empty member id", &catalog.GroupedMember{GroupProductID: "g"}, "member product id"},
		{"self reference", &catalog.GroupedMember{GroupProductID: "same", MemberProductID: "same"}, "itself"},
		{"whitespace group id", &catalog.GroupedMember{GroupProductID: "  ", MemberProductID: "m"}, "group product id"},
		{"whitespace member id", &catalog.GroupedMember{GroupProductID: "g", MemberProductID: "  "}, "member product id"},
		{"padded self reference", &catalog.GroupedMember{GroupProductID: " same ", MemberProductID: "same"}, "itself"},
		{"padded ids are not trimmed", &catalog.GroupedMember{GroupProductID: "  g  ", MemberProductID: "  m  "}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var beforeGroup, beforeMember string
			if tc.m != nil {
				beforeGroup, beforeMember = tc.m.GroupProductID, tc.m.MemberProductID
			}
			err := tc.m.Validate()
			if tc.m != nil && (tc.m.GroupProductID != beforeGroup || tc.m.MemberProductID != beforeMember) {
				t.Fatalf("Validate mutated ids: %+v", tc.m)
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

func TestGroupedMember_ValidateComposition(t *testing.T) {
	m := &catalog.GroupedMember{
		GroupProductID:  "group-1",
		MemberProductID: "member-1",
	}

	for _, leaf := range []catalog.Type{catalog.TypeSimple, catalog.TypeVirtual, catalog.TypeDownloadable} {
		if err := m.ValidateComposition(catalog.TypeGrouped, leaf); err != nil {
			t.Fatalf("ValidateComposition(grouped, %s) = %v", leaf, err)
		}
	}

	if err := m.ValidateComposition(catalog.TypeSimple, catalog.TypeSimple); err == nil {
		t.Fatal("expected error when parent is not grouped")
	}
	if err := m.ValidateComposition(catalog.TypeBundle, catalog.TypeSimple); err == nil {
		t.Fatal("expected error when parent is bundle")
	}

	for _, bad := range []catalog.Type{catalog.TypeBundle, catalog.TypeGrouped, catalog.TypeConfigurable} {
		if err := m.ValidateComposition(catalog.TypeGrouped, bad); err == nil {
			t.Fatalf("expected error when member is %s", bad)
		}
	}
}

func TestType_IsGroupedMemberLeafType(t *testing.T) {
	if !catalog.TypeSimple.IsGroupedMemberLeafType() {
		t.Error("simple should be a grouped member leaf")
	}
	if !catalog.TypeVirtual.IsGroupedMemberLeafType() {
		t.Error("virtual should be a grouped member leaf")
	}
	if !catalog.TypeDownloadable.IsGroupedMemberLeafType() {
		t.Error("downloadable should be a grouped member leaf")
	}
	if catalog.TypeGrouped.IsGroupedMemberLeafType() {
		t.Error("grouped should not be a grouped member leaf")
	}
	if catalog.TypeBundle.IsGroupedMemberLeafType() {
		t.Error("bundle should not be a grouped member leaf")
	}
	if catalog.TypeConfigurable.IsGroupedMemberLeafType() {
		t.Error("configurable should not be a grouped member leaf")
	}
}
