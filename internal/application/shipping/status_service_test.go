package shipping_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	shippingapp "github.com/akarso/shopanda/internal/application/shipping"
	"github.com/akarso/shopanda/internal/domain/shared"
	domainShipping "github.com/akarso/shopanda/internal/domain/shipping"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
)

func requireAppCode(t *testing.T, err error, code apperror.Code) {
	t.Helper()
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != code {
		t.Fatalf("err = %v, want code %s", err, code)
	}
}

type memShipments struct {
	mu   sync.Mutex
	byID map[string]*domainShipping.Shipment
}

func newMemShipments(s ...*domainShipping.Shipment) *memShipments {
	m := &memShipments{byID: make(map[string]*domainShipping.Shipment)}
	for _, sh := range s {
		cp := *sh
		m.byID[sh.ID] = &cp
	}
	return m
}

func (m *memShipments) FindByID(_ context.Context, id string) (*domainShipping.Shipment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	sh, ok := m.byID[id]
	if !ok {
		return nil, nil
	}
	cp := *sh
	return &cp, nil
}

func (m *memShipments) FindByOrderID(_ context.Context, orderID string) (*domainShipping.Shipment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, sh := range m.byID {
		if sh.OrderID == orderID {
			cp := *sh
			return &cp, nil
		}
	}
	return nil, nil
}

func (m *memShipments) Create(_ context.Context, s *domainShipping.Shipment) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *s
	m.byID[s.ID] = &cp
	return nil
}

func (m *memShipments) UpdateStatus(_ context.Context, s *domainShipping.Shipment, prevUpdatedAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.byID[s.ID]
	if !ok {
		return errors.New("not found")
	}
	if !cur.UpdatedAt.Equal(prevUpdatedAt) {
		return errors.New("conflict")
	}
	cp := *s
	m.byID[s.ID] = &cp
	return nil
}

func mustPending(t *testing.T) *domainShipping.Shipment {
	t.Helper()
	s, err := domainShipping.NewShipment("ship-1", "ord-1", domainShipping.MethodFlatRate, shared.MustNewMoney(500, "EUR"))
	if err != nil {
		t.Fatal(err)
	}
	return &s
}

func TestStatusService_MarkShipped(t *testing.T) {
	sh := mustPending(t)
	repo := newMemShipments(sh)
	bus := event.NewBus(logger.New("error"))
	var names []string
	bus.On(domainShipping.EventShipmentShipped, func(_ context.Context, evt event.Event) error {
		names = append(names, evt.Name)
		data := evt.Data.(domainShipping.ShipmentStatusChangedData)
		if data.TrackingNumber != "TRACK-1" || data.OrderID != "ord-1" {
			t.Fatalf("payload = %+v", data)
		}
		return nil
	})
	svc := shippingapp.NewStatusService(repo).WithEventBus(bus)

	res, err := svc.MarkShipped(context.Background(), "ord-1", "TRACK-1", "carrier-ref")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Shipment.Status() != domainShipping.StatusShipped {
		t.Fatalf("result = %+v", res)
	}
	if len(names) != 1 {
		t.Fatalf("events = %v", names)
	}
	// Idempotent.
	res2, err := svc.MarkShipped(context.Background(), "ord-1", "TRACK-1", "carrier-ref")
	if err != nil || res2.Changed {
		t.Fatalf("idempotent = %+v err=%v", res2, err)
	}
	if len(names) != 1 {
		t.Fatalf("republish = %v", names)
	}
}

func TestStatusService_MarkShipped_RequiresTracking(t *testing.T) {
	svc := shippingapp.NewStatusService(newMemShipments(mustPending(t)))
	_, err := svc.MarkShipped(context.Background(), "ord-1", "  ", "")
	requireAppCode(t, err, apperror.CodeValidation)
	if !errors.Is(err, shippingapp.ErrTrackingRequired) {
		t.Fatalf("err = %v, want ErrTrackingRequired", err)
	}
}

func TestStatusService_MarkShipped_RejectsTrackingAmend(t *testing.T) {
	sh := mustPending(t)
	svc := shippingapp.NewStatusService(newMemShipments(sh))
	if _, err := svc.MarkShipped(context.Background(), "ord-1", "TRACK-1", "ref-a"); err != nil {
		t.Fatal(err)
	}
	_, err := svc.MarkShipped(context.Background(), "ord-1", "TRACK-2", "ref-a")
	requireAppCode(t, err, apperror.CodeValidation)
	if !errors.Is(err, shippingapp.ErrTrackingAmendNotSupported) {
		t.Fatalf("err = %v, want ErrTrackingAmendNotSupported", err)
	}
	_, err = svc.MarkShipped(context.Background(), "ord-1", "TRACK-1", "ref-b")
	if !errors.Is(err, shippingapp.ErrTrackingAmendNotSupported) {
		t.Fatalf("provider_ref mismatch err = %v", err)
	}
}

