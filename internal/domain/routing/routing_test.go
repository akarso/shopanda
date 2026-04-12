package routing_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/domain/routing"
)

func TestNewURLRewrite_Valid(t *testing.T) {
	rw, err := routing.NewURLRewrite("/nike-air-max", "product", "aaa-bbb-ccc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rw.Path() != "/nike-air-max" {
		t.Errorf("path = %q, want %q", rw.Path(), "/nike-air-max")
	}
	if rw.Type() != "product" {
		t.Errorf("type = %q, want %q", rw.Type(), "product")
	}
	if rw.EntityID() != "aaa-bbb-ccc" {
		t.Errorf("entity_id = %q, want %q", rw.EntityID(), "aaa-bbb-ccc")
	}
}

func TestNewURLRewrite_EmptyPath(t *testing.T) {
	_, err := routing.NewURLRewrite("", "product", "aaa")
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestNewURLRewrite_EmptyType(t *testing.T) {
	_, err := routing.NewURLRewrite("/foo", "", "aaa")
	if err == nil {
		t.Fatal("expected error for empty type")
	}
}

func TestNewURLRewrite_EmptyEntityID(t *testing.T) {
	_, err := routing.NewURLRewrite("/foo", "product", "")
	if err == nil {
		t.Fatal("expected error for empty entity_id")
	}
}

func TestNewURLRewriteFromDB(t *testing.T) {
	rw := routing.NewURLRewriteFromDB("/slug", "category", "id-123")
	if rw.Path() != "/slug" || rw.Type() != "category" || rw.EntityID() != "id-123" {
		t.Errorf("unexpected values: path=%q type=%q entity_id=%q", rw.Path(), rw.Type(), rw.EntityID())
	}
}

func TestContext_RoundTrip(t *testing.T) {
	rw := routing.NewURLRewriteFromDB("/test", "product", "xyz")
	ctx := routing.WithRewrite(context.Background(), rw)
	got := routing.RewriteFrom(ctx)
	if got != rw {
		t.Error("RewriteFrom did not return the stored rewrite")
	}
}

func TestContext_Missing(t *testing.T) {
	got := routing.RewriteFrom(context.Background())
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}
