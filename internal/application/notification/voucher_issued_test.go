package notification_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/application/notification"
	"github.com/akarso/shopanda/internal/domain/customer"
	"github.com/akarso/shopanda/internal/domain/mail"
	"github.com/akarso/shopanda/internal/domain/order"
	domainvoucher "github.com/akarso/shopanda/internal/domain/voucher"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/id"
)

type memVoucherListRepo struct {
	rows []domainvoucher.Voucher
}

func (m *memVoucherListRepo) Insert(context.Context, *domainvoucher.Voucher) error { return nil }
func (m *memVoucherListRepo) UpdateCounts(context.Context, string, int, int) error {
	return nil
}
func (m *memVoucherListRepo) Void(context.Context, string, time.Time) error { return nil }
func (m *memVoucherListRepo) VoidForOrderItem(context.Context, string, time.Time) ([]domainvoucher.Voucher, error) {
	return nil, nil
}
func (m *memVoucherListRepo) LinkCustomer(context.Context, string, string) error { return nil }
func (m *memVoucherListRepo) FindByID(context.Context, string) (*domainvoucher.Voucher, error) {
	return nil, nil
}
func (m *memVoucherListRepo) FindByCode(context.Context, string) (*domainvoucher.Voucher, error) {
	return nil, nil
}
func (m *memVoucherListRepo) List(context.Context, domainvoucher.ListFilter) ([]domainvoucher.Voucher, error) {
	return nil, nil
}
func (m *memVoucherListRepo) ListByOrderID(context.Context, string) ([]domainvoucher.Voucher, error) {
	return m.rows, nil
}

type stubVoucherPDF struct{}

func (stubVoucherPDF) Render(domainvoucher.PDFDocument) ([]byte, error) {
	return []byte("%PDF-voucher"), nil
}

type recordingDedupe struct {
	keys    []string
	claimed map[string]struct{}
}

func (r *recordingDedupe) TryInsert(_ context.Context, subjectKey, kind string) (bool, error) {
	if r.claimed == nil {
		r.claimed = map[string]struct{}{}
	}
	k := subjectKey + "|" + kind
	r.keys = append(r.keys, k)
	if len(subjectKey) > 2700 {
		return false, fmt.Errorf("subject_key too long for btree: %d bytes", len(subjectKey))
	}
	if _, ok := r.claimed[k]; ok {
		return false, nil
	}
	r.claimed[k] = struct{}{}
	return true, nil
}
func (r *recordingDedupe) MarkCompleted(context.Context, string, string) error { return nil }
func (r *recordingDedupe) Delete(_ context.Context, subjectKey, kind string) error {
	if r.claimed != nil {
		delete(r.claimed, subjectKey+"|"+kind)
	}
	return nil
}

