package cart_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	cartApp "github.com/akarso/shopanda/internal/application/cart"
	"github.com/akarso/shopanda/internal/application/notification"
	domainCart "github.com/akarso/shopanda/internal/domain/cart"
	"github.com/akarso/shopanda/internal/domain/catalog"
	domainCfg "github.com/akarso/shopanda/internal/domain/config"
	"github.com/akarso/shopanda/internal/domain/customer"
	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/domain/mail"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/infrastructure/email"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

func loadRecoveryFileTemplates(t *testing.T) (*mail.Templates, mail.Layout) {
	t.Helper()
	dir := filepath.Join("..", "..", "..", "templates", "emails")
	if _, err := os.Stat(filepath.Join(dir, "cart_recovery.html")); err != nil {
		t.Skipf("repo cart_recovery.html unavailable: %v", err)
	}
	templates := mail.NewTemplates()
	notification.RegisterTemplates(templates)
	loader, layout, _, err := email.Setup(dir)
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if _, err := email.LoadDir(templates, loader); err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	return templates, layout
}

type recoveryCartRepo struct {
	domainCart.CartRepository
	candidates    []domainCart.Cart
	marked        []string
	claimedAt     map[string]time.Time
	markClaimable bool
}

func (s *recoveryCartRepo) FindRecoveryCandidates(_ context.Context, _ time.Time, _ int) ([]*domainCart.Cart, error) {
	out := make([]*domainCart.Cart, len(s.candidates))
	for i := range s.candidates {
		c := s.candidates[i]
		out[i] = &c
	}
	return out, nil
}

func (s *recoveryCartRepo) MarkRecoveryEmailSent(_ context.Context, cartID string, sentAt time.Time) (bool, error) {
	if s.claimedAt == nil {
		s.claimedAt = map[string]time.Time{}
	}
	if _, ok := s.claimedAt[cartID]; ok {
		return false, nil
	}
	if !s.markClaimable {
		return false, nil
	}
	s.claimedAt[cartID] = sentAt
	s.marked = append(s.marked, cartID)
	return true, nil
}

func (s *recoveryCartRepo) ClearRecoveryEmailSent(_ context.Context, cartID string, sentAt time.Time) error {
	if s.claimedAt == nil {
		return domainCart.ErrRecoveryClaimMismatch
	}
	at, ok := s.claimedAt[cartID]
	if !ok || !at.Equal(sentAt) {
		return domainCart.ErrRecoveryClaimMismatch
	}
	delete(s.claimedAt, cartID)
	out := s.marked[:0]
	for _, id := range s.marked {
		if id != cartID {
			out = append(out, id)
		}
	}
	s.marked = out
	return nil
}

type recoveryCustomerRepo struct {
	customer.CustomerRepository
	byID map[string]*customer.Customer
}

func (s *recoveryCustomerRepo) FindByID(_ context.Context, id string) (*customer.Customer, error) {
	return s.byID[id], nil
}

type recoveryVariantRepo struct {
	catalog.VariantRepository
	byID      map[string]*catalog.Variant
	findCalls int
}

func (s *recoveryVariantRepo) FindByID(_ context.Context, id string) (*catalog.Variant, error) {
	s.findCalls++
	return s.byID[id], nil
}

type recoveryProductRepo struct {
	catalog.ProductRepository
	byID map[string]*catalog.Product
}

func (s *recoveryProductRepo) FindByID(_ context.Context, id string) (*catalog.Product, error) {
	return s.byID[id], nil
}

type recoveryQueue struct {
	jobs.Queue
	enqueued   []jobs.Job
	replaced   []jobs.Job
	requeued   []string
	errOnce    error
	replaceErr error
	allowDup   bool // brokers that ignore job id uniqueness
	byID       map[string]struct{}
	details    map[string]*jobs.Detail
}

func (s *recoveryQueue) Enqueue(_ context.Context, job jobs.Job) error {
	if s.errOnce != nil {
		err := s.errOnce
		s.errOnce = nil
		return err
	}
	if s.byID == nil {
		s.byID = map[string]struct{}{}
	}
	if !s.allowDup {
		if _, ok := s.byID[job.ID]; ok {
			return apperror.Conflict("job already enqueued")
		}
	}
	s.byID[job.ID] = struct{}{}
	s.enqueued = append(s.enqueued, job)
	return nil
}

func (s *recoveryQueue) Get(_ context.Context, id string) (*jobs.Detail, error) {
	if s.details == nil {
		return nil, nil
	}
	return s.details[id], nil
}

func (s *recoveryQueue) ReplaceFailed(_ context.Context, job jobs.Job) error {
	if s.replaceErr != nil {
		return s.replaceErr
	}
	s.replaced = append(s.replaced, job)
	if s.details != nil {
		s.details[job.ID] = &jobs.Detail{
			Summary: jobs.Summary{ID: job.ID, Status: jobs.StatusPending, Attempts: 0},
			Payload: job.Payload,
		}
	}
	return nil
}

func (s *recoveryQueue) Requeue(_ context.Context, id string, attempt int, opts ...jobs.RequeueOption) error {
	settings := jobs.ResolveRequeueOptions(opts)
	if !settings.Terminal {
		return errors.New("recoveryQueue.Requeue: WithRequeueTerminal required")
	}
	d := (*jobs.Detail)(nil)
	if s.details != nil {
		d = s.details[id]
	}
	if d == nil || d.Attempts != attempt {
		want := -1
		if d != nil {
			want = d.Attempts
		}
		return fmt.Errorf("recoveryQueue.Requeue: attempt %d != detail.Attempts %d", attempt, want)
	}
	s.requeued = append(s.requeued, id)
	d.Status = jobs.StatusPending
	return nil
}

