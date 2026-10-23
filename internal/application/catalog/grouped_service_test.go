package catalog_test

import (
	"context"
	"database/sql"
	"testing"

	catalogApp "github.com/akarso/shopanda/internal/application/catalog"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

func newGroupedService(products catalog.ProductRepository, members catalog.GroupedMemberRepository) *catalogApp.GroupedService {
	return catalogApp.NewGroupedService(products, members, testTxStarter())
}

type stubGroupedMemberRepo struct {
	catalog.GroupedMemberRepository
	list        []catalog.GroupedMember
	listGroupID string
	inserted    *catalog.GroupedMember
	removed     struct {
		groupID  string
		memberID string
	}
	removeErr error
	calls     *[]string
}

func (s *stubGroupedMemberRepo) ListByGroupID(_ context.Context, groupProductID string) ([]catalog.GroupedMember, error) {
	s.listGroupID = groupProductID
	return s.list, nil
}

func (s *stubGroupedMemberRepo) CountByGroupID(context.Context, string) (int, error) {
	return 0, nil
}

func (s *stubGroupedMemberRepo) CountByMemberID(context.Context, string) (int, error) {
	return 0, nil
}

func (s *stubGroupedMemberRepo) Insert(_ context.Context, m *catalog.GroupedMember) error {
	if s.calls != nil {
		*s.calls = append(*s.calls, "insert")
	}
	cp := *m
	s.inserted = &cp
	return nil
}

func (s *stubGroupedMemberRepo) WithTx(_ *sql.Tx) catalog.GroupedMemberRepository { return s }

func (s *stubGroupedMemberRepo) Remove(_ context.Context, groupProductID, memberProductID string) error {
	s.removed.groupID = groupProductID
	s.removed.memberID = memberProductID
	return s.removeErr
}

func TestGroupedService_Add_LocksBeforeInsert(t *testing.T) {
	var calls []string
	products := &stubProductRepo{
		byID: map[string]*catalog.Product{
			"zzz": {ID: "zzz", Type: catalog.TypeGrouped, Status: catalog.StatusActive},
			"aaa": {ID: "aaa", Type: catalog.TypeSimple, Status: catalog.StatusActive},
		},
		calls: &calls,
	}
	members := &stubGroupedMemberRepo{calls: &calls}
	svc := newGroupedService(products, members)

	m, err := catalog.NewGroupedMember("zzz", "aaa")
	if err != nil {
		t.Fatalf("NewGroupedMember: %v", err)
	}
	if err := svc.Add(context.Background(), m); err != nil {
		t.Fatalf("Add: %v", err)
	}
	want := []string{"lock:aaa", "lock:zzz", "insert"}
	if len(calls) != len(want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Fatalf("calls = %v, want %v", calls, want)
		}
	}
}

func TestGroupedService_Add_ValidComposition(t *testing.T) {
	products := &stubProductRepo{byID: map[string]*catalog.Product{
		"g1": {ID: "g1", Type: catalog.TypeGrouped, Status: catalog.StatusActive},
		"m1": {ID: "m1", Type: catalog.TypeVirtual, Status: catalog.StatusActive},
	}}
	members := &stubGroupedMemberRepo{}
	svc := newGroupedService(products, members)

	m, err := catalog.NewGroupedMember("g1", "m1")
	if err != nil {
		t.Fatalf("NewGroupedMember: %v", err)
	}
	if err := svc.Add(context.Background(), m); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if members.inserted == nil {
		t.Fatal("expected insert")
	}
	if members.inserted.GroupProductID != "g1" || members.inserted.MemberProductID != "m1" {
		t.Errorf("insert = %+v", members.inserted)
	}
}

