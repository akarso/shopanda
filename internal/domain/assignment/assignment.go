package assignment

import (
	"errors"
	"strings"
	"time"
)

// Status is the ProductAssignment state machine.
// Transitions: unassigned → assigned → revoked, or unassigned → revoked
// when a never-assigned unit is released. Revoked is terminal on that
// row; a later binding is a new assigned record, not a resurrection.
type Status string

const (
	StatusUnassigned Status = "unassigned"
	StatusAssigned   Status = "assigned"
	StatusRevoked    Status = "revoked"
)

// IsValid reports whether s is a known assignment status.
func (s Status) IsValid() bool {
	switch s {
	case StatusUnassigned, StatusAssigned, StatusRevoked:
		return true
	}
	return false
}

// IsCurrent reports whether s is the live row for a child unit
// (unassigned or assigned). Revoked rows stay for history.
func (s Status) IsCurrent() bool {
	return s == StatusUnassigned || s == StatusAssigned
}

// IsTerminal reports whether s cannot transition further on this row.
func (s Status) IsTerminal() bool {
	return s == StatusRevoked
}

// ProductAssignment binds one purchased/issued child unit to one purchased
// parent unit (PR-1069). A unit id is opaque TEXT: typically an order line
// or a serialized digital unit. This is not catalog eligibility (PR-1067)
// and not the purchase-time AssignmentContext hint (PR-1068).
//
// An unassigned row is the default when a RequiresLinkedParent item is
// purchased without a target (PR-1070). Cart-time assignment inserts
// assigned directly. Revoke keeps the row. Reassignment inserts a new
// assigned row. This type is the state model only; when assignment may
// happen is PR-1070.
type ProductAssignment struct {
	ID string
	// ParentOrderItemID is the in-memory parent. NewAssigned and Assign
	// are the only writers. A loaded assigned or revoked row keeps the
	// persisted parent in storedParent; Update rejects a rewrite.
	ParentOrderItemID string
	ChildOrderItemID  string
	status            Status
	// storedStatus is the status last loaded or last successfully written.
	// Assign and Revoke change status and leave storedStatus in place so
	// Update can require the row to still be in that state.
	storedStatus Status
	// storedParent is the parent last loaded or last successfully written.
	// Assign changes ParentOrderItemID and leaves storedParent in place
	// so Update can allow that one write (unassigned → assigned).
	storedParent string
	CreatedAt    time.Time
	RevokedAt    *time.Time
}

// NewUnassigned builds a current assignment with no parent. IDs are
// trimmed; field validation runs before return.
func NewUnassigned(id, childOrderItemID string) (*ProductAssignment, error) {
	now := time.Now().UTC()
	a := &ProductAssignment{
		ID:               strings.TrimSpace(id),
		ChildOrderItemID: strings.TrimSpace(childOrderItemID),
		status:           StatusUnassigned,
		storedStatus:     StatusUnassigned,
		CreatedAt:        now,
	}
	if err := a.Validate(); err != nil {
		return nil, err
	}
	return a, nil
}

// NewAssigned builds a current assignment already bound to parent. IDs
// are trimmed; field validation runs before return. Cart-time purchase
// uses this constructor and skips unassigned.
func NewAssigned(id, parentOrderItemID, childOrderItemID string) (*ProductAssignment, error) {
	now := time.Now().UTC()
	parent := strings.TrimSpace(parentOrderItemID)
	a := &ProductAssignment{
		ID:                strings.TrimSpace(id),
		ParentOrderItemID: parent,
		ChildOrderItemID:  strings.TrimSpace(childOrderItemID),
		status:            StatusAssigned,
		storedStatus:      StatusAssigned,
		storedParent:      parent,
		CreatedAt:         now,
	}
	if err := a.Validate(); err != nil {
		return nil, err
	}
	return a, nil
}

// Status returns the current assignment status.
func (a *ProductAssignment) Status() Status {
	if a == nil {
		return ""
	}
	return a.status
}

// StoredStatus is the status last read from persistence, or the status of
// an assignment that has not been saved yet. Assign and Revoke do not
// change it.
func (a *ProductAssignment) StoredStatus() Status {
	if a == nil {
		return ""
	}
	return a.storedStatus
}

// AcceptStoredStatus records that persistence now has Status. Call it only
// after a successful status write.
func (a *ProductAssignment) AcceptStoredStatus() {
	if a == nil {
		return
	}
	a.storedStatus = a.status
	a.storedParent = a.ParentOrderItemID
}