func TestStatusService_MarkShipped_IdempotentOmitsProviderRef(t *testing.T) {
	sh := mustPending(t)
	svc := shippingapp.NewStatusService(newMemShipments(sh))
	if _, err := svc.MarkShipped(context.Background(), "ord-1", "TRACK-1", "carrier-ref"); err != nil {
		t.Fatal(err)
	}
	res, err := svc.MarkShipped(context.Background(), "ord-1", "TRACK-1", "")
	if err != nil || res.Changed {
		t.Fatalf("omitted provider_ref should no-op: %+v err=%v", res, err)
	}
	if res.Shipment.ProviderRef != "carrier-ref" {
		t.Fatalf("stored provider_ref cleared: %q", res.Shipment.ProviderRef)
	}
}

func TestStatusService_UpdateStatusConflictReturnsUnchangedShipment(t *testing.T) {
	sh := mustPending(t)
	repo := &conflictShipments{inner: newMemShipments(sh)}
	svc := shippingapp.NewStatusService(repo)
	res, err := svc.MarkShipped(context.Background(), "ord-1", "TRACK-1", "")
	requireAppCode(t, err, apperror.CodeConflict)
	if res.Shipment == nil || res.Shipment.Status() != domainShipping.StatusPending {
		t.Fatalf("shipment after conflict = %+v", res.Shipment)
	}
}

type conflictShipments struct {
	inner *memShipments
}

func (c *conflictShipments) FindByID(ctx context.Context, id string) (*domainShipping.Shipment, error) {
	return c.inner.FindByID(ctx, id)
}
func (c *conflictShipments) FindByOrderID(ctx context.Context, orderID string) (*domainShipping.Shipment, error) {
	return c.inner.FindByOrderID(ctx, orderID)
}
func (c *conflictShipments) Create(ctx context.Context, s *domainShipping.Shipment) error {
	return c.inner.Create(ctx, s)
}
func (c *conflictShipments) UpdateStatus(context.Context, *domainShipping.Shipment, time.Time) error {
	return apperror.Conflict("shipment was modified concurrently")
}

func TestStatusService_MarkShipped_NotFound(t *testing.T) {
	svc := shippingapp.NewStatusService(newMemShipments())
	_, err := svc.MarkShipped(context.Background(), "missing", "T", "")
	requireAppCode(t, err, apperror.CodeNotFound)
}

func TestStatusService_DeliverAndCancel(t *testing.T) {
	sh := mustPending(t)
	repo := newMemShipments(sh)
	bus := event.NewBus(logger.New("error"))
	var names []string
	for _, n := range []string{domainShipping.EventShipmentShipped, domainShipping.EventShipmentDelivered, domainShipping.EventShipmentCancelled} {
		name := n
		bus.On(name, func(_ context.Context, evt event.Event) error {
			names = append(names, evt.Name)
			return nil
		})
	}
	svc := shippingapp.NewStatusService(repo).WithEventBus(bus)

	if _, err := svc.MarkShipped(context.Background(), "ord-1", "T1", ""); err != nil {
		t.Fatal(err)
	}
	res, err := svc.MarkDelivered(context.Background(), "ord-1")
	if err != nil || !res.Changed || res.Shipment.Status() != domainShipping.StatusDelivered {
		t.Fatalf("deliver = %+v err=%v", res, err)
	}

	pending := mustPending(t)
	pending.ID = "ship-2"
	pending.OrderID = "ord-2"
	repo2 := newMemShipments(pending)
	svc2 := shippingapp.NewStatusService(repo2).WithEventBus(bus)
	cres, err := svc2.Cancel(context.Background(), "ord-2")
	if err != nil || !cres.Changed || cres.Shipment.Status() != domainShipping.StatusCancelled {
		t.Fatalf("cancel = %+v err=%v", cres, err)
	}
}

func TestStatusService_InvalidTransition(t *testing.T) {
	sh := mustPending(t)
	svc := shippingapp.NewStatusService(newMemShipments(sh))
	_, err := svc.MarkDelivered(context.Background(), "ord-1")
	requireAppCode(t, err, apperror.CodeValidation)
}

func TestStatusService_PublishErrorDoesNotFail(t *testing.T) {
	sh := mustPending(t)
	bus := event.NewBus(logger.New("error"))
	bus.On(domainShipping.EventShipmentShipped, func(context.Context, event.Event) error {
		return errors.New("sync handler down")
	})
	svc := shippingapp.NewStatusService(newMemShipments(sh)).
		WithEventBus(bus).
		WithLogger(logger.NewWithWriter(ioDiscard{}, "error"))
	res, err := svc.MarkShipped(context.Background(), "ord-1", "T1", "")
	if err != nil || !res.Changed {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }
