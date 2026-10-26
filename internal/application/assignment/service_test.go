package assignment_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	app "github.com/akarso/shopanda/internal/application/assignment"
	"github.com/akarso/shopanda/internal/domain/assignment"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"
)

type stubRepo struct {
	assignment.Repository
	byID      map[string]*assignment.ProductAssignment
	current   map[string]*assignment.ProductAssignment
	list      []assignment.ProductAssignment
	inserts   []*assignment.ProductAssignment
	updates   []*assignment.ProductAssignment
	insertErr error
	updateErr error
	findErr   error
}

func (s *stubRepo) Insert(_ context.Context, a *assignment.ProductAssignment) error {
	if s.insertErr != nil {
		return s.insertErr
	}
	s.inserts = append(s.inserts, a)
	if s.byID == nil {
		s.byID = map[string]*assignment.ProductAssignment{}
	}
	if s.current == nil {
		s.current = map[string]*assignment.ProductAssignment{}
	}
	cp := *a
	s.byID[a.ID] = &cp
	if a.Status().IsCurrent() {
		s.current[a.ChildOrderItemID] = &cp
	}
	return nil
}

func (s *stubRepo) Update(_ context.Context, a *assignment.ProductAssignment) error {
	if s.updateErr != nil {
		return s.updateErr
	}
	s.updates = append(s.updates, a)
	a.AcceptStoredStatus()
	if s.byID == nil {
		s.byID = map[string]*assignment.ProductAssignment{}
	}
	if s.current == nil {
		s.current = map[string]*assignment.ProductAssignment{}
	}
	cp := *a
	s.byID[a.ID] = &cp
	if a.Status().IsCurrent() {
		s.current[a.ChildOrderItemID] = &cp
	} else {
		delete(s.current, a.ChildOrderItemID)
	}
	return nil
}

func (s *stubRepo) FindByID(_ context.Context, id string) (*assignment.ProductAssignment, error) {
	if s.findErr != nil {
		return nil, s.findErr
	}
	if s.byID == nil {
		return nil, nil
	}
	a := s.byID[id]
	if a == nil {
		return nil, nil
	}
	cp := *a
	return &cp, nil
}

func (s *stubRepo) FindCurrentByChildID(_ context.Context, childOrderItemID string) (*assignment.ProductAssignment, error) {
	if s.findErr != nil {
		return nil, s.findErr
	}
	if s.current == nil {
		return nil, nil
	}
	a := s.current[childOrderItemID]
	if a == nil {
		return nil, nil
	}
	cp := *a
	return &cp, nil
}

func (s *stubRepo) ListByChildID(_ context.Context, _ string) ([]assignment.ProductAssignment, error) {
	if s.findErr != nil {
		return nil, s.findErr
	}
	if s.list == nil {
		return []assignment.ProductAssignment{}, nil
	}
	return s.list, nil
}

func TestService_CreateUnassignedAndAssigned(t *testing.T) {
	repo := &stubRepo{}
	svc := app.NewService(repo)
	ctx := context.Background()

	u, err := svc.CreateUnassigned(ctx, " child-1 ")
	if err != nil {
		t.Fatalf("CreateUnassigned: %v", err)
	}
	if u.ChildOrderItemID != "child-1" || u.Status() != assignment.StatusUnassigned {
		t.Fatalf("unassigned = %+v", u)
	}
	if !id.IsValid(u.ID) {
		t.Fatalf("id = %q", u.ID)
	}
	if len(repo.inserts) != 1 {
		t.Fatalf("inserts = %d", len(repo.inserts))
	}

	_, err = svc.CreateUnassigned(ctx, "child-1")
	if !apperror.Is(err, apperror.CodeConflict) {
		t.Fatalf("second unassigned = %v, want conflict", err)
	}

	a, err := svc.CreateAssigned(ctx, " parent-2 ", " child-2 ")
	if err != nil {
		t.Fatalf("CreateAssigned: %v", err)
	}
	if a.ParentOrderItemID != "parent-2" || a.ChildOrderItemID != "child-2" || a.Status() != assignment.StatusAssigned {
		t.Fatalf("assigned = %+v", a)
	}
}

func TestService_CreateAssigned_RejectsSelfAndEmpty(t *testing.T) {
	svc := app.NewService(&stubRepo{})
	ctx := context.Background()
	if _, err := svc.CreateAssigned(ctx, "same", "same"); !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("self = %v", err)
	}
	if _, err := svc.CreateUnassigned(ctx, "  "); !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("empty child = %v", err)
	}
	if _, err := svc.CreateAssigned(ctx, "p", ""); !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("empty child assigned = %v", err)
	}
}

