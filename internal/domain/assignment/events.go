package assignment

// Domain event names for ProductAssignment observability.
const (
	// EventAssignmentRevoked is published when a live row becomes revoked
	// (including never-assigned unassigned → revoked).
	EventAssignmentRevoked = "assignment.revoked"
)

// AssignmentRevokedData is the payload for EventAssignmentRevoked.
// Reason is a stable token such as return_approved or return_refunded
// when returns triggered the revoke; empty for other callers.
type AssignmentRevokedData struct {
	AssignmentID      string
	ChildOrderItemID  string
	ParentOrderItemID string
	Reason            string
}
