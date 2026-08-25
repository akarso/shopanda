package shared

import (
	"net/http/httptest"
	"testing"
)

// White-box (package shared, not shared_test): wrapStatus and statusWriter
// are unexported, and their sharing behavior is a pointer-identity claim
// that's awkward to observe correctly through the public HTTP-handler
// surface alone — an allocation-count comparison via a full middleware
// chain is dominated by unrelated costs (logging, span creation, request
// parsing) and doesn't reliably isolate this one mechanism.

// TestWrapStatus_ReusesExistingWrapper pins the fix for Metrics, Tracing,
// and Logging middleware each allocating their own *statusWriter around
// the same request when stacked together (the normal case — see
// wire_routes.go) instead of sharing one: calling wrapStatus on a writer
// that's already a *statusWriter (as it would be, from an outer
// middleware layer) must return that exact same instance, not allocate a
// new wrapper around it.
func TestWrapStatus_ReusesExistingWrapper(t *testing.T) {
	rec := httptest.NewRecorder()
	outer := wrapStatus(rec)
	inner := wrapStatus(outer)

	if inner != outer {
		t.Error("wrapStatus wrapped an existing *statusWriter in a new one instead of reusing it")
	}
}

// TestWrapStatus_WrapsWhenNoneExists confirms the other half: given a
// plain ResponseWriter (the outermost middleware's case — nothing to
// reuse yet), wrapStatus still wraps it correctly.
func TestWrapStatus_WrapsWhenNoneExists(t *testing.T) {
	rec := httptest.NewRecorder()
	sw := wrapStatus(rec)

	if sw.ResponseWriter != rec {
		t.Error("wrapStatus did not wrap the given ResponseWriter")
	}
	if sw.status != 200 {
		t.Errorf("status = %d, want the http.StatusOK default before any WriteHeader call", sw.status)
	}
}