func TestGroupedService_Add_TrimsIDs(t *testing.T) {
	products := &stubProductRepo{byID: map[string]*catalog.Product{
		"g1": {ID: "g1", Type: catalog.TypeGrouped, Status: catalog.StatusActive},
		"m1": {ID: "m1", Type: catalog.TypeSimple, Status: catalog.StatusActive},
	}}
	members := &stubGroupedMemberRepo{}
	svc := newGroupedService(products, members)

	m := &catalog.GroupedMember{
		GroupProductID:  "  g1  ",
		MemberProductID: " m1 ",
	}
	if err := svc.Add(context.Background(), m); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if m.GroupProductID != "g1" || m.MemberProductID != "m1" {
		t.Fatalf("ids = %q %q, want trimmed", m.GroupProductID, m.MemberProductID)
	}
	if members.inserted == nil || members.inserted.GroupProductID != "g1" || members.inserted.MemberProductID != "m1" {
		t.Fatalf("insert = %+v", members.inserted)
	}
}

func TestGroupedService_List_EmptyID(t *testing.T) {
	svc := newGroupedService(&stubProductRepo{}, &stubGroupedMemberRepo{})

	_, err := svc.List(context.Background(), "")
	if !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("List = %v, want validation", err)
	}
}

func TestGroupedService_Add_ProductNotFound(t *testing.T) {
	products := &stubProductRepo{byID: map[string]*catalog.Product{}}
	svc := newGroupedService(products, &stubGroupedMemberRepo{})

	m, _ := catalog.NewGroupedMember("missing", "m1")
	err := svc.Add(context.Background(), m)
	if !apperror.Is(err, apperror.CodeNotFound) {
		t.Fatalf("Add = %v, want not found", err)
	}
}

func TestGroupedService_Add_MissingMember(t *testing.T) {
	products := &stubProductRepo{byID: map[string]*catalog.Product{
		"g1": {ID: "g1", Type: catalog.TypeGrouped, Status: catalog.StatusActive},
	}}
	svc := newGroupedService(products, &stubGroupedMemberRepo{})

	m, _ := catalog.NewGroupedMember("g1", "missing")
	err := svc.Add(context.Background(), m)
	if !apperror.Is(err, apperror.CodeNotFound) {
		t.Fatalf("Add = %v, want not found", err)
	}
}

func TestGroupedService_Add_InvalidComposition(t *testing.T) {
	products := &stubProductRepo{byID: map[string]*catalog.Product{
		"g1": {ID: "g1", Type: catalog.TypeSimple, Status: catalog.StatusActive},
		"m1": {ID: "m1", Type: catalog.TypeSimple, Status: catalog.StatusActive},
	}}
	svc := newGroupedService(products, &stubGroupedMemberRepo{})

	m, _ := catalog.NewGroupedMember("g1", "m1")
	err := svc.Add(context.Background(), m)
	if !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("Add = %v, want validation", err)
	}
}

func TestGroupedService_Add_RejectsBundleMember(t *testing.T) {
	products := &stubProductRepo{byID: map[string]*catalog.Product{
		"g1": {ID: "g1", Type: catalog.TypeGrouped, Status: catalog.StatusActive},
		"b1": {ID: "b1", Type: catalog.TypeBundle, Status: catalog.StatusActive},
	}}
	svc := newGroupedService(products, &stubGroupedMemberRepo{})

	m, _ := catalog.NewGroupedMember("g1", "b1")
	err := svc.Add(context.Background(), m)
	if !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("Add = %v, want validation", err)
	}
}

func TestGroupedService_Add_RejectsConfigurableMember(t *testing.T) {
	products := &stubProductRepo{byID: map[string]*catalog.Product{
		"g1": {ID: "g1", Type: catalog.TypeGrouped, Status: catalog.StatusActive},
		"x1": {ID: "x1", Type: catalog.TypeConfigurable, Status: catalog.StatusActive},
	}}
	svc := newGroupedService(products, &stubGroupedMemberRepo{})

	m, _ := catalog.NewGroupedMember("g1", "x1")
	err := svc.Add(context.Background(), m)
	if !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("Add = %v, want validation", err)
	}
}