// recoveryGetOnlyQueue has Get but not ReplaceFailed (Redis-shaped).
type recoveryGetOnlyQueue struct {
	jobs.Queue
	enqueued []jobs.Job
	byID     map[string]struct{}
	details  map[string]*jobs.Detail
}

func (s *recoveryGetOnlyQueue) Enqueue(_ context.Context, job jobs.Job) error {
	if s.byID == nil {
		s.byID = map[string]struct{}{}
	}
	if _, ok := s.byID[job.ID]; ok {
		return apperror.Conflict("job already enqueued")
	}
	s.byID[job.ID] = struct{}{}
	s.enqueued = append(s.enqueued, job)
	return nil
}

func (s *recoveryGetOnlyQueue) Get(_ context.Context, id string) (*jobs.Detail, error) {
	if s.details == nil {
		return nil, nil
	}
	return s.details[id], nil
}

// recoveryOuterGetQueue mimics WrapRequeueNotify: Get outside, ReplaceFailed
// only on the UnwrapQueue inner value.
type recoveryOuterGetQueue struct {
	jobs.Queue
	inner    *recoveryReplaceInner
	enqueued []jobs.Job
	byID     map[string]struct{}
	details  map[string]*jobs.Detail
}

func (s *recoveryOuterGetQueue) Enqueue(_ context.Context, job jobs.Job) error {
	if s.byID == nil {
		s.byID = map[string]struct{}{}
	}
	if _, ok := s.byID[job.ID]; ok {
		return apperror.Conflict("job already enqueued")
	}
	s.byID[job.ID] = struct{}{}
	s.enqueued = append(s.enqueued, job)
	return nil
}

func (s *recoveryOuterGetQueue) Get(_ context.Context, id string) (*jobs.Detail, error) {
	if s.details == nil {
		return nil, nil
	}
	return s.details[id], nil
}

func (s *recoveryOuterGetQueue) UnwrapQueue() jobs.Queue {
	return s.inner
}

type recoveryReplaceInner struct {
	jobs.Queue
	replaced []jobs.Job
}

func (s *recoveryReplaceInner) ReplaceFailed(_ context.Context, job jobs.Job) error {
	s.replaced = append(s.replaced, job)
	return nil
}

type recoveryLogger struct {
	infos []string
}

func (l *recoveryLogger) Info(msg string, _ map[string]interface{}) {
	l.infos = append(l.infos, msg)
}

func (l *recoveryLogger) Warn(_ string, _ map[string]interface{}) {}

func (l *recoveryLogger) Error(_ string, _ error, _ map[string]interface{}) {}

func testRecoveryHandler(t *testing.T, carts *recoveryCartRepo, customers *recoveryCustomerRepo) (*cartApp.RecoveryHandler, *recoveryQueue) {
	return testRecoveryHandlerWithSettings(t, carts, customers, nil)
}

func testRecoveryHandlerWithSettings(t *testing.T, carts *recoveryCartRepo, customers *recoveryCustomerRepo, settings domainCfg.Repository) (*cartApp.RecoveryHandler, *recoveryQueue) {
	t.Helper()
	templates := mail.NewTemplates()
	notification.RegisterTemplates(templates)
	queue := &recoveryQueue{}
	h := cartApp.NewRecoveryHandler(cartApp.RecoveryHandlerConfig{
		Carts:     carts,
		Customers: customers,
		Variants: &recoveryVariantRepo{byID: map[string]*catalog.Variant{
			"v1": {ID: "v1", ProductID: "p1", SKU: "SKU-1", Name: "Blue Tee"},
		}},
		Products:  &recoveryProductRepo{byID: map[string]*catalog.Product{}},
		Templates: templates,
		Queue:     queue,
		StoreURL:  "https://shop.example",
		Settings:  settings,
		Log:       &recoveryLogger{},
	})
	return h, queue
}

func TestRecoveryHandler_Type(t *testing.T) {
	h, _ := testRecoveryHandler(t, &recoveryCartRepo{}, &recoveryCustomerRepo{byID: map[string]*customer.Customer{}})
	if h.Type() != cartApp.RecoveryJobType {
		t.Errorf("Type() = %q, want %q", h.Type(), cartApp.RecoveryJobType)
	}
}

func TestRecoveryHandler_Handle_NoCandidates(t *testing.T) {
	h, queue := testRecoveryHandler(t, &recoveryCartRepo{}, &recoveryCustomerRepo{byID: map[string]*customer.Customer{}})
	err := h.Handle(context.Background(), jobs.Job{Type: cartApp.RecoveryJobType})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(queue.enqueued) != 0 {
		t.Fatalf("expected no enqueued jobs, got %d", len(queue.enqueued))
	}
}

