package assignment

import (
	"context"
	"errors"
	"fmt"
	"strings"

	domain "github.com/akarso/shopanda/internal/domain/assignment"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"
)

// Service manages ProductAssignment rows. It is the state model only:
// create unassigned, create assigned, assign, revoke, and read. It does
// not check catalog eligibility, ownership, or purchasability (PR-1070).
// Unit ids are opaque; they are not resolved against order_items.
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
	parentOrderItemID = strings.TrimSpace(parentOrderItemID)
	childOrderItemID = strings.TrimSpace(childOrderItemID)
	if err := s.requireNoCurrent(ctx, childOrderItemID); err != nil {
		return nil, err
	}
	a, err := domain.NewAssigned(id.New(), parentOrderItemID, childOrderItemID)
	if err != nil {
		return nil, apperror.Validation(err.Error())
	}
	if err := s.assignments.Insert(ctx, a); err != nil {
		return nil, mapWriteError("create assigned", err)
	}
	return a, nil
}

// Assign transitions the named unassigned row to assigned.
func (s *Service) Assign(ctx context.Context, id, parentOrderItemID string) (*domain.ProductAssignment, error) {
	parentOrderItemID = strings.TrimSpace(parentOrderItemID)
	a, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := a.Assign(parentOrderItemID); err != nil {
		return nil, apperror.Validation(err.Error())
	}
	if err := s.assignments.Update(ctx, a); err != nil {
		return nil, mapWriteError("assign", err)
	}
	return a, nil
}

// Revoke transitions the named unassigned or assigned row to revoked.
func (s *Service) Revoke(ctx context.Context, id string) (*domain.ProductAssignment, error) {
	a, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := a.Revoke(); err != nil {
		return nil, apperror.Validation(err.Error())
	}
	if err := s.assignments.Update(ctx, a); err != nil {
		return nil, mapWriteError("revoke", err)
	}
	return a, nil
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
func (s *Service) CurrentByChild(ctx context.Context, childOrderItemID string) (*domain.ProductAssignment, error) {
	childOrderItemID = strings.TrimSpace(childOrderItemID)
	if childOrderItemID == "" {
		return nil, apperror.Validation("child order item id must not be empty")
	}
	a, err := s.assignments.FindCurrentByChildID(ctx, childOrderItemID)
	if err != nil {
		return nil, fmt.Errorf("assignment: current by child: %w", err)
	}
	return a, nil
}

// ListByChild returns assignment history for the child, newest first.
func (s *Service) ListByChild(ctx context.Context, childOrderItemID string) ([]domain.ProductAssignment, error) {
	childOrderItemID = strings.TrimSpace(childOrderItemID)
	if childOrderItemID == "" {
		return nil, apperror.Validation("child order item id must not be empty")
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
