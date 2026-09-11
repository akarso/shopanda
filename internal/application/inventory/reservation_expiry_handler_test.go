package inventory_test

import (
	"context"
	"errors"
	"testing"
	"time"

	inventoryApp "github.com/akarso/shopanda/internal/application/inventory"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/inventory"
	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
)

type stubVariantRepo struct {
	variants map[string]*catalog.Variant
	err      error
}

func (r *stubVariantRepo) FindByID(_ context.Context, id string) (*catalog.Variant, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.variants[id], nil
}
func (r *stubVariantRepo) FindBySKU(_ context.Context, _ string) (*catalog.Variant, error) {
	return nil, nil
}
func (r *stubVariantRepo) FindBySKUs(_ context.Context, _ []string) (map[string]*catalog.Variant, error) {
	return nil, nil
}
func (r *stubVariantRepo) ListByProductID(_ context.Context, _ string, _, _ int) ([]catalog.Variant, error) {
	return nil, nil
}
func (r *stubVariantRepo) ListByProductIDs(_ context.Context, _ []string, _ int) (map[string][]catalog.Variant, error) {
	return nil, nil
}
func (r *stubVariantRepo) Create(_ context.Context, _ *catalog.Variant) error { return nil }
func (r *stubVariantRepo) Update(_ context.Context, _ *catalog.Variant) error { return nil }

type stubReleaser struct {
	released int
	err      error
	called   bool
	cutoff   time.Time
	gotCtx   context.Context
}

func (s *stubReleaser) ReleaseExpiredBefore(ctx context.Context, cutoff time.Time) ([]inventory.ReleasedReservation, error) {
	s.called = true
	s.cutoff = cutoff
	s.gotCtx = ctx
	if s.released == 0 {
		return nil, s.err
	}
	released := make([]inventory.ReleasedReservation, s.released)
	for i := range released {
		released[i] = inventory.ReleasedReservation{VariantID: "v1", Quantity: 1}
	}
	return released, s.err
}

type errorCall struct {
	msg    string
	err    error
	fields map[string]interface{}
}

type stubLogger struct {
	infos      []string
	fields     map[string]interface{}
	errorCalls []errorCall
}

func (l *stubLogger) Info(msg string, f map[string]interface{}) {
	l.infos = append(l.infos, msg)
	l.fields = f
}

func (l *stubLogger) Error(msg string, err error, f map[string]interface{}) {
	l.errorCalls = append(l.errorCalls, errorCall{msg: msg, err: err, fields: f})
}

func TestReservationExpiryHandler_Type(t *testing.T) {
	h := inventoryApp.NewReservationExpiryHandler(&stubReleaser{}, &stubLogger{})
	if h.Type() != inventoryApp.ReservationExpiryJobType {
		t.Errorf("Type() = %q, want %q", h.Type(), inventoryApp.ReservationExpiryJobType)
	}
}

func TestReservationExpiryHandler_Handle_Success(t *testing.T) {
	r := &stubReleaser{released: 7}
	log := &stubLogger{}
	h := inventoryApp.NewReservationExpiryHandler(r, log)

	before := time.Now()
	job := jobs.Job{ID: "j1", Type: inventoryApp.ReservationExpiryJobType}
	if err := h.Handle(context.Background(), job); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if !r.called {
		t.Fatal("expected ReleaseExpiredBefore to be called")
	}
	if r.cutoff.Before(before) {
		t.Errorf("cutoff = %v, want >= %v (cutoff must be roughly now, not zero-value)", r.cutoff, before)
	}
	if len(log.infos) != 1 || log.infos[0] != "job.reservation_expiry.released" {
		t.Errorf("infos = %v, want [job.reservation_expiry.released]", log.infos)
	}
	if got, ok := log.fields["released"]; !ok {
		t.Error("expected released field in log")
	} else if got != 7 {
		t.Errorf("log.fields[released] = %v, want 7", got)
	}
	if got := log.fields["more_remaining"]; got != false {
		t.Errorf("log.fields[more_remaining] = %v, want false — an uncancelled, undeadlined context completed normally", got)
	}
}

func TestReservationExpiryHandler_Handle_Error(t *testing.T) {
	r := &stubReleaser{err: context.DeadlineExceeded}
	log := &stubLogger{}
	h := inventoryApp.NewReservationExpiryHandler(r, log)

	job := jobs.Job{ID: "j2", Type: inventoryApp.ReservationExpiryJobType}
	err := h.Handle(context.Background(), job)
	if err != context.DeadlineExceeded {
		t.Errorf("err = %v, want DeadlineExceeded", err)
	}
	if len(log.infos) != 0 {
		t.Errorf("expected no info logs on error, got %v", log.infos)
	}
}