// StoredParentOrderItemID is the parent last read from persistence, or
// the parent of an assignment that has not been saved yet. Assign does
// not change it.
func (a *ProductAssignment) StoredParentOrderItemID() string {
	if a == nil {
		return ""
	}
	return a.storedParent
}

// AssigningParent reports whether this is the unassigned → assigned
// transition that is allowed to write parent_order_item_id.
func (a *ProductAssignment) AssigningParent() bool {
	return a != nil && a.storedStatus == StatusUnassigned && a.status == StatusAssigned
}

// ParentChanged reports whether ParentOrderItemID differs from the value
// last loaded or written.
func (a *ProductAssignment) ParentChanged() bool {
	if a == nil {
		return false
	}
	return strings.TrimSpace(a.ParentOrderItemID) != strings.TrimSpace(a.storedParent)
}

// Validate checks field-level constraints and status invariants.
// It does not trim IDs. NewUnassigned, NewAssigned, Assign, and the
// application service trim before calling Validate.
func (a *ProductAssignment) Validate() error {
	if a == nil {
		return errors.New("product assignment must not be nil")
	}
	if strings.TrimSpace(a.ID) == "" {
		return errors.New("product assignment: id must not be empty")
	}
	childID := strings.TrimSpace(a.ChildOrderItemID)
	if childID == "" {
		return errors.New("product assignment: child order item id must not be empty")
	}
	if !a.status.IsValid() {
		return errors.New("product assignment: invalid status")
	}
	parentID := strings.TrimSpace(a.ParentOrderItemID)
	switch a.status {
	case StatusUnassigned:
		if parentID != "" {
			return errors.New("product assignment: unassigned parent order item id must be empty")
		}
		if a.RevokedAt != nil {
			return errors.New("product assignment: unassigned revoked_at must be empty")
		}
	case StatusAssigned:
		if parentID == "" {
			return errors.New("product assignment: assigned parent order item id must not be empty")
		}
		if childID == parentID {
			return errors.New("product assignment: child cannot be assigned to itself")
		}
		if a.RevokedAt != nil {
			return errors.New("product assignment: assigned revoked_at must be empty")
		}
	case StatusRevoked:
		if parentID != "" && childID == parentID {
			return errors.New("product assignment: child cannot be assigned to itself")
		}
		if a.RevokedAt == nil || a.RevokedAt.IsZero() {
			return errors.New("product assignment: revoked_at must not be zero")
		}
	}
	return nil
}

// Assign transitions unassigned → assigned and sets the parent unit.
// parentOrderItemID is trimmed. A revoked or already-assigned row is
// rejected; reassignment is a new record, not a second Assign.
func (a *ProductAssignment) Assign(parentOrderItemID string) error {
	if a == nil {
		return errors.New("product assignment must not be nil")
	}
	if a.status != StatusUnassigned {
		return errors.New("product assignment: can only assign an unassigned record")
	}
	a.ParentOrderItemID = strings.TrimSpace(parentOrderItemID)
	a.status = StatusAssigned
	if err := a.Validate(); err != nil {
		a.ParentOrderItemID = ""
		a.status = StatusUnassigned
		return err
	}
	return nil
}

// Revoke transitions assigned → revoked or unassigned → revoked and
// records revoked_at. A never-assigned unit keeps a null parent; do not
// invent a stand-in parent just to retire the row. An already-revoked
// row is rejected. The row is not deleted; a later binding inserts a
// new assigned record.
func (a *ProductAssignment) Revoke() error {
	if a == nil {
		return errors.New("product assignment must not be nil")
	}
	if a.status != StatusAssigned && a.status != StatusUnassigned {
		return errors.New("product assignment: can only revoke an unassigned or assigned record")
	}
	prev := a.status
	now := time.Now().UTC()
	a.status = StatusRevoked
	a.RevokedAt = &now
	if err := a.Validate(); err != nil {
		a.status = prev
		a.RevokedAt = nil
		return err
	}
	return nil
}

// SetStatusFromDB restores status when loading from persistence.
func (a *ProductAssignment) SetStatusFromDB(s string) error {
	if a == nil {
		return errors.New("product assignment must not be nil")
	}
	status := Status(s)
	if !status.IsValid() {
		return errors.New("product assignment: invalid status from db: " + s)
	}
	a.status = status
	a.storedStatus = status
	a.storedParent = a.ParentOrderItemID
	return nil
}
