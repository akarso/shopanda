package http

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

func TestStorefrontCategoryTree_PreservesDescendants(t *testing.T) {
	parentID := "cat-root"
	childID := "cat-child"
	tree := storefrontCategoryTree([]catalog.Category{
		{ID: "cat-root", Name: "Electronics", Slug: "electronics"},
		{ID: childID, ParentID: &parentID, Name: "Headphones", Slug: "headphones"},
		{ID: "cat-grandchild", ParentID: &childID, Name: "Wireless", Slug: "wireless"},
	})

	if len(tree) != 1 {
		t.Fatalf("roots = %d, want 1", len(tree))
	}
	if len(tree[0].Children) != 1 {
		t.Fatalf("root children = %d, want 1", len(tree[0].Children))
	}
	if len(tree[0].Children[0].Children) != 1 {
		t.Fatalf("grandchildren = %d, want 1", len(tree[0].Children[0].Children))
	}
	if tree[0].Children[0].Children[0].Label != "Wireless" {
		t.Fatalf("grandchild label = %q, want %q", tree[0].Children[0].Children[0].Label, "Wireless")
	}
}

func TestStorefrontBreadcrumbs_StopsOnCycle(t *testing.T) {
	parentA := "cat-b"
	parentB := "cat-a"
	all := []catalog.Category{
		{ID: "cat-a", ParentID: &parentA, Name: "Audio", Slug: "audio"},
		{ID: "cat-b", ParentID: &parentB, Name: "Headphones", Slug: "headphones"},
	}

	trail := storefrontBreadcrumbs(all, &all[0])

	if len(trail) != 3 {
		t.Fatalf("breadcrumb count = %d, want 3", len(trail))
	}
	if trail[0].Label != "Home" {
		t.Fatalf("first breadcrumb = %q, want %q", trail[0].Label, "Home")
	}
	if trail[len(trail)-1].Label != "Audio" {
		t.Fatalf("last breadcrumb = %q, want %q", trail[len(trail)-1].Label, "Audio")
	}
	if !trail[len(trail)-1].Current {
		t.Fatal("last breadcrumb should be current")
	}
}

func TestStorefrontCheckoutErrorMessage_SanitizesServerErrors(t *testing.T) {
	err := errors.New("db credentials leaked")
	if got := storefrontCheckoutErrorMessage(err); got != "Sorry, something went wrong. Please try again later." {
		t.Fatalf("storefrontCheckoutErrorMessage() = %q", got)
	}
}

func TestStorefrontCheckoutErrorMessage_PreservesClientErrors(t *testing.T) {
	err := apperror.Validation("Select a shipping method to continue.")
	if got := storefrontCheckoutErrorMessage(err); got != err.Error() {
		t.Fatalf("storefrontCheckoutErrorMessage() = %q, want %q", got, err.Error())
	}
}

func TestStorefrontAccountSecurityVerifier_IsVerifiedRejectsMissingContext(t *testing.T) {
	verifier := newStorefrontAccountSecurityVerifier("test-secret", time.Minute)
	request := httptest.NewRequest("GET", "/account/security", nil)

	if verifier.isVerified(nil, "cust-1") {
		t.Fatal("expected nil request to fail verification")
	}
	if verifier.isVerified(request, "   ") {
		t.Fatal("expected blank customer id to fail verification")
	}
}

func TestStorefrontHandler_WithAccountSecurity_PanicsOnInvalidConfig(t *testing.T) {
	tests := []struct {
		name   string
		secret string
		ttl    time.Duration
		want   string
	}{
		{name: "empty secret", secret: " ", ttl: time.Minute, want: "secret must not be empty"},
		{name: "non-positive ttl", secret: "test-secret", ttl: 0, want: "ttl must be positive"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatal("expected panic, got nil")
				}
				msg, ok := r.(string)
				if !ok {
					t.Fatalf("panic = %#v, want string", r)
				}
				if !strings.Contains(msg, tc.want) {
					t.Fatalf("panic = %q, want substring %q", msg, tc.want)
				}
			}()

			NewStorefrontHandler(nil, nil, nil, nil, nil, nil).WithAccountSecurity(tc.secret, tc.ttl)
		})
	}
}

func TestStorefrontHandler_WithAccountSecurityEmailLinks_PanicsOnInvalidBaseURL(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic, got nil")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("panic = %#v, want string", r)
		}
		if !strings.Contains(msg, "absolute store base URL") {
			t.Fatalf("panic = %q, want absolute store base URL", msg)
		}
	}()

	NewStorefrontHandler(nil, nil, nil, nil, nil, nil).
		WithAccountSecurity("test-secret", time.Minute).
		WithAccountSecurityEmailLinks("/relative", 0)
}
