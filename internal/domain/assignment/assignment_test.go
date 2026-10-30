package assignment_test

import (
	"strings"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/assignment"
	"github.com/akarso/shopanda/internal/platform/id"
)

func TestNewUnassigned_TrimsIDs(t *testing.T) {
	a, err := assignment.NewUnassigned(" "+id.New()+" ", " child-1 ")
	if err != nil {
		t.Fatalf("NewUnassigned: %v", err)
	}
	if strings.Contains(a.ID, " ") || a.ChildOrderItemID != "child-1" {
		t.Errorf("ids not trimmed: %+v", a)
	}
	if a.Status() != assignment.StatusUnassigned {
		t.Errorf("status = %q", a.Status())
	}
	if a.ParentOrderItemID != "" {
		t.Errorf("parent = %q, want empty", a.ParentOrderItemID)
	}
	if a.RevokedAt != nil {
		t.Errorf("revoked_at = %v, want nil", a.RevokedAt)
	}
	if a.StoredStatus() != assignment.StatusUnassigned {
		t.Errorf("stored status = %q", a.StoredStatus())
	}
}

func TestNewAssigned_TrimsIDs(t *testing.T) {
	a, err := assignment.NewAssigned(" "+id.New()+" ", " parent-1 ", " child-1 ")
	if err != nil {
		t.Fatalf("NewAssigned: %v", err)
	}
	if a.ParentOrderItemID != "parent-1" || a.ChildOrderItemID != "child-1" {
		t.Errorf("ids not trimmed: %+v", a)
	}
	if a.Status() != assignment.StatusAssigned {
		t.Errorf("status = %q", a.Status())
	}
}

func TestNewUnassigned_RejectsEmpty(t *testing.T) {
	if _, err := assignment.NewUnassigned("", "child-1"); err == nil || !strings.Contains(err.Error(), "id must not be empty") {
		t.Fatalf("empty id = %v", err)
	}
	if _, err := assignment.NewUnassigned(id.New(), "  "); err == nil || !strings.Contains(err.Error(), "child order item id") {
		t.Fatalf("empty child = %v", err)
	}
}

func TestNewAssigned_RejectsEmptyAndSelf(t *testing.T) {
	if _, err := assignment.NewAssigned(id.New(), "", "child-1"); err == nil || !strings.Contains(err.Error(), "parent order item id") {
		t.Fatalf("empty parent = %v", err)
	}
	if _, err := assignment.NewAssigned(id.New(), "same", "same"); err == nil || !strings.Contains(err.Error(), "itself") {
		t.Fatalf("self = %v", err)
	}
	if _, err := assignment.NewAssigned(id.New(), " same ", "same"); err == nil || !strings.Contains(err.Error(), "itself") {
		t.Fatalf("padded self = %v", err)
	}
}

