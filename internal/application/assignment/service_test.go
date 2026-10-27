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
	byID       map[string]*assignment.ProductAssignment
	current    map[string]*assignment.ProductAssignment
	list       []assignment.ProductAssignment
	byParent   map[string][]assignment.ProductAssignment
	lastParent string
	lastChild  string
	inserts    []*assignment.ProductAssignment
	updates    []*assignment.ProductAssignment
	insertErr  error
	updateErr  error
	findErr    error
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

func (s *stubRepo) ListByChildID(_ context.Context, childOrderItemID string) ([]assignment.ProductAssignment, error) {
	s.lastChild = childOrderItemID
	if s.findErr != nil {
		return nil, s.findErr
	}
	if s.list == nil {
		return []assignment.ProductAssignment{}, nil
	}
	return s.list, nil
}

func (s *stubRepo) ListByParentID(_ context.Context, parentOrderItemID string, page assignment.ParentPage) ([]assignment.ProductAssignment, bool, error) {
	if s.findErr != nil {
		return nil, false, s.findErr
	}
	s.lastParent = parentOrderItemID
	rows := []assignment.ProductAssignment{}
	if s.byParent != nil && s.byParent[parentOrderItemID] != nil {
		rows = s.byParent[parentOrderItemID]
	}
	paged, hasMore := pageParentRows(rows, page)
	return paged, hasMore, nil
}