// TestReservationExpiryHandler_Handle_OrphanedStockRestoreWarning pins the
// fix for a silently-dropped stock restore: an *inventory.OrphanedStockRestoreError
// from the releaser is logged as a warning, not treated as a job failure —
// the release itself succeeded, so Handle must still return nil (a job
// retry would find nothing left to do and just be noise).
func TestReservationExpiryHandler_Handle_OrphanedStockRestoreWarning(t *testing.T) {
	orphanErr := &inventory.OrphanedStockRestoreError{Count: 2, ReservationIDs: []string{"r1", "r2"}}
	r := &stubReleaser{released: 5, err: orphanErr}
	log := &stubLogger{}
	h := inventoryApp.NewReservationExpiryHandler(r, log)

	job := jobs.Job{ID: "j3", Type: inventoryApp.ReservationExpiryJobType}
	if err := h.Handle(context.Background(), job); err != nil {
		t.Fatalf("Handle: %v, want nil — an orphaned-restore warning must not fail the job", err)
	}
	if len(log.errorCalls) != 1 {
		t.Fatalf("errorCalls = %d, want 1", len(log.errorCalls))
	}
	if log.errorCalls[0].msg != "job.reservation_expiry.orphaned_stock_restore" {
		t.Errorf("error msg = %q, want job.reservation_expiry.orphaned_stock_restore", log.errorCalls[0].msg)
	}
	if got := log.errorCalls[0].fields["count"]; got != 2 {
		t.Errorf("error fields[count] = %v, want 2", got)
	}
	// The success log must still fire — the release count is real even
	// though some restores were orphaned.
	if len(log.infos) != 1 || log.infos[0] != "job.reservation_expiry.released" {
		t.Errorf("infos = %v, want [job.reservation_expiry.released]", log.infos)
	}
	if got := log.fields["released"]; got != 5 {
		t.Errorf("log.fields[released] = %v, want 5", got)
	}
}

// TestReservationExpiryHandler_Handle_BoundsContextDeadline pins the fix
// for an unbounded execution window: Handle must wrap the ctx it passes to
// ReleaseExpiredBefore with a deadline, not hand through a ctx that only
// ends at process shutdown (see sweepTimeout's doc comment).
func TestReservationExpiryHandler_Handle_BoundsContextDeadline(t *testing.T) {
	r := &stubReleaser{released: 0}
	log := &stubLogger{}
	h := inventoryApp.NewReservationExpiryHandler(r, log)

	// A background context has no deadline of its own — if the ctx the
	// releaser actually receives has one anyway, Handle must have added it.
	job := jobs.Job{ID: "j4", Type: inventoryApp.ReservationExpiryJobType}
	if err := h.Handle(context.Background(), job); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if r.gotCtx == nil {
		t.Fatal("releaser did not receive a context")
	}
	deadline, ok := r.gotCtx.Deadline()
	if !ok {
		t.Fatal("releaser's context has no deadline — sweep has no bounded execution window")
	}
	if until := time.Until(deadline); until <= 0 || until > 10*time.Minute {
		t.Errorf("deadline is %v from now, want a positive bound of at most 10m", until)
	}
}

// TestReservationExpiryHandler_Handle_MoreRemainingOnParentCancellation pins
// the fix for more_remaining only recognizing context.DeadlineExceeded: a
// sweep that stops early because the caller's own ctx was cancelled (e.g.
// worker shutdown), not because sweepTimeout fired, must still be reported
// as more_remaining — the underlying fact (this invocation did not finish
// draining the backlog) is the same either way, and the next attempt to
// continue is delayed, not eliminated.
func TestReservationExpiryHandler_Handle_MoreRemainingOnParentCancellation(t *testing.T) {
	r := &stubReleaser{released: 3}
	log := &stubLogger{}
	h := inventoryApp.NewReservationExpiryHandler(r, log)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // simulate the parent (worker poll-loop) ctx already being done

	job := jobs.Job{ID: "j5", Type: inventoryApp.ReservationExpiryJobType}
	if err := h.Handle(ctx, job); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(log.infos) != 1 || log.infos[0] != "job.reservation_expiry.released" {
		t.Fatalf("infos = %v, want [job.reservation_expiry.released]", log.infos)
	}
	if got := log.fields["more_remaining"]; got != true {
		t.Errorf("log.fields[more_remaining] = %v, want true on parent-ctx cancellation, not just sweepTimeout", got)
	}
}