func TestRecoveryHandler_Handle_SendsRecoveryEmail(t *testing.T) {
	c, err := domainCart.NewCart("cart-1", "EUR")
	if err != nil {
		t.Fatalf("NewCart: %v", err)
	}
	if err := c.SetCustomerID("cust-1"); err != nil {
		t.Fatalf("SetCustomerID: %v", err)
	}
	if err := c.AddItem("v1", 2, shared.MustNewMoney(1999, "EUR")); err != nil {
		t.Fatalf("AddItem: %v", err)
	}

	carts := &recoveryCartRepo{candidates: []domainCart.Cart{c}, markClaimable: true}
	customers := &recoveryCustomerRepo{byID: map[string]*customer.Customer{
		"cust-1": {
			ID:        "cust-1",
			Email:     "buyer@example.com",
			FirstName: "Alex",
			Status:    customer.StatusActive,
		},
	}}
	templates, layout := loadRecoveryFileTemplates(t)
	queue := &recoveryQueue{}
	h := cartApp.NewRecoveryHandler(cartApp.RecoveryHandlerConfig{
		Carts:     carts,
		Customers: customers,
		Variants: &recoveryVariantRepo{byID: map[string]*catalog.Variant{
			"v1": {ID: "v1", ProductID: "p1", SKU: "SKU-1", Name: "Blue Tee"},
		}},
		Products:  &recoveryProductRepo{byID: map[string]*catalog.Product{}},
		Templates: templates,
		Layout:    layout,
		Queue:     queue,
		StoreURL:  "https://shop.example",
		Log:       &recoveryLogger{},
	})

	err = h.Handle(context.Background(), jobs.Job{Type: cartApp.RecoveryJobType})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(queue.enqueued) != 1 {
		t.Fatalf("enqueued jobs = %d, want 1", len(queue.enqueued))
	}
	job := queue.enqueued[0]
	if job.Type != notification.JobTypeEmailSend {
		t.Errorf("job type = %q, want %q", job.Type, notification.JobTypeEmailSend)
	}
	if got := job.Payload["to"]; got != "buyer@example.com" {
		t.Errorf("payload to = %v, want buyer@example.com", got)
	}
	body, _ := job.Payload["body"].(string)
	if !strings.Contains(body, `class="cart-recovery-items"`) {
		t.Fatalf("body missing file cart table: %s", body)
	}
	if !strings.Contains(body, "https://shop.example/cart") {
		t.Fatalf("body missing absolute cart URL: %s", body)
	}
	if strings.Contains(body, `href="/cart"`) {
		t.Fatalf("body must not use relative cart href: %s", body)
	}
	if !strings.Contains(body, "EUR 19.99") {
		t.Fatalf("body missing major-unit unit price, got: %s", body)
	}
	if !strings.Contains(body, "EUR 39.98") {
		t.Fatalf("body missing major-unit line total, got: %s", body)
	}
	if strings.Contains(body, "1999 EUR") || strings.Contains(body, "3998 EUR") {
		t.Fatalf("body still has minor-unit money: %s", body)
	}
	if len(carts.marked) != 1 || carts.marked[0] != "cart-1" {
		t.Errorf("marked carts = %v, want [cart-1]", carts.marked)
	}
}

func TestRecoveryHandler_Handle_EmptyStoreURLSkipsSend(t *testing.T) {
	for _, storeURL := range []string{"", "not-a-url", "/relative", "ftp://shop.example"} {
		t.Run(storeURL, func(t *testing.T) {
			c, err := domainCart.NewCart("cart-bad-url", "EUR")
			if err != nil {
				t.Fatalf("NewCart: %v", err)
			}
			if err := c.SetCustomerID("cust-1"); err != nil {
				t.Fatalf("SetCustomerID: %v", err)
			}
			if err := c.AddItem("v1", 1, shared.MustNewMoney(999, "EUR")); err != nil {
				t.Fatalf("AddItem: %v", err)
			}
			carts := &recoveryCartRepo{candidates: []domainCart.Cart{c}, markClaimable: true}
			customers := &recoveryCustomerRepo{byID: map[string]*customer.Customer{
				"cust-1": {ID: "cust-1", Email: "buyer@example.com", FirstName: "Alex", Status: customer.StatusActive},
			}}
			templates := mail.NewTemplates()
			notification.RegisterTemplates(templates)
			queue := &recoveryQueue{}
			variants := &recoveryVariantRepo{byID: map[string]*catalog.Variant{
				"v1": {ID: "v1", ProductID: "p1", SKU: "SKU-1", Name: "Blue Tee"},
			}}
			h := cartApp.NewRecoveryHandler(cartApp.RecoveryHandlerConfig{
				Carts:     carts,
				Customers: customers,
				Variants:  variants,
				Products:  &recoveryProductRepo{byID: map[string]*catalog.Product{}},
				Templates: templates,
				Queue:     queue,
				StoreURL:  storeURL,
				Log:       &recoveryLogger{},
			})
			if err := h.Handle(context.Background(), jobs.Job{Type: cartApp.RecoveryJobType}); err == nil {
				t.Fatal("Handle must fail on bad store URL before scanning carts")
			}
			if len(queue.enqueued) != 0 {
				t.Fatalf("bad store URL %q must not enqueue, got %d", storeURL, len(queue.enqueued))
			}
			if len(carts.marked) != 0 {
				t.Fatalf("bad store URL %q must not mark, got %v", storeURL, carts.marked)
			}
			if variants.findCalls != 0 {
				t.Fatalf("bad store URL %q must not look up variants, got %d calls", storeURL, variants.findCalls)
			}
		})
	}
}