func pageParentRows(rows []assignment.ProductAssignment, page assignment.ParentPage) ([]assignment.ProductAssignment, bool) {
	status := page.Status
	if status == "" {
		status = string(assignment.StatusAssigned)
	}
	limit := page.Limit
	if limit <= 0 {
		limit = 100
	}
	offset := page.Offset
	if offset < 0 {
		offset = 0
	}
	filtered := make([]assignment.ProductAssignment, 0, len(rows))
	for _, row := range rows {
		if status != "all" && string(row.Status()) != status {
			continue
		}
		filtered = append(filtered, row)
	}
	if offset >= len(filtered) {
		return []assignment.ProductAssignment{}, false
	}
	rest := filtered[offset:]
	if len(rest) > limit {
		return rest[:limit], true
	}
	return rest, false
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

	revoked, idempotent, err := svc.Revoke(ctx, assigned.ID)
	if err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if idempotent || revoked.Status() != assignment.StatusRevoked || revoked.RevokedAt == nil {
		t.Fatalf("revoked = %+v idempotent=%v", revoked, idempotent)
	}

	if _, err := svc.Assign(ctx, revoked.ID, "parent-2"); !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("assign revoked = %v", err)
	}
	again, idempotent, err := svc.Revoke(ctx, revoked.ID)
	if err != nil || !idempotent || again.ID != revoked.ID || again.Status() != assignment.StatusRevoked {
		t.Fatalf("idempotent revoke = %+v %v idempotent=%v", again, err, idempotent)
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

	child := assignment.UnitID(id.New(), "child-var")
	u, err := svc.CreateUnassigned(ctx, child)
	if err != nil {
		t.Fatalf("CreateUnassigned: %v", err)
	}
	revoked, idempotent, err := svc.Revoke(ctx, u.ID)
	if err != nil || idempotent {
		t.Fatalf("Revoke unassigned: %v idempotent=%v", err, idempotent)
	}
	if revoked.Status() != assignment.StatusRevoked || revoked.ParentOrderItemID != "" {
		t.Fatalf("revoked = %+v", revoked)
	}
	current, err := svc.CurrentByChild(ctx, " "+child+" ")
	if err != nil || current != nil {
		t.Fatalf("CurrentByChild after unassigned revoke = %+v %v", current, err)
	}
	next, err := svc.CreateAssigned(ctx, assignment.UnitID(id.New(), "parent-var"), child)
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

	parent := assignment.UnitID(id.New(), "card-var")
	child := assignment.UnitID(id.New(), "ticket-var")
	created, err := svc.CreateAssigned(ctx, parent, child)
	if err != nil {
		t.Fatalf("CreateAssigned: %v", err)
	}
	got, err := svc.Get(ctx, created.ID)
	if err != nil || got.ID != created.ID {
		t.Fatalf("Get = %+v %v", got, err)
	}

	current, err := svc.CurrentByChild(ctx, " "+child+" ")
	if err != nil || current == nil || current.ID != created.ID {
		t.Fatalf("CurrentByChild = %+v %v", current, err)
	}
	missing, err := svc.CurrentByChild(ctx, assignment.UnitID(id.New(), "other-var"))
	if err != nil || missing != nil {
		t.Fatalf("CurrentByChild missing = %+v %v", missing, err)
	}
	if _, err := svc.CurrentByChild(ctx, "  "); !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("empty current = %v", err)
	}
	if _, err := svc.CurrentByChild(ctx, "foo/bar"); !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("non-uuid current = %v", err)
	}

	repo.list = nil
	rows, err := svc.ListByChild(ctx, child)
	if err != nil || rows == nil {
		t.Fatalf("ListByChild nil = %v %v", rows, err)
	}
	if _, err := svc.ListByChild(ctx, ""); !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("empty list = %v", err)
	}
	if _, err := svc.ListByChild(ctx, "foo/bar"); !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("non-uuid list = %v", err)
	}
	orderID, variantID, ok := assignment.SplitUnitID(child)
	if !ok {
		t.Fatalf("split child %q", child)
	}
	if _, err := svc.ListByChild(ctx, strings.ToUpper(orderID)+"/"+variantID); err != nil {
		t.Fatalf("uppercase list: %v", err)
	}
	if repo.lastChild != child {
		t.Fatalf("ListByChild canonical = %q, want %q", repo.lastChild, child)
	}

	revokedHistory, err := assignment.NewAssigned(id.New(), parent, assignment.UnitID(id.New(), "old-var"))
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if err := revokedHistory.Revoke(); err != nil {
		t.Fatalf("history revoke: %v", err)
	}
	repo.byParent = map[string][]assignment.ProductAssignment{
		parent: {*created, *revokedHistory},
	}
	parents, hasMore, err := svc.ListByParent(ctx, " "+parent+" ", app.ParentListQuery{})
	if err != nil || hasMore || len(parents) != 1 || parents[0].ID != created.ID {
		t.Fatalf("ListByParent default = %+v hasMore=%v %v", parents, hasMore, err)
	}
	if repo.lastParent != parent {
		t.Fatalf("parent passed to repo = %q", repo.lastParent)
	}
	all, hasMore, err := svc.ListByParent(ctx, parent, app.ParentListQuery{Status: app.ParentListStatusAll})
	if err != nil || hasMore || len(all) != 2 {
		t.Fatalf("ListByParent all = %+v hasMore=%v %v", all, hasMore, err)
	}
	onlyRevoked, hasMore, err := svc.ListByParent(ctx, parent, app.ParentListQuery{Status: app.ParentListStatusRevoked})
	if err != nil || hasMore || len(onlyRevoked) != 1 || onlyRevoked[0].Status() != assignment.StatusRevoked {
		t.Fatalf("ListByParent revoked = %+v hasMore=%v %v", onlyRevoked, hasMore, err)
	}
	first, hasMore, err := svc.ListByParent(ctx, parent, app.ParentListQuery{Status: app.ParentListStatusAll, Limit: 1})
	if err != nil || !hasMore || len(first) != 1 || first[0].ID != created.ID {
		t.Fatalf("ListByParent first page = %+v hasMore=%v %v", first, hasMore, err)
	}
	page, hasMore, err := svc.ListByParent(ctx, parent, app.ParentListQuery{Status: app.ParentListStatusAll, Limit: 1, Offset: 1})
	if err != nil || hasMore || len(page) != 1 || page[0].ID != revokedHistory.ID {
		t.Fatalf("ListByParent page = %+v hasMore=%v %v", page, hasMore, err)
	}
	if _, _, err := svc.ListByParent(ctx, "  ", app.ParentListQuery{}); !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("empty parent list = %v", err)
	}
	if _, _, err := svc.ListByParent(ctx, "foo/bar", app.ParentListQuery{}); !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("non-uuid parent list = %v", err)
	}
	if _, _, err := svc.ListByParent(ctx, parent, app.ParentListQuery{Status: "nope"}); !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("bad status = %v", err)
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