func TestGroupedService_Add_RejectsNestedGroup(t *testing.T) {
	products := &stubProductRepo{byID: map[string]*catalog.Product{
		"g1": {ID: "g1", Type: catalog.TypeGrouped, Status: catalog.StatusActive},
		"g2": {ID: "g2", Type: catalog.TypeGrouped, Status: catalog.StatusActive},
	}}
	svc := newGroupedService(products, &stubGroupedMemberRepo{})

	m, _ := catalog.NewGroupedMember("g1", "g2")
	err := svc.Add(context.Background(), m)
	if !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("Add = %v, want validation", err)
	}
}

func TestGroupedService_Add_EmptyIDs(t *testing.T) {
	svc := newGroupedService(&stubProductRepo{}, &stubGroupedMemberRepo{})

	err := svc.Add(context.Background(), &catalog.GroupedMember{})
	if !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("Add = %v, want validation", err)
	}
}

func TestGroupedService_Add_Nil(t *testing.T) {
	svc := newGroupedService(&stubProductRepo{}, &stubGroupedMemberRepo{})

	err := svc.Add(context.Background(), nil)
	if !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("Add = %v, want validation", err)
	}
}

func TestGroupedService_List_ForwardsGroupID(t *testing.T) {
	want := []catalog.GroupedMember{{GroupProductID: "g1", MemberProductID: "m1"}}
	members := &stubGroupedMemberRepo{list: want}
	products := &stubProductRepo{byID: map[string]*catalog.Product{
		"g1": {ID: "g1", Type: catalog.TypeGrouped, Status: catalog.StatusActive},
	}}
	svc := newGroupedService(products, members)

	got, err := svc.List(context.Background(), "g1")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if members.listGroupID != "g1" {
		t.Errorf("listGroupID = %q, want g1", members.listGroupID)
	}
	if len(got) != 1 || got[0].MemberProductID != "m1" {
		t.Errorf("List = %+v", got)
	}
}

func TestGroupedService_List_NotFound(t *testing.T) {
	svc := newGroupedService(&stubProductRepo{}, &stubGroupedMemberRepo{})

	_, err := svc.List(context.Background(), "missing")
	if !apperror.Is(err, apperror.CodeNotFound) {
		t.Fatalf("List = %v, want not found", err)
	}
}

func TestGroupedService_List_NotGrouped(t *testing.T) {
	products := &stubProductRepo{byID: map[string]*catalog.Product{
		"p1": {ID: "p1", Type: catalog.TypeSimple, Status: catalog.StatusActive},
	}}
	svc := newGroupedService(products, &stubGroupedMemberRepo{})

	_, err := svc.List(context.Background(), "p1")
	if !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("List = %v, want validation", err)
	}
}

func TestGroupedService_List_NilSliceBecomesEmpty(t *testing.T) {
	products := &stubProductRepo{byID: map[string]*catalog.Product{
		"g1": {ID: "g1", Type: catalog.TypeGrouped, Status: catalog.StatusActive},
	}}
	members := &stubGroupedMemberRepo{list: nil}
	svc := newGroupedService(products, members)

	got, err := svc.List(context.Background(), "g1")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil empty slice")
	}
	if len(got) != 0 {
		t.Errorf("List = %+v", got)
	}
}

func TestGroupedService_Remove(t *testing.T) {
	members := &stubGroupedMemberRepo{}
	products := &stubProductRepo{byID: map[string]*catalog.Product{
		"g1": {ID: "g1", Type: catalog.TypeGrouped, Status: catalog.StatusActive},
	}}
	svc := newGroupedService(products, members)

	if err := svc.Remove(context.Background(), "g1", "m1"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if members.removed.groupID != "g1" || members.removed.memberID != "m1" {
		t.Errorf("removed = %+v", members.removed)
	}
}

func TestGroupedService_Remove_NotFound(t *testing.T) {
	members := &stubGroupedMemberRepo{removeErr: apperror.NotFound("grouped member not found")}
	products := &stubProductRepo{byID: map[string]*catalog.Product{
		"g1": {ID: "g1", Type: catalog.TypeGrouped, Status: catalog.StatusActive},
	}}
	svc := newGroupedService(products, members)

	err := svc.Remove(context.Background(), "g1", "m1")
	if !apperror.Is(err, apperror.CodeNotFound) {
		t.Fatalf("Remove = %v, want not found", err)
	}
}

func TestGroupedService_Remove_EmptyIDs(t *testing.T) {
	svc := newGroupedService(&stubProductRepo{}, &stubGroupedMemberRepo{})

	if err := svc.Remove(context.Background(), "", "m1"); !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("Remove empty group = %v, want validation", err)
	}
	if err := svc.Remove(context.Background(), "g1", ""); !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("Remove empty member = %v, want validation", err)
	}
	if err := svc.Remove(context.Background(), "  ", "m1"); !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("Remove blank group = %v, want validation", err)
	}
}

