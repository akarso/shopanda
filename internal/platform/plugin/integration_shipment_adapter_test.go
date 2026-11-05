package plugin_test

import (
	"context"
	"errors"
	"testing"
	"time"

	shippingapp "github.com/akarso/shopanda/internal/application/shipping"
	"github.com/akarso/shopanda/internal/domain/shared"
	domainShipping "github.com/akarso/shopanda/internal/domain/shipping"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/extapi"
)

type stubShipRepo struct {
	sh          *domainShipping.Shipment
	updateErr   error
	updateCalls int
}

func (s *stubShipRepo) FindByID(context.Context, string) (*domainShipping.Shipment, error) {
	return nil, nil
}
func (s *stubShipRepo) FindByOrderID(_ context.Context, orderID string) (*domainShipping.Shipment, error) {
	if s.sh == nil || s.sh.OrderID != orderID {
		return nil, nil
	}
	cp := *s.sh
	return &cp, nil
}
func (s *stubShipRepo) Create(context.Context, *domainShipping.Shipment) error { return nil }
func (s *stubShipRepo) UpdateStatus(_ context.Context, sh *domainShipping.Shipment, prev time.Time) error {
	s.updateCalls++
	if s.updateErr != nil {
		return s.updateErr
	}
	if s.sh == nil || !s.sh.UpdatedAt.Equal(prev) {
		return apperror.Conflict("shipment was modified concurrently")
	}
	cp := *sh
	s.sh = &cp
	return nil
}

func TestIntegrationShipmentStatusUpdater_Ship(t *testing.T) {
	pending, err := domainShipping.NewShipment("s1", "o1", domainShipping.MethodFlatRate, shared.MustNewMoney(100, "EUR"))
	if err != nil {
		t.Fatal(err)
	}
	svc := shippingapp.NewStatusService(&stubShipRepo{sh: &pending})
	upd := plugin.NewIntegrationShipmentStatusUpdater(svc)
	res, err := upd.ApplyShipmentStatus(context.Background(), "o1", "SHIPPED", "TRK", "ref")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Status != "shipped" || res.TrackingNumber != "TRK" {
		t.Fatalf("%+v", res)
	}
	_, err = upd.ApplyShipmentStatus(context.Background(), "o1", "bogus", "", "")
	if err != extapi.ErrIntegrationShipmentInvalidStatus {
		t.Fatalf("err = %v", err)
	}
}

func TestIntegrationShipmentStatusUpdater_MissingTracking(t *testing.T) {
	pending, err := domainShipping.NewShipment("s1", "o1", domainShipping.MethodFlatRate, shared.MustNewMoney(100, "EUR"))
	if err != nil {
		t.Fatal(err)
	}
	upd := plugin.NewIntegrationShipmentStatusUpdater(shippingapp.NewStatusService(&stubShipRepo{sh: &pending}))
	_, err = upd.ApplyShipmentStatus(context.Background(), "o1", "shipped", "", "")
	if !errors.Is(err, extapi.ErrIntegrationShipmentInvalidPayload) {
		t.Fatalf("err = %v", err)
	}
}

func TestIntegrationShipmentStatusUpdater_EmptyOrderIDMapsToInvalidPayload(t *testing.T) {
	pending, err := domainShipping.NewShipment("s1", "o1", domainShipping.MethodFlatRate, shared.MustNewMoney(100, "EUR"))
	if err != nil {
		t.Fatal(err)
	}
	upd := plugin.NewIntegrationShipmentStatusUpdater(shippingapp.NewStatusService(&stubShipRepo{sh: &pending}))
	_, err = upd.ApplyShipmentStatus(context.Background(), "  ", "SHIPPED", "T", "")
	if !errors.Is(err, extapi.ErrIntegrationShipmentInvalidPayload) {
		t.Fatalf("err = %v, want InvalidPayload", err)
	}
	if errors.Is(err, extapi.ErrIntegrationShipmentInvalidTransition) {
		t.Fatal("empty order id must not map to InvalidTransition")
	}
}

func TestIntegrationShipmentStatusUpdater_TrackingAmendMapsToInvalidTransition(t *testing.T) {
	pending, err := domainShipping.NewShipment("s1", "o1", domainShipping.MethodFlatRate, shared.MustNewMoney(100, "EUR"))
	if err != nil {
		t.Fatal(err)
	}
	upd := plugin.NewIntegrationShipmentStatusUpdater(shippingapp.NewStatusService(&stubShipRepo{sh: &pending}))
	if _, err := upd.ApplyShipmentStatus(context.Background(), "o1", "SHIPPED", "T1", "ref-a"); err != nil {
		t.Fatal(err)
	}
	_, err = upd.ApplyShipmentStatus(context.Background(), "o1", "SHIPPED", "T2", "ref-a")
	if !errors.Is(err, extapi.ErrIntegrationShipmentInvalidTransition) {
		t.Fatalf("err = %v, want InvalidTransition", err)
	}
	if errors.Is(err, extapi.ErrIntegrationShipmentInvalidPayload) {
		t.Fatal("amend must not map to InvalidPayload")
	}
}

func TestIntegrationShipmentStatusUpdater_Conflict(t *testing.T) {
	pending, err := domainShipping.NewShipment("s1", "o1", domainShipping.MethodFlatRate, shared.MustNewMoney(100, "EUR"))
	if err != nil {
		t.Fatal(err)
	}
	repo := &stubShipRepo{sh: &pending, updateErr: apperror.Conflict("shipment was modified concurrently")}
	upd := plugin.NewIntegrationShipmentStatusUpdater(shippingapp.NewStatusService(repo))
	_, err = upd.ApplyShipmentStatus(context.Background(), "o1", "SHIPPED", "T", "")
	if !errors.Is(err, extapi.ErrIntegrationShipmentConflict) {
		t.Fatalf("err = %v", err)
	}
}

func TestIntegrationShipmentStatusUpdater_NotFound(t *testing.T) {
	upd := plugin.NewIntegrationShipmentStatusUpdater(shippingapp.NewStatusService(&stubShipRepo{}))
	_, err := upd.ApplyShipmentStatus(context.Background(), "missing", "SHIPPED", "T", "")
	if !errors.Is(err, extapi.ErrIntegrationShipmentNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestIntegrationShipmentStatusUpdater_DeliverCancel(t *testing.T) {
	pending, err := domainShipping.NewShipment("s1", "o1", domainShipping.MethodFlatRate, shared.MustNewMoney(100, "EUR"))
	if err != nil {
		t.Fatal(err)
	}
	repo := &stubShipRepo{sh: &pending}
	upd := plugin.NewIntegrationShipmentStatusUpdater(shippingapp.NewStatusService(repo))
	if _, err := upd.ApplyShipmentStatus(context.Background(), "o1", "SHIPPED", "T", ""); err != nil {
		t.Fatal(err)
	}
	res, err := upd.ApplyShipmentStatus(context.Background(), "o1", "DELIVERED", "", "")
	if err != nil || res.Status != "delivered" {
		t.Fatalf("%+v err=%v", res, err)
	}

	pending2, err := domainShipping.NewShipment("s2", "o2", domainShipping.MethodFlatRate, shared.MustNewMoney(100, "EUR"))
	if err != nil {
		t.Fatal(err)
	}
	upd2 := plugin.NewIntegrationShipmentStatusUpdater(shippingapp.NewStatusService(&stubShipRepo{sh: &pending2}))
	cres, err := upd2.ApplyShipmentStatus(context.Background(), "o2", "CANCELLED", "", "")
	if err != nil || cres.Status != "cancelled" {
		t.Fatalf("%+v err=%v", cres, err)
	}
}
