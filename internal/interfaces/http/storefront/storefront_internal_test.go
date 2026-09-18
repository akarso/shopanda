package storefront

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/infrastructure/localcache"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
)

type countingCategoryRepo struct {
	catalog.CategoryRepository // nil embed: only FindAll is exercised below
	calls                      atomic.Int64
	categories                 []catalog.Category
}

func (r *countingCategoryRepo) FindAll(context.Context) ([]catalog.Category, error) {
	r.calls.Add(1)
	return append([]catalog.Category(nil), r.categories...), nil
}

// TestCachedCategories_CachesWithinTTL pins PR-1040's L1 cache over
// cachedCategories: a second call within the TTL window must not hit
// the repository again.
func TestCachedCategories_CachesWithinTTL(t *testing.T) {
	repo := &countingCategoryRepo{categories: []catalog.Category{{ID: "c1", Name: "Electronics"}}}
	h := &StorefrontHandler{cats: repo, catNav: localcache.New[[]catalog.Category](1, time.Minute)}

	if _, err := h.cachedCategories(context.Background()); err != nil {
		t.Fatalf("first cachedCategories: %v", err)
	}
	if _, err := h.cachedCategories(context.Background()); err != nil {
		t.Fatalf("second cachedCategories: %v", err)
	}
	if got := repo.calls.Load(); got != 1 {
		t.Fatalf("FindAll called %d times, want 1 (second call must be served from L1)", got)
	}
}

// TestCachedCategories_ReturnsIndependentCopies pins that mutating one
// caller's result can never corrupt what a later call returns — the
// same aliasing concern the pre-PR-1040 ad-hoc cache already guarded
// against (see cachedCategories's own doc comment).
func TestCachedCategories_ReturnsIndependentCopies(t *testing.T) {
	repo := &countingCategoryRepo{categories: []catalog.Category{{ID: "c1", Name: "Electronics"}}}
	h := &StorefrontHandler{cats: repo, catNav: localcache.New[[]catalog.Category](1, time.Minute)}

	first, err := h.cachedCategories(context.Background())
	if err != nil {
		t.Fatalf("cachedCategories: %v", err)
	}
	first[0].Name = "mutated"

	second, err := h.cachedCategories(context.Background())
	if err != nil {
		t.Fatalf("cachedCategories: %v", err)
	}
	if second[0].Name != "Electronics" {
		t.Fatalf("second[0].Name = %q, want %q — mutating a prior result must not leak into the cache", second[0].Name, "Electronics")
	}
}

// TestCachedCategories_TTLExpiryRefetches pins that the cache does not
// serve stale data forever without WithBus wired — the TTL alone must
// eventually force a refetch.
func TestCachedCategories_TTLExpiryRefetches(t *testing.T) {
	repo := &countingCategoryRepo{categories: []catalog.Category{{ID: "c1"}}}
	h := &StorefrontHandler{cats: repo, catNav: localcache.New[[]catalog.Category](1, 20*time.Millisecond)}

	if _, err := h.cachedCategories(context.Background()); err != nil {
		t.Fatalf("cachedCategories: %v", err)
	}
	time.Sleep(40 * time.Millisecond)
	if _, err := h.cachedCategories(context.Background()); err != nil {
		t.Fatalf("cachedCategories: %v", err)
	}
	if got := repo.calls.Load(); got != 2 {
		t.Fatalf("FindAll called %d times, want 2 (TTL must force a refetch)", got)
	}
}

// TestWithBus_EvictsOnCategoryEvents pins that WithBus makes a category
// create/update/delete evict the L1 cache immediately in this process,
// rather than relying solely on the TTL.
func TestWithBus_EvictsOnCategoryEvents(t *testing.T) {
	for _, evtName := range []string{
		catalog.EventCategoryCreated,
		catalog.EventCategoryUpdated,
		catalog.EventCategoryDeleted,
	} {
		t.Run(evtName, func(t *testing.T) {
			repo := &countingCategoryRepo{categories: []catalog.Category{{ID: "c1"}}}
			h := &StorefrontHandler{cats: repo, catNav: localcache.New[[]catalog.Category](1, time.Minute)}
			bus := event.NewBus(logger.New("error"))
			h.WithBus(bus)

			if _, err := h.cachedCategories(context.Background()); err != nil {
				t.Fatalf("first cachedCategories: %v", err)
			}
			if err := bus.Publish(context.Background(), event.New(evtName, "test", nil)); err != nil {
				t.Fatalf("Publish: %v", err)
			}
			if _, err := h.cachedCategories(context.Background()); err != nil {
				t.Fatalf("second cachedCategories: %v", err)
			}
			if got := repo.calls.Load(); got != 2 {
				t.Fatalf("FindAll called %d times, want 2 (%s must evict the L1 cache)", got, evtName)
			}
		})
	}
}

// TestWithBus_IgnoresCategoryAssignmentSource pins the code review fix:
// catalog.EventCategoryUpdated republished by
// category_product_assignment_admin.go on every single product↔category
// Assign/Unassign call (source "category.assignment") must NOT clear the
// nav cache — it fires far more often than an actual category edit, and
// the nav tree doesn't depend on product membership at all. Without this
// filter, this event would clear L1 on every catalog assignment op,
// which is exactly the "tag that changes more than a few times a
// minute" case the package's own denylist rule exists to keep out.
func TestWithBus_IgnoresCategoryAssignmentSource(t *testing.T) {
	repo := &countingCategoryRepo{categories: []catalog.Category{{ID: "c1"}}}
	h := &StorefrontHandler{cats: repo, catNav: localcache.New[[]catalog.Category](1, time.Minute)}
	bus := event.NewBus(logger.New("error"))
	h.WithBus(bus)

	if _, err := h.cachedCategories(context.Background()); err != nil {
		t.Fatalf("first cachedCategories: %v", err)
	}
	if err := bus.Publish(context.Background(), event.New(catalog.EventCategoryUpdated, "category.assignment", nil)); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if _, err := h.cachedCategories(context.Background()); err != nil {
		t.Fatalf("second cachedCategories: %v", err)
	}
	if got := repo.calls.Load(); got != 1 {
		t.Fatalf("FindAll called %d times, want 1 (a category-assignment-sourced event must not evict the L1 cache)", got)
	}
}

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

func TestStorefrontAccountSecurityVerifier_VerifyEmailToken_AcceptsLegacyPurposeLessTokens(t *testing.T) {
	verifier := newStorefrontAccountSecurityVerifier("test-secret", time.Minute)
	token, err := verifier.emailToken("", "cust-1", "/account/orders", storefrontEmailVerificationDefaultRedirect, time.Now().UTC())
	if err != nil {
		t.Fatalf("emailToken: %v", err)
	}

	customerID, redirectTo, ok := verifier.verifyEmailToken(token, storefrontEmailTokenPurposeAccountEmail)
	if !ok {
		t.Fatal("expected legacy purpose-less token to verify")
	}
	if customerID != "cust-1" {
		t.Fatalf("customerID = %q, want %q", customerID, "cust-1")
	}
	if redirectTo != "/account/orders" {
		t.Fatalf("redirectTo = %q, want %q", redirectTo, "/account/orders")
	}
}

func TestStorefrontAccountSecurityVerifier_VerifyCheckoutResumeToken_RejectsTamperedCiphertext(t *testing.T) {
	verifier := newStorefrontAccountSecurityVerifier("test-secret", time.Minute)
	state := storefrontCheckoutResumeState{
		Step:           "payment",
		ShippingMethod: "flat_rate",
		PaymentMethod:  "manual",
		Address: StorefrontCheckoutAddress{
			FirstName: "Ada", LastName: "Lovelace",
			Street: "1 Logic Lane", City: "Berlin", Postcode: "10115", Country: "DE",
		},
	}
	token, err := verifier.checkoutResumeToken("cust-1", state, time.Now().UTC())
	if err != nil {
		t.Fatalf("checkoutResumeToken: %v", err)
	}
	stdToken := strings.NewReplacer("-", "+", "_", "/").Replace(token)
	if rem := len(stdToken) % 4; rem != 0 {
		stdToken += strings.Repeat("=", 4-rem)
	}
	raw, err := base64.StdEncoding.DecodeString(stdToken)
	if err != nil {
		t.Fatalf("DecodeString token: %v", err)
	}
	// flip a byte in decoded ciphertext so base64 stays valid but GCM auth fails
	raw[len(raw)-1] ^= 0xFF
	tamperedStd := base64.StdEncoding.EncodeToString(raw)
	tampered := strings.TrimRight(strings.NewReplacer("+", "-", "/", "_").Replace(tamperedStd), "=")

	if _, ok := verifier.verifyCheckoutResumeToken(tampered, "cust-1"); ok {
		t.Fatal("expected tampered token to be rejected")
	}
}

func TestStorefrontAccountSecurityVerifier_VerifyCheckoutResumeToken_RejectsExpiredToken(t *testing.T) {
	verifier := newStorefrontAccountSecurityVerifier("test-secret", time.Minute)
	state := storefrontCheckoutResumeState{Step: "payment", ShippingMethod: "flat_rate", PaymentMethod: "manual"}
	// issue a token whose ExpiresAt lands in the past: use a base time older than the emailTokenTTL
	past := time.Now().UTC().Add(-(verifier.emailTokenTTL + time.Minute))
	token, err := verifier.checkoutResumeToken("cust-1", state, past)
	if err != nil {
		t.Fatalf("checkoutResumeToken: %v", err)
	}

	if _, ok := verifier.verifyCheckoutResumeToken(token, "cust-1"); ok {
		t.Fatal("expected expired token to be rejected")
	}
}

func TestStorefrontAccountSecurityVerifier_VerifyCheckoutResumeToken_RejectsMismatchedCustomer(t *testing.T) {
	verifier := newStorefrontAccountSecurityVerifier("test-secret", time.Minute)
	state := storefrontCheckoutResumeState{Step: "payment", ShippingMethod: "flat_rate", PaymentMethod: "manual"}
	token, err := verifier.checkoutResumeToken("cust-1", state, time.Now().UTC())
	if err != nil {
		t.Fatalf("checkoutResumeToken: %v", err)
	}

	if _, ok := verifier.verifyCheckoutResumeToken(token, "cust-2"); ok {
		t.Fatal("expected token issued for cust-1 to be rejected when verified as cust-2")
	}
}

func TestStorefrontAccountSecurityVerifier_VerifyCheckoutResumeToken_AcceptsValidToken(t *testing.T) {
	verifier := newStorefrontAccountSecurityVerifier("test-secret", time.Minute)
	want := storefrontCheckoutResumeState{
		Step:           "payment",
		ShippingMethod: "flat_rate",
		PaymentMethod:  "manual",
		Address: StorefrontCheckoutAddress{
			FirstName: "Ada", LastName: "Lovelace",
			Street: "1 Logic Lane", City: "Berlin", Postcode: "10115", Country: "DE",
		},
	}
	token, err := verifier.checkoutResumeToken("cust-1", want, time.Now().UTC())
	if err != nil {
		t.Fatalf("checkoutResumeToken: %v", err)
	}

	got, ok := verifier.verifyCheckoutResumeToken(token, "cust-1")
	if !ok {
		t.Fatal("expected valid token to verify")
	}
	if got.Step != want.Step {
		t.Errorf("Step = %q, want %q", got.Step, want.Step)
	}
	if got.ShippingMethod != want.ShippingMethod {
		t.Errorf("ShippingMethod = %q, want %q", got.ShippingMethod, want.ShippingMethod)
	}
	if got.PaymentMethod != want.PaymentMethod {
		t.Errorf("PaymentMethod = %q, want %q", got.PaymentMethod, want.PaymentMethod)
	}
	if got.Address.FirstName != want.Address.FirstName {
		t.Errorf("Address.FirstName = %q, want %q", got.Address.FirstName, want.Address.FirstName)
	}
	if got.Address.Country != want.Address.Country {
		t.Errorf("Address.Country = %q, want %q", got.Address.Country, want.Address.Country)
	}
}
