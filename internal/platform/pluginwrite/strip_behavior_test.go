package pluginwrite_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/platform/pluginwrite"
	"github.com/akarso/shopanda/internal/platform/requestctx"
)

// Pins Strip itself: clears every granted code, keeps cancel + request id, and
// allows a fresh Grant on the stripped context.
func TestStrip_PreservesCancelAndRequestID_AllowsRegrant(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	parent = requestctx.WithRequestID(parent, "req-strip-1")
	parent = pluginwrite.Grant(parent, "acme", "acme.a", "acme.b")

	stripped := pluginwrite.Strip(parent)
	for _, code := range []string{"acme.a", "acme.b"} {
		if _, ok := pluginwrite.For(stripped, code); ok {
			t.Fatalf("For(%q) still trusted after Strip", code)
		}
	}
	if stripped.Err() != nil {
		t.Fatalf("stripped.Err() = %v, want nil before cancel", stripped.Err())
	}
	if got := requestctx.RequestID(stripped); got != "req-strip-1" {
		t.Fatalf("RequestID = %q, want req-strip-1", got)
	}

	cancel()
	if stripped.Err() == nil {
		t.Fatal("stripped ctx must observe parent cancel")
	}

	regranted := pluginwrite.Grant(stripped, "acme", "acme.c")
	if _, ok := pluginwrite.For(regranted, "acme.c"); !ok {
		t.Fatal("Grant on stripped ctx must stick")
	}
	if _, ok := pluginwrite.For(regranted, "acme.a"); ok {
		t.Fatal("re-grant must not revive stripped codes")
	}
}
