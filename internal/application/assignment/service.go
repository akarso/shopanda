package assignment

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	domain "github.com/akarso/shopanda/internal/domain/assignment"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"
)

// Service manages ProductAssignment rows. It is the state model only:
// create unassigned, create assigned, assign, revoke, and read. Catalog
// eligibility, ownership, and purchasability live on Flow (PR-1070).
// Unit ids are opaque; they are not resolved against order_items.
// ListByParent is the admin "this card's tickets" read (PR-1071).
type Service struct {
	assignments domain.Repository
}

// NewService creates a ProductAssignment service.
func NewService(assignments domain.Repository) *Service {
	if assignments == nil {
		panic("assignment.NewService: nil assignments repository")
	}
	return &Service{assignments: assignments}
}

// CreateUnassigned inserts a current unassigned row for childOrderItemID.
// A child that already has an unassigned or assigned row is a conflict.
func (s *Service) CreateUnassigned(ctx context.Context, childOrderItemID string) (*domain.ProductAssignment, error) {
	childOrderItemID = strings.TrimSpace(childOrderItemID)
	if err := s.requireNoCurrent(ctx, childOrderItemID); err != nil {
		return nil, err
	}
	a, err := domain.NewUnassigned(id.New(), childOrderItemID)
	if err != nil {
		return nil, apperror.Validation(err.Error())
	}
	if err := s.assignments.Insert(ctx, a); err != nil {
		return nil, mapWriteError("create unassigned", err)
	}
	return a, nil
}

// CreateAssigned inserts a current assigned row. Cart-time purchase uses
// this when no unassigned row exists. A child that already has a current
// row is a conflict; transition that row with Assign instead.
func (s *Service) CreateAssigned(ctx context.Context, parentOrderItemID, childOrderItemID string) (*domain.ProductAssignment, error) {
	return s.insertAssigned(ctx, parentOrderItemID, childOrderItemID, "")
}

// CreateAssignedForced inserts a current assigned row and records the
// operator override. reason is required.
func (s *Service) CreateAssignedForced(ctx context.Context, parentOrderItemID, childOrderItemID, reason string) (*domain.ProductAssignment, error) {
	return s.insertAssigned(ctx, parentOrderItemID, childOrderItemID, reason)
}

func (s *Service) insertAssigned(ctx context.Context, parentOrderItemID, childOrderItemID, forcedReason string) (*domain.ProductAssignment, error) {
	parentOrderItemID = strings.TrimSpace(parentOrderItemID)
	childOrderItemID = strings.TrimSpace(childOrderItemID)
	if err := s.requireNoCurrent(ctx, childOrderItemID); err != nil {
		return nil, err
	}
	a, err := domain.NewAssigned(id.New(), parentOrderItemID, childOrderItemID)
	if err != nil {
		return nil, apperror.Validation(err.Error())
	}
	if err := applyForced(a, forcedReason); err != nil {
		return nil, err
	}
	if err := s.assignments.Insert(ctx, a); err != nil {
		return nil, mapWriteError("create assigned", err)
	}
	return a, nil
}

// Assign transitions the named unassigned row to assigned.
func (s *Service) Assign(ctx context.Context, id, parentOrderItemID string) (*domain.ProductAssignment, error) {
	return s.assign(ctx, id, parentOrderItemID, "")
}

// AssignForced transitions the named unassigned row to assigned and
// records the operator override. reason is required.
func (s *Service) AssignForced(ctx context.Context, id, parentOrderItemID, reason string) (*domain.ProductAssignment, error) {
	return s.assign(ctx, id, parentOrderItemID, reason)
}

func (s *Service) assign(ctx context.Context, id, parentOrderItemID, forcedReason string) (*domain.ProductAssignment, error) {
	parentOrderItemID = strings.TrimSpace(parentOrderItemID)
	a, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := a.Assign(parentOrderItemID); err != nil {
		return nil, apperror.Validation(err.Error())
	}
	if err := applyForced(a, forcedReason); err != nil {
		return nil, err
	}
	if err := s.assignments.Update(ctx, a); err != nil {
		return nil, mapWriteError("assign", err)
	}
	return a, nil
}

// Revoke transitions the named unassigned or assigned row to revoked.
// An already-revoked row is returned unchanged. idempotent is true when
// the row was already revoked and this call did not write.
func (s *Service) Revoke(ctx context.Context, id string) (*domain.ProductAssignment, bool, error) {
	a, err := s.Get(ctx, id)
	if err != nil {
		return nil, false, err
	}
	if a.Status() == domain.StatusRevoked {
		return a, true, nil
	}
	if err := a.Revoke(); err != nil {
		return nil, false, apperror.Validation(err.Error())
	}
	if err := s.assignments.Update(ctx, a); err != nil {
		return nil, false, mapWriteError("revoke", err)
	}
	return a, false, nil
}

// Get returns the assignment by id.
func (s *Service) Get(ctx context.Context, assignmentID string) (*domain.ProductAssignment, error) {
	assignmentID = strings.TrimSpace(assignmentID)
	if assignmentID == "" {
		return nil, apperror.Validation("product assignment id must not be empty")
	}
	if !id.IsUUID(assignmentID) {
		return nil, apperror.Validation("invalid assignment id")
	}
	a, err := s.assignments.FindByID(ctx, assignmentID)
	if err != nil {
		if apperror.Is(err, apperror.CodeValidation) || apperror.Is(err, apperror.CodeNotFound) {
			return nil, err
		}
		return nil, fmt.Errorf("assignment: get: %w", err)
	}
	if a == nil {
		return nil, apperror.NotFound("product assignment not found")
	}
	return a, nil
}

