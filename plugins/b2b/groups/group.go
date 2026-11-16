package groups

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

var (
	codeRegex = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,48}[a-z0-9]$`)
	uuidRegex = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
)

// Group segments customers for B2B pricing and promotions.
type Group struct {
	ID          string
	Code        string
	Name        string
	Description string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// NewGroup creates a group with validation.
func NewGroup(groupID, code, name, description string) (Group, error) {
	groupID = strings.TrimSpace(groupID)
	code = normalizeCode(code)
	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)

	if groupID == "" {
		return Group{}, errors.New("customer group: id must not be empty")
	}
	if !uuidRegex.MatchString(groupID) {
		return Group{}, errors.New("customer group: id must be a valid UUID")
	}
	if err := validateCode(code); err != nil {
		return Group{}, err
	}
	if name == "" {
		return Group{}, errors.New("customer group: name must not be empty")
	}

	now := time.Now().UTC()
	return Group{
		ID:          groupID,
		Code:        code,
		Name:        name,
		Description: description,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, nil
}

// Update mutates editable fields.
func (g *Group) Update(name, description string) error {
	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)
	if name == "" {
		return errors.New("customer group: name must not be empty")
	}
	g.Name = name
	g.Description = description
	g.UpdatedAt = time.Now().UTC()
	return nil
}

func normalizeCode(code string) string {
	return strings.ToLower(strings.TrimSpace(code))
}

func validateCode(code string) error {
	if code == "" {
		return errors.New("customer group: code must not be empty")
	}
	if !codeRegex.MatchString(code) {
		return fmt.Errorf("customer group: invalid code format: %q", code)
	}
	return nil
}

// Repository persists customer groups and membership.
type Repository interface {
	// List returns groups ordered by name.
	List(ctx context.Context, offset, limit int) ([]Group, error)

	// FindByID returns a group by ID. (nil, nil) when not found.
	FindByID(ctx context.Context, id string) (*Group, error)

	// FindByCode returns a group by code. (nil, nil) when not found.
	FindByCode(ctx context.Context, code string) (*Group, error)

	// Save inserts or updates a group.
	Save(ctx context.Context, group *Group) error

	// Delete removes a group by ID.
	Delete(ctx context.Context, id string) error

	// AssignCustomer sets the customer's group membership (replaces any existing).
	AssignCustomer(ctx context.Context, customerID, groupID string) error

	// RemoveCustomer clears group membership for a customer.
	RemoveCustomer(ctx context.Context, customerID string) error

	// FindGroupByCustomerID returns the group for a customer. (nil, nil) when unassigned.
	FindGroupByCustomerID(ctx context.Context, customerID string) (*Group, error)
}
