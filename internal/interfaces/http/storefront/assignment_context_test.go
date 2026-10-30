package storefront

import "testing"

func TestAssignmentDeferredFromForm(t *testing.T) {
	if !assignmentDeferredFromForm("true") || !assignmentDeferredFromForm("1") || !assignmentDeferredFromForm("on") {
		t.Fatal("true tokens")
	}
	if assignmentDeferredFromForm("") || assignmentDeferredFromForm("false") || assignmentDeferredFromForm("no") {
		t.Fatal("false tokens")
	}
}