func TestProductAssignment_Validate(t *testing.T) {
	validUnassigned := mustUnassigned(t, "child-1")
	validAssigned := mustAssigned(t, "parent-1", "child-1")

	revokedAt := time.Now().UTC()
	validRevoked := &assignment.ProductAssignment{
		ID:                id.New(),
		ParentOrderItemID: "parent-1",
		ChildOrderItemID:  "child-1",
		CreatedAt:         time.Now().UTC(),
		RevokedAt:         &revokedAt,
	}
	if err := validRevoked.SetStatusFromDB(string(assignment.StatusRevoked)); err != nil {
		t.Fatalf("SetStatusFromDB: %v", err)
	}
	validRevokedNeverAssigned := &assignment.ProductAssignment{
		ID:               id.New(),
		ChildOrderItemID: "child-1",
		CreatedAt:        time.Now().UTC(),
		RevokedAt:        &revokedAt,
	}
	if err := validRevokedNeverAssigned.SetStatusFromDB(string(assignment.StatusRevoked)); err != nil {
		t.Fatalf("SetStatusFromDB never assigned: %v", err)
	}

	tests := []struct {
		name    string
		a       *assignment.ProductAssignment
		wantErr string
	}{
		{"valid unassigned", validUnassigned, ""},
		{"valid assigned", validAssigned, ""},
		{"valid revoked", validRevoked, ""},
		{"valid revoked never assigned", validRevokedNeverAssigned, ""},
		{"nil", nil, "must not be nil"},
		{"empty id", &assignment.ProductAssignment{ChildOrderItemID: "c"}, "id must not be empty"},
		{"empty child", func() *assignment.ProductAssignment {
			a := mustUnassigned(t, "c")
			a.ChildOrderItemID = ""
			return a
		}(), "child order item id"},
		{"whitespace child", func() *assignment.ProductAssignment {
			a := mustUnassigned(t, "c")
			a.ChildOrderItemID = "  "
			return a
		}(), "child order item id"},
		{"unassigned with parent", func() *assignment.ProductAssignment {
			a := mustUnassigned(t, "c")
			a.ParentOrderItemID = "p"
			return a
		}(), "unassigned parent"},
		{"assigned without parent", func() *assignment.ProductAssignment {
			a := mustAssigned(t, "p", "c")
			a.ParentOrderItemID = ""
			return a
		}(), "assigned parent"},
		{"assigned self", func() *assignment.ProductAssignment {
			a := mustAssigned(t, "p", "c")
			a.ParentOrderItemID = "same"
			a.ChildOrderItemID = "same"
			return a
		}(), "itself"},
		{"assigned with revoked_at", func() *assignment.ProductAssignment {
			a := mustAssigned(t, "p", "c")
			t0 := time.Now().UTC()
			a.RevokedAt = &t0
			return a
		}(), "assigned revoked_at"},
		{"revoked without revoked_at", func() *assignment.ProductAssignment {
			a := mustAssigned(t, "p", "c")
			if err := a.SetStatusFromDB(string(assignment.StatusRevoked)); err != nil {
				t.Fatalf("SetStatusFromDB: %v", err)
			}
			return a
		}(), "revoked_at"},
		{"padded ids are not trimmed", func() *assignment.ProductAssignment {
			a := mustAssigned(t, "p", "c")
			a.ParentOrderItemID = "  p  "
			a.ChildOrderItemID = "  c  "
			return a
		}(), ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var beforeParent, beforeChild string
			if tc.a != nil {
				beforeParent, beforeChild = tc.a.ParentOrderItemID, tc.a.ChildOrderItemID
			}
			err := tc.a.Validate()
			if tc.a != nil && (tc.a.ParentOrderItemID != beforeParent || tc.a.ChildOrderItemID != beforeChild) {
				t.Fatalf("Validate mutated ids: %+v", tc.a)
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

func TestAssign_UnassignedToAssigned(t *testing.T) {
	a := mustUnassigned(t, "child-1")
	if err := a.Assign(" parent-1 "); err != nil {
		t.Fatalf("Assign: %v", err)
	}
	if a.Status() != assignment.StatusAssigned {
		t.Fatalf("status = %q", a.Status())
	}
	if a.ParentOrderItemID != "parent-1" {
		t.Fatalf("parent = %q", a.ParentOrderItemID)
	}
	if a.StoredStatus() != assignment.StatusUnassigned {
		t.Fatalf("stored status changed: %q", a.StoredStatus())
	}
	if a.StoredParentOrderItemID() != "" {
		t.Fatalf("stored parent changed: %q", a.StoredParentOrderItemID())
	}
	if !a.AssigningParent() || !a.ParentChanged() {
		t.Fatal("Assign should be the one allowed parent write")
	}
	a.AcceptStoredStatus()
	if a.StoredStatus() != assignment.StatusAssigned {
		t.Fatalf("stored status after accept = %q", a.StoredStatus())
	}
	if a.StoredParentOrderItemID() != "parent-1" {
		t.Fatalf("stored parent after accept = %q", a.StoredParentOrderItemID())
	}
	if a.AssigningParent() || a.ParentChanged() {
		t.Fatal("accepted assign must not look like a parent rewrite")
	}
}

func TestAssign_RejectsNonUnassignedAndEmptyParent(t *testing.T) {
	assigned := mustAssigned(t, "parent-1", "child-1")
	if err := assigned.Assign("parent-2"); err == nil || !strings.Contains(err.Error(), "unassigned") {
		t.Fatalf("assign assigned = %v", err)
	}
	if assigned.ParentOrderItemID != "parent-1" {
		t.Fatalf("parent mutated: %q", assigned.ParentOrderItemID)
	}

	unassigned := mustUnassigned(t, "child-1")
	if err := unassigned.Assign("  "); err == nil || !strings.Contains(err.Error(), "parent") {
		t.Fatalf("empty parent = %v", err)
	}
	if unassigned.Status() != assignment.StatusUnassigned || unassigned.ParentOrderItemID != "" {
		t.Fatalf("failed Assign mutated row: status=%q parent=%q", unassigned.Status(), unassigned.ParentOrderItemID)
	}

	self := mustUnassigned(t, "same")
	if err := self.Assign("same"); err == nil || !strings.Contains(err.Error(), "itself") {
		t.Fatalf("self = %v", err)
	}
}

func TestRevoke_AssignedToRevoked(t *testing.T) {
	a := mustAssigned(t, "parent-1", "child-1")
	before := time.Now().UTC().Add(-time.Second)
	if err := a.Revoke(); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if a.Status() != assignment.StatusRevoked {
		t.Fatalf("status = %q", a.Status())
	}
	if a.RevokedAt == nil || a.RevokedAt.Before(before) {
		t.Fatalf("revoked_at = %v", a.RevokedAt)
	}
	if a.ParentOrderItemID != "parent-1" {
		t.Fatalf("parent cleared: %q", a.ParentOrderItemID)
	}
	if a.StoredStatus() != assignment.StatusAssigned {
		t.Fatalf("stored status changed: %q", a.StoredStatus())
	}
}

func TestRevoke_UnassignedToRevoked(t *testing.T) {
	a := mustUnassigned(t, "child-1")
	if err := a.Revoke(); err != nil {
		t.Fatalf("Revoke unassigned: %v", err)
	}
	if a.Status() != assignment.StatusRevoked {
		t.Fatalf("status = %q", a.Status())
	}
	if a.ParentOrderItemID != "" {
		t.Fatalf("parent = %q, want empty", a.ParentOrderItemID)
	}
	if a.RevokedAt == nil {
		t.Fatal("revoked_at")
	}
	if a.StoredStatus() != assignment.StatusUnassigned {
		t.Fatalf("stored status changed: %q", a.StoredStatus())
	}
}

func TestRevoke_RejectsRevokedAndDoesNotResurrect(t *testing.T) {
	a := mustAssigned(t, "parent-1", "child-1")
	if err := a.Revoke(); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if err := a.Assign("parent-2"); err == nil || !strings.Contains(err.Error(), "unassigned") {
		t.Fatalf("assign revoked = %v", err)
	}
	if err := a.Revoke(); err == nil || !strings.Contains(err.Error(), "unassigned or assigned") {
		t.Fatalf("revoke revoked = %v", err)
	}
	if a.Status() != assignment.StatusRevoked {
		t.Fatalf("status = %q", a.Status())
	}
}

func TestParentChanged_OnAssignedAndRevoked(t *testing.T) {
	assigned := mustAssigned(t, "parent-1", "child-1")
	if assigned.AssigningParent() || assigned.ParentChanged() {
		t.Fatal("NewAssigned must snapshot the parent")
	}
	assigned.ParentOrderItemID = "parent-2"
	if !assigned.ParentChanged() || assigned.AssigningParent() {
		t.Fatal("rewritten assigned parent must not be a legal Assign write")
	}

	revoked := mustAssigned(t, "parent-1", "child-1")
	if err := revoked.Revoke(); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	revoked.AcceptStoredStatus()
	revoked.ParentOrderItemID = "parent-2"
	if !revoked.ParentChanged() || revoked.AssigningParent() {
		t.Fatal("rewritten revoked parent must not be a legal Assign write")
	}
}

func TestStatusHelpers(t *testing.T) {
	if !assignment.StatusUnassigned.IsValid() || !assignment.StatusAssigned.IsValid() || !assignment.StatusRevoked.IsValid() {
		t.Fatal("known statuses should be valid")
	}
	if assignment.Status("nope").IsValid() {
		t.Fatal("unknown status")
	}
	if !assignment.StatusUnassigned.IsCurrent() || !assignment.StatusAssigned.IsCurrent() || assignment.StatusRevoked.IsCurrent() {
		t.Fatal("IsCurrent")
	}
	if assignment.StatusUnassigned.IsTerminal() || assignment.StatusAssigned.IsTerminal() || !assignment.StatusRevoked.IsTerminal() {
		t.Fatal("IsTerminal")
	}
}

func TestSetStatusFromDB(t *testing.T) {
	a := mustUnassigned(t, "child-1")
	if err := a.SetStatusFromDB("assigned"); err != nil {
		t.Fatalf("SetStatusFromDB: %v", err)
	}
	if a.Status() != assignment.StatusAssigned || a.StoredStatus() != assignment.StatusAssigned {
		t.Fatalf("status=%q stored=%q", a.Status(), a.StoredStatus())
	}
	if err := a.SetStatusFromDB("nope"); err == nil || !strings.Contains(err.Error(), "invalid status") {
		t.Fatalf("invalid = %v", err)
	}
}

func mustUnassigned(t *testing.T, child string) *assignment.ProductAssignment {
	t.Helper()
	a, err := assignment.NewUnassigned(id.New(), child)
	if err != nil {
		t.Fatalf("NewUnassigned: %v", err)
	}
	return a
}

func mustAssigned(t *testing.T, parent, child string) *assignment.ProductAssignment {
	t.Helper()
	a, err := assignment.NewAssigned(id.New(), parent, child)
	if err != nil {
		t.Fatalf("NewAssigned: %v", err)
	}
	return a
}

func TestUnitID_RoundTrip(t *testing.T) {
	orderID := id.New()
	variantID := id.New()
	got := assignment.UnitID(" "+orderID+" ", " "+variantID+" ")
	if got != orderID+"/"+variantID {
		t.Fatalf("UnitID = %q", got)
	}
	parsedOrder, parsedVariant, ok := assignment.SplitUnitID(" " + got + " ")
	if !ok || parsedOrder != orderID || parsedVariant != variantID {
		t.Fatalf("SplitUnitID = %q %q ok=%v", parsedOrder, parsedVariant, ok)
	}
}

func TestUnitID_EmptyParts(t *testing.T) {
	if assignment.UnitID("", "v") != "" || assignment.UnitID("o", "") != "" {
		t.Fatal("empty part must yield empty unit id")
	}
	if _, _, ok := assignment.SplitUnitID(""); ok {
		t.Fatal("empty unit must not parse")
	}
	if _, _, ok := assignment.SplitUnitID("no-separator"); ok {
		t.Fatal("missing separator must not parse")
	}
	if _, _, ok := assignment.SplitUnitID("/variant"); ok {
		t.Fatal("empty order must not parse")
	}
}

func TestCanonicalUnitID_RebuildsPaddedParts(t *testing.T) {
	orderID := id.New()
	variantID := id.New()
	got, ok := assignment.CanonicalUnitID(" " + orderID + " / " + variantID + " ")
	if !ok || got != orderID+"/"+variantID {
		t.Fatalf("CanonicalUnitID = %q ok=%v, want minted form", got, ok)
	}
	if _, ok := assignment.CanonicalUnitID("no-separator"); ok {
		t.Fatal("non-unit id must not canonicalize")
	}
}

func TestCanonicalUnitID_LowercasesOrderUUIDOnly(t *testing.T) {
	orderID := id.New()
	variantID := "Ticket-VAR"
	got, ok := assignment.CanonicalUnitID(" " + strings.ToUpper(orderID) + " / " + variantID + " ")
	if !ok || got != orderID+"/"+variantID {
		t.Fatalf("CanonicalUnitID = %q ok=%v, want lowercase order and original variant", got, ok)
	}
}

func TestSetForced_AssignedOnly(t *testing.T) {
	a := mustAssigned(t, "parent", "child")
	if err := a.SetForced("  correct a bind  ", time.Now().UTC()); err != nil {
		t.Fatalf("SetForced: %v", err)
	}
	if !a.Forced() || a.ForcedReason != "correct a bind" || a.ForcedAt == nil {
		t.Fatalf("forced = %+v", a)
	}
	unassigned := mustUnassigned(t, "child-2")
	if err := unassigned.SetForced("nope", time.Now().UTC()); err == nil {
		t.Fatal("unassigned force must fail")
	}
	exact := strings.Repeat("a", assignment.MaxForcedReasonLen)
	if err := a.SetForced(exact, time.Now().UTC()); err != nil {
		t.Fatalf("SetForced %d chars: %v", assignment.MaxForcedReasonLen, err)
	}
	if err := a.SetForced(exact+"b", time.Now().UTC()); err == nil || !strings.Contains(err.Error(), assignment.ForcedReasonTooLongMessage) {
		t.Fatalf("SetForced over cap = %v", err)
	}
}

func TestValidate_AllowsOversizedForcedReason(t *testing.T) {
	a := mustAssigned(t, "parent", "child")
	at := time.Now().UTC()
	a.ForcedAt = &at
	a.ForcedReason = strings.Repeat("é", assignment.MaxForcedReasonLen+1)
	if err := a.Validate(); err != nil {
		t.Fatalf("Validate must load an over-long stored reason: %v", err)
	}
}

func TestCanonicalUnitID_RejectsNonUUIDOrder(t *testing.T) {
	if _, ok := assignment.CanonicalUnitID("foo/bar"); ok {
		t.Fatal("non-uuid order part must not canonicalize")
	}
	variant := "bar"
	if _, ok := assignment.CanonicalUnitID(id.New() + "/" + variant); !ok {
		t.Fatal("uuid order with a non-uuid variant must canonicalize")
	}
}