func TestRecoveryHandler_Handle_SkipsInactiveCustomer(t *testing.T) {
	c, err := domainCart.NewCart("cart-2", "EUR")
	if err != nil {
		t.Fatalf("NewCart: %v", err)
	}
	if err := c.SetCustomerID("cust-2"); err != nil {
		t.Fatalf("SetCustomerID: %v", err)
	}
	if err := c.AddItem("v1", 1, shared.MustNewMoney(999, "EUR")); err != nil {
		t.Fatalf("AddItem: %v", err)
	}

	carts := &recoveryCartRepo{candidates: []domainCart.Cart{c}, markClaimable: true}
	customers := &recoveryCustomerRepo{byID: map[string]*customer.Customer{
		"cust-2": {ID: "cust-2", Email: "gone@example.com", Status: customer.StatusDisabled},
	}}
	h, queue := testRecoveryHandler(t, carts, customers)

	err = h.Handle(context.Background(), jobs.Job{Type: cartApp.RecoveryJobType})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(queue.enqueued) != 0 {
		t.Fatalf("expected no email jobs, got %d", len(queue.enqueued))
	}
	if len(carts.marked) != 1 || carts.marked[0] != "cart-2" {
		t.Fatalf("expected terminal skip mark for cart-2, got %v", carts.marked)
	}
}

func TestRecoveryHandler_Handle_SkipsAlreadyClaimedCart(t *testing.T) {
	c, err := domainCart.NewCart("cart-3", "EUR")
	if err != nil {
		t.Fatalf("NewCart: %v", err)
	}
	if err := c.SetCustomerID("cust-3"); err != nil {
		t.Fatalf("SetCustomerID: %v", err)
	}
	if err := c.AddItem("v1", 1, shared.MustNewMoney(999, "EUR")); err != nil {
		t.Fatalf("AddItem: %v", err)
	}

	carts := &recoveryCartRepo{candidates: []domainCart.Cart{c}, markClaimable: false}
	customers := &recoveryCustomerRepo{byID: map[string]*customer.Customer{
		"cust-3": {
			ID:        "cust-3",
			Email:     "buyer@example.com",
			FirstName: "Alex",
			Status:    customer.StatusActive,
		},
	}}
	h, queue := testRecoveryHandler(t, carts, customers)

	err = h.Handle(context.Background(), jobs.Job{Type: cartApp.RecoveryJobType})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(queue.enqueued) != 0 {
		t.Fatalf("expected no email jobs when claim fails, got %d", len(queue.enqueued))
	}
	if len(carts.marked) != 0 {
		t.Fatalf("expected no mark when claim fails, got %v", carts.marked)
	}
}

