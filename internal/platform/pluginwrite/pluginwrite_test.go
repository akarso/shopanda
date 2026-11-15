package pluginwrite_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/platform/pluginwrite"
)

func TestGrantForStrip(t *testing.T) {
	ctx := pluginwrite.Grant(context.Background(), "acme", "a.b")
	if who, ok := pluginwrite.For(ctx, "a.b"); !ok || who != "acme" {
		t.Fatalf("For = %q,%v", who, ok)
	}
	if _, ok := pluginwrite.For(ctx, "a.c"); ok {
		t.Fatal("unnamed code must not be trusted")
	}
	if _, ok := pluginwrite.For(pluginwrite.Strip(ctx), "a.b"); ok {
		t.Fatal("stripped ctx must not be trusted")
	}
	if _, ok := pluginwrite.For(context.Background(), "a.b"); ok {
		t.Fatal("plain ctx must not be trusted")
	}
}