func TestHandleVoucherIssued_HundredCodesDedupeFitsBtree(t *testing.T) {
	tmpl := mail.NewTemplates()
	notification.RegisterTemplates(tmpl)

	ids := make([]string, 100)
	rows := make([]domainvoucher.Voucher, 100)
	now := time.Now().UTC()
	for i := 0; i < 100; i++ {
		ids[i] = id.New()
		code, err := domainvoucher.GenerateCode()
		if err != nil {
			t.Fatalf("GenerateCode: %v", err)
		}
		v, err := domainvoucher.NewVoucher(ids[i], code, "ord-100", "ord-100/var-1", id.New(), "var-1", "cust-1", i, 1, now)
		if err != nil {
			t.Fatalf("NewVoucher: %v", err)
		}
		rows[i] = *v
	}

	q := &mockQueue{ignoreID: true}
	dedupe := &recordingDedupe{}
	custRepo := &mockCustomerRepo{
		findByID: func(_ context.Context, cid string) (*customer.Customer, error) {
			c, _ := customer.NewCustomer(cid, "buyer@example.com")
			return &c, nil
		},
	}
	ordRepo := &mockOrderRepo{
		findByID: func(_ context.Context, oid string) (*order.Order, error) {
			return &order.Order{ID: oid, CustomerID: "cust-1"}, nil
		},
	}
	svc := newTestService(t, tmpl, custRepo, ordRepo, q,
		notification.WithEmailDedupe(dedupe),
		notification.WithVouchers(&memVoucherListRepo{rows: rows}),
		notification.WithVoucherPDF(stubVoucherPDF{}),
	)

	err := svc.HandleVoucherIssued(context.Background(), event.New(
		domainvoucher.EventVoucherIssued, "test",
		domainvoucher.IssuedData{OrderID: "ord-100", CustomerID: "cust-1", VoucherIDs: ids},
	))
	if err != nil {
		t.Fatalf("HandleVoucherIssued: %v", err)
	}
	if len(q.enqueued) != 1 {
		t.Fatalf("enqueued = %d, want 1", len(q.enqueued))
	}
	if len(dedupe.keys) != 1 {
		t.Fatalf("dedupe claims = %d, want 1", len(dedupe.keys))
	}
	subject := dedupe.keys[0]
	if len(subject) > 2700 {
		t.Fatalf("subject_key length %d exceeds btree limit", len(subject))
	}
	// Fixed-length UUID form: 36 chars + "|" + kind
	if len(subject) > 80 {
		t.Fatalf("subject_key %q looks like raw id join, want hashed UUID", subject)
	}
}

func TestResendVoucherIssued_ForceEnqueuesNewJobWhenOriginalJobIDStillQueued(t *testing.T) {
	tmpl := mail.NewTemplates()
	notification.RegisterTemplates(tmpl)

	vid := id.New()
	code, err := domainvoucher.GenerateCode()
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}
	v, err := domainvoucher.NewVoucher(vid, code, "ord-force", "ord-force/var-1", id.New(), "var-1", "cust-1", 0, 1, time.Now().UTC())
	if err != nil {
		t.Fatalf("NewVoucher: %v", err)
	}
	rows := []domainvoucher.Voucher{*v}

	q := &mockQueue{} // conflict on duplicate job ids
	dedupe := &recordingDedupe{}
	custRepo := &mockCustomerRepo{
		findByID: func(_ context.Context, cid string) (*customer.Customer, error) {
			c, _ := customer.NewCustomer(cid, "buyer@example.com")
			return &c, nil
		},
	}
	ordRepo := &mockOrderRepo{
		findByID: func(_ context.Context, oid string) (*order.Order, error) {
			return &order.Order{ID: oid, CustomerID: "cust-1"}, nil
		},
	}
	svc := newTestService(t, tmpl, custRepo, ordRepo, q,
		notification.WithEmailDedupe(dedupe),
		notification.WithVouchers(&memVoucherListRepo{rows: rows}),
		notification.WithVoucherPDF(stubVoucherPDF{}),
	)

	if err := svc.HandleVoucherIssued(context.Background(), event.New(
		domainvoucher.EventVoucherIssued, "test",
		domainvoucher.IssuedData{OrderID: "ord-force", CustomerID: "cust-1", VoucherIDs: []string{vid}},
	)); err != nil {
		t.Fatalf("first HandleVoucherIssued: %v", err)
	}
	if len(q.enqueued) != 1 {
		t.Fatalf("after first send enqueued = %d, want 1", len(q.enqueued))
	}
	firstJobID := q.enqueued[0].ID

	// Simulate lost mail: first job still in queue / terminal, staff force-resends.
	if err := svc.ResendVoucherIssued(context.Background(), "ord-force", true); err != nil {
		t.Fatalf("ResendVoucherIssued force: %v", err)
	}
	if len(q.enqueued) != 2 {
		t.Fatalf("after force resend enqueued = %d, want 2", len(q.enqueued))
	}
	if q.enqueued[1].ID == firstJobID {
		t.Fatalf("force resend reused job id %s — must enqueue a new pending job", firstJobID)
	}
}
