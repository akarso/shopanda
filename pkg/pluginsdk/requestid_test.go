package pluginsdk_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/platform/requestctx"
	"github.com/akarso/shopanda/pkg/pluginsdk"
)

func TestRequestIDFromContext_Empty(t *testing.T) {
	if got := pluginsdk.RequestIDFromContext(context.Background()); got != "" {
		t.Fatalf("empty ctx = %q, want empty", got)
	}
}

func TestRequestIDFromContext_RoundTrip(t *testing.T) {
	ctx := requestctx.WithRequestID(context.Background(), "req-from-http")
	if got := pluginsdk.RequestIDFromContext(ctx); got != "req-from-http" {
		t.Fatalf("RequestIDFromContext = %q, want req-from-http", got)
	}
}