func TestRecoveryHandler_Handle_EnqueueErrorClearsMark(t *testing.T) {
	c, err := domainCart.NewCart("cart-enq-err", "EUR")
	if err != nil {
		t.Fatalf("NewCart: %v", err)
	}
	if err := c.SetCustomerID("cust-1"); err != nil {
		t.Fatalf("SetCustomerID: %v", err)
	}
	if err := c.AddItem("v1", 1, shared.MustNewMoney(999, "EUR")); err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	carts := &recoveryCartRepo{candidates: []domainCart.Cart{c}, markClaimable: true}
	customers := &recoveryCustomerRepo{byID: map[string]*customer.Customer{
		"cust-1": {ID: "cust-1", Email: "buyer@example.com", FirstName: "Alex", Status: customer.StatusActive},
	}}
	templates := mail.NewTemplates()
	notification.RegisterTemplates(templates)
	queue := &recoveryQueue{errOnce: errors.New("queue down")}
	h := cartApp.NewRecoveryHandler(cartApp.RecoveryHandlerConfig{
		Carts:     carts,
		Customers: customers,
		Variants: &recoveryVariantRepo{byID: map[string]*catalog.Variant{
			"v1": {ID: "v1", ProductID: "p1", SKU: "SKU-1", Name: "Blue Tee"},
		}},
		Products:  &recoveryProductRepo{byID: map[string]*catalog.Product{}},
		Templates: templates,
		Queue:     queue,
		StoreURL:  "https://shop.example",
		Log:       &recoveryLogger{},
	})
	if err := h.Handle(context.Background(), jobs.Job{Type: cartApp.RecoveryJobType}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(queue.enqueued) != 0 {
		t.Fatalf("enqueue error must leave queue empty, got %d", len(queue.enqueued))
	}
	if len(carts.marked) != 0 {
		t.Fatalf("enqueue error must clear mark, got %v", carts.marked)
	}
}

func TestRecoveryHandler_Handle_AllowDupQueueClaimPreventsSecondPublish(t *testing.T) {
	c, err := domainCart.NewCart("cart-dup", "EUR")
	if err != nil {
		t.Fatalf("NewCart: %v", err)
	}
	if err := c.SetCustomerID("cust-1"); err != nil {
		t.Fatalf("SetCustomerID: %v", err)
	}
	if err := c.AddItem("v1", 1, shared.MustNewMoney(999, "EUR")); err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	carts := &recoveryCartRepo{candidates: []domainCart.Cart{c}, markClaimable: true}
	customers := &recoveryCustomerRepo{byID: map[string]*customer.Customer{
		"cust-1": {ID: "cust-1", Email: "buyer@example.com", FirstName: "Alex", Status: customer.StatusActive},
	}}
	templates := mail.NewTemplates()
	notification.RegisterTemplates(templates)
	queue := &recoveryQueue{allowDup: true}
	h := cartApp.NewRecoveryHandler(cartApp.RecoveryHandlerConfig{
		Carts:     carts,
		Customers: customers,
		Variants: &recoveryVariantRepo{byID: map[string]*catalog.Variant{
			"v1": {ID: "v1", ProductID: "p1", SKU: "SKU-1", Name: "Blue Tee"},
		}},
		Products:  &recoveryProductRepo{byID: map[string]*catalog.Product{}},
		Templates: templates,
		Queue:     queue,
		StoreURL:  "https://shop.example",
		Log:       &recoveryLogger{},
	})
	if err := h.Handle(context.Background(), jobs.Job{Type: cartApp.RecoveryJobType}); err != nil {
		t.Fatalf("first: %v", err)
	}
	if len(queue.enqueued) != 1 {
		t.Fatalf("first enqueue = %d", len(queue.enqueued))
	}
	if err := h.Handle(context.Background(), jobs.Job{Type: cartApp.RecoveryJobType}); err != nil {
		t.Fatalf("second: %v", err)
	}
	if len(queue.enqueued) != 1 {
		t.Fatalf("mark-before-enqueue must block second publish on allow-dup queue, got %d", len(queue.enqueued))
	}
}

func TestRecoveryHandler_Handle_ConflictPendingKeepsMark(t *testing.T) {
	c, err := domainCart.NewCart("cart-conflict", "EUR")
	if err != nil {
		t.Fatalf("NewCart: %v", err)
	}
	if err := c.SetCustomerID("cust-1"); err != nil {
		t.Fatalf("SetCustomerID: %v", err)
	}
	if err := c.AddItem("v1", 1, shared.MustNewMoney(999, "EUR")); err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	jobID := cartApp.CartRecoveryEmailJobID(c.ID)
	carts := &recoveryCartRepo{candidates: []domainCart.Cart{c}, markClaimable: true}
	customers := &recoveryCustomerRepo{byID: map[string]*customer.Customer{
		"cust-1": {ID: "cust-1", Email: "buyer@example.com", FirstName: "Alex", Status: customer.StatusActive},
	}}
	templates := mail.NewTemplates()
	notification.RegisterTemplates(templates)
	queue := &recoveryQueue{
		byID:    map[string]struct{}{jobID: {}},
		details: map[string]*jobs.Detail{jobID: {Summary: jobs.Summary{ID: jobID, Status: jobs.StatusPending}}},
	}
	h := cartApp.NewRecoveryHandler(cartApp.RecoveryHandlerConfig{
		Carts:     carts,
		Customers: customers,
		Variants: &recoveryVariantRepo{byID: map[string]*catalog.Variant{
			"v1": {ID: "v1", ProductID: "p1", SKU: "SKU-1", Name: "Blue Tee"},
		}},
		Products:  &recoveryProductRepo{byID: map[string]*catalog.Product{}},
		Templates: templates,
		Queue:     queue,
		StoreURL:  "https://shop.example",
		Log:       &recoveryLogger{},
	})
	if err := h.Handle(context.Background(), jobs.Job{Type: cartApp.RecoveryJobType}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(queue.enqueued) != 0 {
		t.Fatalf("conflict must not enqueue again, got %d", len(queue.enqueued))
	}
	if len(carts.marked) != 1 {
		t.Fatalf("pending conflict must keep mark, got %v", carts.marked)
	}
}

func TestRecoveryHandler_Handle_ConflictFailedReplacesAndKeepsMark(t *testing.T) {
	c, err := domainCart.NewCart("cart-failed", "EUR")
	if err != nil {
		t.Fatalf("NewCart: %v", err)
	}
	if err := c.SetCustomerID("cust-1"); err != nil {
		t.Fatalf("SetCustomerID: %v", err)
	}
	if err := c.AddItem("v1", 1, shared.MustNewMoney(999, "EUR")); err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	jobID := cartApp.CartRecoveryEmailJobID(c.ID)
	carts := &recoveryCartRepo{candidates: []domainCart.Cart{c}, markClaimable: true}
	customers := &recoveryCustomerRepo{byID: map[string]*customer.Customer{
		"cust-1": {ID: "cust-1", Email: "buyer@example.com", FirstName: "Alex", Status: customer.StatusActive},
	}}
	templates := mail.NewTemplates()
	notification.RegisterTemplates(templates)
	queue := &recoveryQueue{
		byID: map[string]struct{}{jobID: {}},
		details: map[string]*jobs.Detail{
			jobID: {
				Summary: jobs.Summary{ID: jobID, Status: jobs.StatusFailed, Attempts: 3},
				Payload: map[string]interface{}{"body": "stale"},
			},
		},
	}
	h := cartApp.NewRecoveryHandler(cartApp.RecoveryHandlerConfig{
		Carts:     carts,
		Customers: customers,
		Variants: &recoveryVariantRepo{byID: map[string]*catalog.Variant{
			"v1": {ID: "v1", ProductID: "p1", SKU: "SKU-1", Name: "Blue Tee"},
		}},
		Products:  &recoveryProductRepo{byID: map[string]*catalog.Product{}},
		Templates: templates,
		Queue:     queue,
		StoreURL:  "https://shop.example",
		Log:       &recoveryLogger{},
	})
	if err := h.Handle(context.Background(), jobs.Job{Type: cartApp.RecoveryJobType}); err != nil {
		t.Fatalf("first: %v", err)
	}
	if len(queue.replaced) != 1 || queue.replaced[0].ID != jobID {
		t.Fatalf("replaced = %#v, want id %s", queue.replaced, jobID)
	}
	if body, _ := queue.replaced[0].Payload["body"].(string); body == "" || body == "stale" {
		t.Fatalf("ReplaceFailed must write fresh body, got %q", body)
	}
	if len(carts.marked) != 1 {
		t.Fatalf("successful replace must keep mark, got %v", carts.marked)
	}
	if err := h.Handle(context.Background(), jobs.Job{Type: cartApp.RecoveryJobType}); err != nil {
		t.Fatalf("second: %v", err)
	}
	if len(queue.enqueued) != 0 {
		t.Fatalf("second Handle must not publish again, got %d", len(queue.enqueued))
	}
	if len(queue.replaced) != 1 {
		t.Fatalf("second Handle must not replace again, got %d", len(queue.replaced))
	}
	if len(carts.marked) != 1 {
		t.Fatalf("marked after second = %v, want 1", carts.marked)
	}
}

func TestRecoveryHandler_Handle_ConflictFailedReplaceViaUnwrap(t *testing.T) {
	c, err := domainCart.NewCart("cart-unwrap", "EUR")
	if err != nil {
		t.Fatalf("NewCart: %v", err)
	}
	if err := c.SetCustomerID("cust-1"); err != nil {
		t.Fatalf("SetCustomerID: %v", err)
	}
	if err := c.AddItem("v1", 1, shared.MustNewMoney(999, "EUR")); err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	jobID := cartApp.CartRecoveryEmailJobID(c.ID)
	carts := &recoveryCartRepo{candidates: []domainCart.Cart{c}, markClaimable: true}
	customers := &recoveryCustomerRepo{byID: map[string]*customer.Customer{
		"cust-1": {ID: "cust-1", Email: "buyer@example.com", FirstName: "Alex", Status: customer.StatusActive},
	}}
	templates := mail.NewTemplates()
	notification.RegisterTemplates(templates)
	inner := &recoveryReplaceInner{}
	queue := &recoveryOuterGetQueue{
		inner:   inner,
		byID:    map[string]struct{}{jobID: {}},
		details: map[string]*jobs.Detail{jobID: {Summary: jobs.Summary{ID: jobID, Status: jobs.StatusFailed, Attempts: 3}}},
	}
	h := cartApp.NewRecoveryHandler(cartApp.RecoveryHandlerConfig{
		Carts:     carts,
		Customers: customers,
		Variants: &recoveryVariantRepo{byID: map[string]*catalog.Variant{
			"v1": {ID: "v1", ProductID: "p1", SKU: "SKU-1", Name: "Blue Tee"},
		}},
		Products:  &recoveryProductRepo{byID: map[string]*catalog.Product{}},
		Templates: templates,
		Queue:     queue,
		StoreURL:  "https://shop.example",
		Log:       &recoveryLogger{},
	})
	if err := h.Handle(context.Background(), jobs.Job{Type: cartApp.RecoveryJobType}); err != nil {
		t.Fatalf("first: %v", err)
	}
	if len(inner.replaced) != 1 || inner.replaced[0].ID != jobID {
		t.Fatalf("replaced via unwrap = %#v, want id %s", inner.replaced, jobID)
	}
	if body, _ := inner.replaced[0].Payload["body"].(string); body == "" || body == "stale" {
		t.Fatalf("fresh body via unwrap, got %q", body)
	}
	if _, ok := any(queue).(interface {
		ReplaceFailed(context.Context, jobs.Job) error
	}); ok {
		t.Fatal("outer queue must not implement ReplaceFailed")
	}
	if len(carts.marked) != 1 {
		t.Fatalf("marked after replace = %v, want 1", carts.marked)
	}
	if err := h.Handle(context.Background(), jobs.Job{Type: cartApp.RecoveryJobType}); err != nil {
		t.Fatalf("second: %v", err)
	}
	if len(inner.replaced) != 1 {
		t.Fatalf("second Handle must not replace again, got %d", len(inner.replaced))
	}
	if len(carts.marked) != 1 {
		t.Fatalf("marked after second = %v, want 1", carts.marked)
	}
}

func TestRecoveryHandler_Handle_ConflictFailedUnsupportedKeepsMark(t *testing.T) {
	c, err := domainCart.NewCart("cart-unsupported", "EUR")
	if err != nil {
		t.Fatalf("NewCart: %v", err)
	}
	if err := c.SetCustomerID("cust-1"); err != nil {
		t.Fatalf("SetCustomerID: %v", err)
	}
	if err := c.AddItem("v1", 1, shared.MustNewMoney(999, "EUR")); err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	jobID := cartApp.CartRecoveryEmailJobID(c.ID)
	carts := &recoveryCartRepo{candidates: []domainCart.Cart{c}, markClaimable: true}
	customers := &recoveryCustomerRepo{byID: map[string]*customer.Customer{
		"cust-1": {ID: "cust-1", Email: "buyer@example.com", FirstName: "Alex", Status: customer.StatusActive},
	}}
	templates := mail.NewTemplates()
	notification.RegisterTemplates(templates)
	queue := &recoveryGetOnlyQueue{
		byID:    map[string]struct{}{jobID: {}},
		details: map[string]*jobs.Detail{jobID: {Summary: jobs.Summary{ID: jobID, Status: jobs.StatusFailed}}},
	}
	h := cartApp.NewRecoveryHandler(cartApp.RecoveryHandlerConfig{
		Carts:     carts,
		Customers: customers,
		Variants: &recoveryVariantRepo{byID: map[string]*catalog.Variant{
			"v1": {ID: "v1", ProductID: "p1", SKU: "SKU-1", Name: "Blue Tee"},
		}},
		Products:  &recoveryProductRepo{byID: map[string]*catalog.Product{}},
		Templates: templates,
		Queue:     queue,
		StoreURL:  "https://shop.example",
		Log:       &recoveryLogger{},
	})
	if err := h.Handle(context.Background(), jobs.Job{Type: cartApp.RecoveryJobType}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(queue.enqueued) != 0 {
		t.Fatalf("unsupported must not enqueue, got %d", len(queue.enqueued))
	}
	if len(carts.marked) != 1 {
		t.Fatalf("unsupported replace must keep mark, got %v", carts.marked)
	}
}

func TestRecoveryHandler_Handle_ConflictCancelledKeepsMark(t *testing.T) {
	c, err := domainCart.NewCart("cart-cancelled", "EUR")
	if err != nil {
		t.Fatalf("NewCart: %v", err)
	}
	if err := c.SetCustomerID("cust-1"); err != nil {
		t.Fatalf("SetCustomerID: %v", err)
	}
	if err := c.AddItem("v1", 1, shared.MustNewMoney(999, "EUR")); err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	jobID := cartApp.CartRecoveryEmailJobID(c.ID)
	carts := &recoveryCartRepo{candidates: []domainCart.Cart{c}, markClaimable: true}
	customers := &recoveryCustomerRepo{byID: map[string]*customer.Customer{
		"cust-1": {ID: "cust-1", Email: "buyer@example.com", FirstName: "Alex", Status: customer.StatusActive},
	}}
	templates := mail.NewTemplates()
	notification.RegisterTemplates(templates)
	queue := &recoveryQueue{
		byID:    map[string]struct{}{jobID: {}},
		details: map[string]*jobs.Detail{jobID: {Summary: jobs.Summary{ID: jobID, Status: jobs.StatusCancelled}}},
	}
	h := cartApp.NewRecoveryHandler(cartApp.RecoveryHandlerConfig{
		Carts:     carts,
		Customers: customers,
		Variants: &recoveryVariantRepo{byID: map[string]*catalog.Variant{
			"v1": {ID: "v1", ProductID: "p1", SKU: "SKU-1", Name: "Blue Tee"},
		}},
		Products:  &recoveryProductRepo{byID: map[string]*catalog.Product{}},
		Templates: templates,
		Queue:     queue,
		StoreURL:  "https://shop.example",
		Log:       &recoveryLogger{},
	})
	if err := h.Handle(context.Background(), jobs.Job{Type: cartApp.RecoveryJobType}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(queue.replaced) != 0 {
		t.Fatalf("cancelled must not ReplaceFailed, got %#v", queue.replaced)
	}
	if len(carts.marked) != 1 {
		t.Fatalf("cancelled must keep mark, got %v", carts.marked)
	}
}

func TestRecoveryHandler_Handle_ConflictReclaimingKeepsMark(t *testing.T) {
	c, err := domainCart.NewCart("cart-reclaiming", "EUR")
	if err != nil {
		t.Fatalf("NewCart: %v", err)
	}
	if err := c.SetCustomerID("cust-1"); err != nil {
		t.Fatalf("SetCustomerID: %v", err)
	}
	if err := c.AddItem("v1", 1, shared.MustNewMoney(999, "EUR")); err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	jobID := cartApp.CartRecoveryEmailJobID(c.ID)
	carts := &recoveryCartRepo{candidates: []domainCart.Cart{c}, markClaimable: true}
	customers := &recoveryCustomerRepo{byID: map[string]*customer.Customer{
		"cust-1": {ID: "cust-1", Email: "buyer@example.com", FirstName: "Alex", Status: customer.StatusActive},
	}}
	templates := mail.NewTemplates()
	notification.RegisterTemplates(templates)
	queue := &recoveryQueue{
		byID:    map[string]struct{}{jobID: {}},
		details: map[string]*jobs.Detail{jobID: {Summary: jobs.Summary{ID: jobID, Status: jobs.StatusReclaiming}}},
	}
	h := cartApp.NewRecoveryHandler(cartApp.RecoveryHandlerConfig{
		Carts:     carts,
		Customers: customers,
		Variants: &recoveryVariantRepo{byID: map[string]*catalog.Variant{
			"v1": {ID: "v1", ProductID: "p1", SKU: "SKU-1", Name: "Blue Tee"},
		}},
		Products:  &recoveryProductRepo{byID: map[string]*catalog.Product{}},
		Templates: templates,
		Queue:     queue,
		StoreURL:  "https://shop.example",
		Log:       &recoveryLogger{},
	})
	if err := h.Handle(context.Background(), jobs.Job{Type: cartApp.RecoveryJobType}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(queue.replaced) != 0 {
		t.Fatalf("reclaiming must not ReplaceFailed, got %#v", queue.replaced)
	}
	if len(carts.marked) != 1 {
		t.Fatalf("reclaiming must keep mark, got %v", carts.marked)
	}
}

func TestRecoveryHandler_Handle_SkipsWhenDisabled(t *testing.T) {
	c, err := domainCart.NewCart("cart-off", "EUR")
	if err != nil {
		t.Fatalf("NewCart: %v", err)
	}
	if err := c.SetCustomerID("cust-1"); err != nil {
		t.Fatalf("SetCustomerID: %v", err)
	}
	if err := c.AddItem("v1", 1, shared.MustNewMoney(999, "EUR")); err != nil {
		t.Fatalf("AddItem: %v", err)
	}

	carts := &recoveryCartRepo{candidates: []domainCart.Cart{c}, markClaimable: true}
	customers := &recoveryCustomerRepo{byID: map[string]*customer.Customer{
		"cust-1": {ID: "cust-1", Email: "buyer@example.com", FirstName: "Alex", Status: customer.StatusActive},
	}}
	settings := &stubRecoveryConfigRepo{values: map[string]interface{}{
		cartApp.ConfigKeyCartRecoveryEnabled: false,
	}}
	h, queue := testRecoveryHandlerWithSettings(t, carts, customers, settings)

	err = h.Handle(context.Background(), jobs.Job{Type: cartApp.RecoveryJobType})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(queue.enqueued) != 0 {
		t.Fatalf("expected no enqueued jobs when disabled, got %d", len(queue.enqueued))
	}
	if len(carts.marked) != 0 {
		t.Fatalf("expected no cart marks when disabled, got %v", carts.marked)
	}
}

type recoveryLayout struct {
	err error
}

func (r recoveryLayout) Wrap(body string, _ mail.EmailData) (string, error) {
	if r.err != nil {
		return "", r.err
	}
	return "<layout>" + body + "</layout>", nil
}

func TestRecoveryHandler_Handle_WrapsLayout(t *testing.T) {
	c, err := domainCart.NewCart("cart-layout", "EUR")
	if err != nil {
		t.Fatalf("NewCart: %v", err)
	}
	if err := c.SetCustomerID("cust-1"); err != nil {
		t.Fatalf("SetCustomerID: %v", err)
	}
	if err := c.AddItem("v1", 1, shared.MustNewMoney(999, "EUR")); err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	carts := &recoveryCartRepo{candidates: []domainCart.Cart{c}, markClaimable: true}
	customers := &recoveryCustomerRepo{byID: map[string]*customer.Customer{
		"cust-1": {ID: "cust-1", Email: "buyer@example.com", FirstName: "Alex", Status: customer.StatusActive},
	}}
	templates := mail.NewTemplates()
	notification.RegisterTemplates(templates)
	queue := &recoveryQueue{}
	h := cartApp.NewRecoveryHandler(cartApp.RecoveryHandlerConfig{
		Carts:     carts,
		Customers: customers,
		Variants: &recoveryVariantRepo{byID: map[string]*catalog.Variant{
			"v1": {ID: "v1", ProductID: "p1", SKU: "SKU-1", Name: "Blue Tee"},
		}},
		Products:  &recoveryProductRepo{byID: map[string]*catalog.Product{}},
		Templates: templates,
		Layout:    recoveryLayout{},
		Queue:     queue,
		StoreURL:  "https://shop.example",
		Log:       &recoveryLogger{},
	})
	if err := h.Handle(context.Background(), jobs.Job{Type: cartApp.RecoveryJobType}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(queue.enqueued) != 1 {
		t.Fatalf("enqueued = %d", len(queue.enqueued))
	}
	body, _ := queue.enqueued[0].Payload["body"].(string)
	if !strings.HasPrefix(body, "<layout>") || !strings.Contains(body, "</layout>") {
		t.Fatalf("body not wrapped: %q", body)
	}
}

func TestRecoveryHandler_Handle_WrapErrorSkipsEnqueue(t *testing.T) {
	c, err := domainCart.NewCart("cart-wrap-err", "EUR")
	if err != nil {
		t.Fatalf("NewCart: %v", err)
	}
	if err := c.SetCustomerID("cust-1"); err != nil {
		t.Fatalf("SetCustomerID: %v", err)
	}
	if err := c.AddItem("v1", 1, shared.MustNewMoney(999, "EUR")); err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	carts := &recoveryCartRepo{candidates: []domainCart.Cart{c}, markClaimable: true}
	customers := &recoveryCustomerRepo{byID: map[string]*customer.Customer{
		"cust-1": {ID: "cust-1", Email: "buyer@example.com", FirstName: "Alex", Status: customer.StatusActive},
	}}
	templates := mail.NewTemplates()
	notification.RegisterTemplates(templates)
	queue := &recoveryQueue{}
	h := cartApp.NewRecoveryHandler(cartApp.RecoveryHandlerConfig{
		Carts:     carts,
		Customers: customers,
		Variants: &recoveryVariantRepo{byID: map[string]*catalog.Variant{
			"v1": {ID: "v1", ProductID: "p1", SKU: "SKU-1", Name: "Blue Tee"},
		}},
		Products:  &recoveryProductRepo{byID: map[string]*catalog.Product{}},
		Templates: templates,
		Layout:    recoveryLayout{err: errors.New("wrap boom")},
		Queue:     queue,
		StoreURL:  "https://shop.example",
		Log:       &recoveryLogger{},
	})
	if err := h.Handle(context.Background(), jobs.Job{Type: cartApp.RecoveryJobType}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(queue.enqueued) != 0 {
		t.Fatalf("wrap error must skip enqueue, got %d", len(queue.enqueued))
	}
	if len(carts.marked) != 0 {
		t.Fatalf("wrap error must leave cart unmarked, got %v", carts.marked)
	}
}

func TestRecoveryHandler_PanicsOnNilCarts(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil carts")
		}
	}()
	cartApp.NewRecoveryHandler(cartApp.RecoveryHandlerConfig{
		Customers: &recoveryCustomerRepo{},
		Variants:  &recoveryVariantRepo{},
		Products:  &recoveryProductRepo{},
		Templates: mail.NewTemplates(),
		Queue:     &recoveryQueue{},
		Log:       &recoveryLogger{},
	})
}