func TestGroupedService_List_TrimsID(t *testing.T) {
	products := &stubProductRepo{byID: map[string]*catalog.Product{
		"g1": {ID: "g1", Type: catalog.TypeGrouped, Status: catalog.StatusActive},
	}}
	members := &stubGroupedMemberRepo{}
	svc := newGroupedService(products, members)

	if _, err := svc.List(context.Background(), "  g1  "); err != nil {
		t.Fatalf("List: %v", err)
	}
	if members.listGroupID != "g1" {
		t.Fatalf("listGroupID = %q, want g1", members.listGroupID)
	}
}

func TestGroupedService_Remove_TrimsIDs(t *testing.T) {
	members := &stubGroupedMemberRepo{}
	products := &stubProductRepo{byID: map[string]*catalog.Product{
		"g1": {ID: "g1", Type: catalog.TypeGrouped, Status: catalog.StatusActive},
	}}
	svc := newGroupedService(products, members)

	if err := svc.Remove(context.Background(), " g1 ", " m1 "); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if members.removed.groupID != "g1" || members.removed.memberID != "m1" {
		t.Fatalf("removed = %+v", members.removed)
	}
}

func TestGroupedService_Add_DraftGroupRequiresOperatorScope(t *testing.T) {
	products := &stubProductRepo{byID: map[string]*catalog.Product{
		"g1": {ID: "g1", Type: catalog.TypeGrouped, Status: catalog.StatusDraft},
		"m1": {ID: "m1", Type: catalog.TypeSimple, Status: catalog.StatusActive},
	}}
	members := &stubGroupedMemberRepo{}
	svc := newGroupedService(products, members)
	m, err := catalog.NewGroupedMember("g1", "m1")
	if err != nil {
		t.Fatalf("NewGroupedMember: %v", err)
	}

	err = svc.Add(context.Background(), m)
	if !apperror.Is(err, apperror.CodeNotFound) {
		t.Fatalf("Add without scope = %v, want not found", err)
	}
	if members.inserted != nil {
		t.Fatal("insert must not run without operator scope")
	}

	ctx := catalog.WithOperatorProductReadScope(context.Background())
	if err := svc.Add(ctx, m); err != nil {
		t.Fatalf("Add with operator scope: %v", err)
	}
	if members.inserted == nil {
		t.Fatal("expected insert with operator scope")
	}
}

func TestGroupedService_Add_DraftMemberRequiresOperatorScope(t *testing.T) {
	products := &stubProductRepo{byID: map[string]*catalog.Product{
		"g1": {ID: "g1", Type: catalog.TypeGrouped, Status: catalog.StatusActive},
		"m1": {ID: "m1", Type: catalog.TypeSimple, Status: catalog.StatusDraft},
	}}
	members := &stubGroupedMemberRepo{}
	svc := newGroupedService(products, members)
	m, _ := catalog.NewGroupedMember("g1", "m1")

	err := svc.Add(context.Background(), m)
	if !apperror.Is(err, apperror.CodeNotFound) {
		t.Fatalf("Add = %v, want not found", err)
	}
	if members.inserted != nil {
		t.Fatal("insert must not run")
	}
}