func TestService_AssignAndRevoke(t *testing.T) {
	repo := &stubRepo{}
	svc := app.NewService(repo)
	ctx := context.Background()

	u, err := svc.CreateUnassigned(ctx, "child-1")
	if err != nil {
		t.Fatalf("CreateUnassigned: %v", err)
	}
	assigned, err := svc.Assign(ctx, " "+u.ID+" ", " parent-1 ")
	if err != nil {
		t.Fatalf("Assign: %v", err)
	}
	if assigned.Status() != assignment.StatusAssigned || assigned.ParentOrderItemID != "parent-1" {
		t.Fatalf("assigned = %+v", assigned)
	}
	if assigned.StoredStatus() != assignment.StatusAssigned {
		t.Fatalf("stored after update = %q", assigned.StoredStatus())
	}

	revoked, err := svc.Revoke(ctx, assigned.ID)
	if err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if revoked.Status() != assignment.StatusRevoked || revoked.RevokedAt == nil {
		t.Fatalf("revoked = %+v", revoked)
	}

	if _, err := svc.Assign(ctx, revoked.ID, "parent-2"); !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("assign revoked = %v", err)
	}
	if _, err := svc.Revoke(ctx, revoked.ID); !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("revoke revoked = %v", err)
	}

	// History stays; a new assigned row can replace the revoked one.
	next, err := svc.CreateAssigned(ctx, "parent-2", "child-1")
	if err != nil {
		t.Fatalf("CreateAssigned after revoke: %v", err)
	}
	if next.ID == revoked.ID {
		t.Fatal("reassignment must be a new row")
	}
}

func TestService_RevokeUnassigned(t *testing.T) {
	repo := &stubRepo{}
	svc := app.NewService(repo)
	ctx := context.Background()

	u, err := svc.CreateUnassigned(ctx, "child-1")
	if err != nil {
		t.Fatalf("CreateUnassigned: %v", err)
	}
	revoked, err := svc.Revoke(ctx, u.ID)
	if err != nil {
		t.Fatalf("Revoke unassigned: %v", err)
	}
	if revoked.Status() != assignment.StatusRevoked || revoked.ParentOrderItemID != "" {
		t.Fatalf("revoked = %+v", revoked)
	}
	current, err := svc.CurrentByChild(ctx, "child-1")
	if err != nil || current != nil {
		t.Fatalf("CurrentByChild after unassigned revoke = %+v %v", current, err)
	}
	next, err := svc.CreateAssigned(ctx, "parent-1", "child-1")
	if err != nil {
		t.Fatalf("CreateAssigned after unassigned revoke: %v", err)
	}
	if next.ID == revoked.ID {
		t.Fatal("replacement must be a new row")
	}
}

func TestService_GetAndList(t *testing.T) {
	repo := &stubRepo{
		list: []assignment.ProductAssignment{},
	}
	svc := app.NewService(repo)
	ctx := context.Background()

	if _, err := svc.Get(ctx, ""); !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("empty get = %v", err)
	}
	if _, err := svc.Get(ctx, "nope"); !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("invalid id get = %v", err)
	}
	if _, err := svc.Get(ctx, id.New()); !apperror.Is(err, apperror.CodeNotFound) {
		t.Fatalf("missing get = %v", err)
	}

	created, err := svc.CreateAssigned(ctx, "parent-1", "child-1")
	if err != nil {
		t.Fatalf("CreateAssigned: %v", err)
	}
	got, err := svc.Get(ctx, created.ID)
	if err != nil || got.ID != created.ID {
		t.Fatalf("Get = %+v %v", got, err)
	}

	current, err := svc.CurrentByChild(ctx, " child-1 ")
	if err != nil || current == nil || current.ID != created.ID {
		t.Fatalf("CurrentByChild = %+v %v", current, err)
	}
	missing, err := svc.CurrentByChild(ctx, "other")
	if err != nil || missing != nil {
		t.Fatalf("CurrentByChild missing = %+v %v", missing, err)
	}
	if _, err := svc.CurrentByChild(ctx, "  "); !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("empty current = %v", err)
	}

	repo.list = nil
	rows, err := svc.ListByChild(ctx, "child-1")
	if err != nil || rows == nil {
		t.Fatalf("ListByChild nil = %v %v", rows, err)
	}
	if _, err := svc.ListByChild(ctx, ""); !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("empty list = %v", err)
	}
}

func TestService_UpdateConflict(t *testing.T) {
	repo := &stubRepo{updateErr: assignment.ErrStatusConflict}
	svc := app.NewService(repo)
	ctx := context.Background()
	u, err := svc.CreateUnassigned(ctx, "child-1")
	if err != nil {
		t.Fatalf("CreateUnassigned: %v", err)
	}
	if _, err := svc.Assign(ctx, u.ID, "parent-1"); !apperror.Is(err, apperror.CodeConflict) {
		t.Fatalf("Assign conflict = %v", err)
	}
}

func TestService_InsertConflictPassthrough(t *testing.T) {
	repo := &stubRepo{insertErr: apperror.Conflict("child already has an active assignment")}
	svc := app.NewService(repo)
	if _, err := svc.CreateUnassigned(context.Background(), "child-1"); !apperror.Is(err, apperror.CodeConflict) {
		t.Fatalf("insert conflict = %v", err)
	}
}

func TestService_WrappedFindError(t *testing.T) {
	repo := &stubRepo{findErr: errors.New("db down")}
	svc := app.NewService(repo)
	_, err := svc.Get(context.Background(), id.New())
	if err == nil || !strings.Contains(err.Error(), "db down") {
		t.Fatalf("Get wrap = %v", err)
	}
}

func TestNewService_NilRepoPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	app.NewService(nil)
}