func TestReservationExpiryHandler_PanicsOnNilReleaser(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil releaser")
		}
	}()
	inventoryApp.NewReservationExpiryHandler(nil, &stubLogger{})
}

func TestReservationExpiryHandler_PanicsOnNilLogger(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil logger")
		}
	}()
	inventoryApp.NewReservationExpiryHandler(&stubReleaser{}, nil)
}

// TestReservationExpiryHandler_Handle_PublishesStockUpdatedEvents pins
// PR-1049's reservation-expiry fix: SetEventPublishing must publish
// inventory.EventStockUpdated for every reservation ReleaseExpiredBefore
// released — the search index's on-save subscriber was previously never
// notified when a reservation expired and its stock was restored.
func TestReservationExpiryHandler_Handle_PublishesStockUpdatedEvents(t *testing.T) {
	r := &stubReleaser{released: 2}
	log := &stubLogger{}
	h := inventoryApp.NewReservationExpiryHandler(r, log)
	variants := &stubVariantRepo{variants: map[string]*catalog.Variant{
		"v1": {ID: "v1", ProductID: "p1", SKU: "SKU-1"},
	}}
	bus := event.NewBus(logger.New("error"))
	h.SetEventPublishing(variants, bus)

	var captured []event.Event
	bus.On(inventory.EventStockUpdated, func(_ context.Context, evt event.Event) error {
		captured = append(captured, evt)
		return nil
	})

	job := jobs.Job{ID: "j6", Type: inventoryApp.ReservationExpiryJobType}
	if err := h.Handle(context.Background(), job); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if len(captured) != 2 {
		t.Fatalf("published %d events, want 2 (one per released reservation)", len(captured))
	}
	for i, evt := range captured {
		data, ok := evt.Data.(inventory.StockUpdatedData)
		if !ok {
			t.Fatalf("event[%d] data type = %T, want StockUpdatedData", i, evt.Data)
		}
		if data.VariantID != "v1" || data.ProductID != "p1" || data.SKU != "SKU-1" || data.Quantity != 1 {
			t.Errorf("event[%d] data = %+v, want variant_id=v1 product_id=p1 sku=SKU-1 quantity=1", i, data)
		}
	}
}

// TestReservationExpiryHandler_Handle_NoEventPublishingByDefault pins
// that Handle works exactly as before when SetEventPublishing was never
// called — the default for every existing caller/test that doesn't need
// eventing, matching InventoryAdminHandler.SetBus's own convention.
func TestReservationExpiryHandler_Handle_NoEventPublishingByDefault(t *testing.T) {
	r := &stubReleaser{released: 2}
	log := &stubLogger{}
	h := inventoryApp.NewReservationExpiryHandler(r, log)

	job := jobs.Job{ID: "j7", Type: inventoryApp.ReservationExpiryJobType}
	if err := h.Handle(context.Background(), job); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(log.infos) != 1 || log.infos[0] != "job.reservation_expiry.released" {
		t.Fatalf("infos = %v, want [job.reservation_expiry.released]", log.infos)
	}
}

// TestReservationExpiryHandler_Handle_SkipsPublishOnVariantLookupError
// pins that a failed variant lookup for one released reservation is
// swallowed, not surfaced as a job failure — the release itself already
// succeeded, so a stale search index entry for that one variant is not
// worth retrying (and re-releasing nothing) the whole sweep over.
func TestReservationExpiryHandler_Handle_SkipsPublishOnVariantLookupError(t *testing.T) {
	r := &stubReleaser{released: 1}
	log := &stubLogger{}
	h := inventoryApp.NewReservationExpiryHandler(r, log)
	variants := &stubVariantRepo{err: errors.New("db down")}
	bus := event.NewBus(logger.New("error"))
	h.SetEventPublishing(variants, bus)

	var captured []event.Event
	bus.On(inventory.EventStockUpdated, func(_ context.Context, evt event.Event) error {
		captured = append(captured, evt)
		return nil
	})

	job := jobs.Job{ID: "j8", Type: inventoryApp.ReservationExpiryJobType}
	if err := h.Handle(context.Background(), job); err != nil {
		t.Fatalf("Handle: %v, want nil despite the variant lookup failing", err)
	}
	if len(captured) != 0 {
		t.Errorf("published %d events, want 0 (variant lookup failed)", len(captured))
	}
}