// CurrentByChild returns the live (unassigned or assigned) row for the
// child, or (nil, nil) when only revoked history exists or none exists.
// childOrderItemID is canonicalized to orderID/variantID before the read.
func (s *Service) CurrentByChild(ctx context.Context, childOrderItemID string) (*domain.ProductAssignment, error) {
	childOrderItemID, err := canonicalUnitID(childOrderItemID, "child order item id must not be empty", "child order item id is invalid")
	if err != nil {
		return nil, err
	}
	a, err := s.assignments.FindCurrentByChildID(ctx, childOrderItemID)
	if err != nil {
		return nil, fmt.Errorf("assignment: current by child: %w", err)
	}
	return a, nil
}

// ListByChild returns assignment history for the child, newest first.
// childOrderItemID is canonicalized the same way as CurrentByChild.
func (s *Service) ListByChild(ctx context.Context, childOrderItemID string) ([]domain.ProductAssignment, error) {
	childOrderItemID, err := canonicalUnitID(childOrderItemID, "child order item id must not be empty", "child order item id is invalid")
	if err != nil {
		return nil, err
	}
	rows, err := s.assignments.ListByChildID(ctx, childOrderItemID)
	if err != nil {
		return nil, fmt.Errorf("assignment: list by child: %w", err)
	}
	if rows == nil {
		rows = []domain.ProductAssignment{}
	}
	return rows, nil
}

// ParentListQuery is the admin card-view page for one parent unit.
// Status empty means assigned (the live card). "revoked" and "all" are
// the other accepted values. Limit defaults to ParentListDefaultLimit
// and is capped at ParentListMaxLimit. Offset skips that many matches
// after the status filter. Status, limit, and offset are applied in
// ListByParentID. HasMore is true when another row exists past the page.
type ParentListQuery struct {
	Status string
	Limit  int
	Offset int
}

const (
	ParentListStatusAssigned = "assigned"
	ParentListStatusRevoked  = "revoked"
	ParentListStatusAll      = "all"
	ParentListDefaultLimit   = 100
	ParentListMaxLimit       = 100
)

// ListByParent returns one page of assignments bound to
// parentOrderItemID, newest first. Unassigned rows have no parent and
// are not included. parentOrderItemID is canonicalized before the read.
// The default status is assigned and the default limit is 100. hasMore
// is true when a further row exists past this page.
func (s *Service) ListByParent(ctx context.Context, parentOrderItemID string, q ParentListQuery) ([]domain.ProductAssignment, bool, error) {
	parentOrderItemID, err := canonicalUnitID(parentOrderItemID, "parent order item id must not be empty", "parent order item id is invalid")
	if err != nil {
		return nil, false, err
	}
	status, err := parentListStatus(q.Status)
	if err != nil {
		return nil, false, err
	}
	limit := q.Limit
	if limit <= 0 {
		limit = ParentListDefaultLimit
	}
	if limit > ParentListMaxLimit {
		limit = ParentListMaxLimit
	}
	if q.Offset < 0 {
		return nil, false, apperror.Validation("offset must be a non-negative integer")
	}
	rows, hasMore, err := s.assignments.ListByParentID(ctx, parentOrderItemID, domain.ParentPage{
		Status: status,
		Limit:  limit,
		Offset: q.Offset,
	})
	if err != nil {
		return nil, false, fmt.Errorf("assignment: list by parent: %w", err)
	}
	if rows == nil {
		rows = []domain.ProductAssignment{}
	}
	return rows, hasMore, nil
}

func parentListStatus(raw string) (string, error) {
	switch strings.TrimSpace(raw) {
	case "", ParentListStatusAssigned:
		return ParentListStatusAssigned, nil
	case ParentListStatusRevoked, ParentListStatusAll:
		return strings.TrimSpace(raw), nil
	default:
		return "", apperror.Validation("status must be assigned, revoked, or all")
	}
}

func canonicalUnitID(raw, emptyMsg, invalidMsg string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", apperror.Validation(emptyMsg)
	}
	unit, ok := domain.CanonicalUnitID(raw)
	if !ok {
		return "", apperror.Validation(invalidMsg)
	}
	return unit, nil
}

func applyForced(a *domain.ProductAssignment, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil
	}
	if err := a.SetForced(reason, time.Now().UTC()); err != nil {
		return apperror.Validation(err.Error())
	}
	return nil
}

func (s *Service) requireNoCurrent(ctx context.Context, childOrderItemID string) error {
	if childOrderItemID == "" {
		return apperror.Validation("child order item id must not be empty")
	}
	current, err := s.assignments.FindCurrentByChildID(ctx, childOrderItemID)
	if err != nil {
		return fmt.Errorf("assignment: find current: %w", err)
	}
	if current != nil {
		return apperror.Conflict("child already has an active assignment")
	}
	return nil
}

func mapWriteError(op string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, domain.ErrStatusConflict) {
		return apperror.Conflict("product assignment status conflict")
	}
	if apperror.Is(err, apperror.CodeConflict) || apperror.Is(err, apperror.CodeValidation) || apperror.Is(err, apperror.CodeNotFound) {
		return err
	}
	return fmt.Errorf("assignment: %s: %w", op, err)
}
