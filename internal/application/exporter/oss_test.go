package exporter_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/application/exporter"
	"github.com/akarso/shopanda/internal/domain/order"
)

type stubOssOrderRepo struct {
	rows []order.TaxSnapshotRow
	err  error
}

func (s *stubOssOrderRepo) FindByID(context.Context, string) (*order.Order, error) { return nil, nil }
func (s *stubOssOrderRepo) FindByCustomerID(context.Context, string) ([]order.Order, error) {
	return nil, nil
}
func (s *stubOssOrderRepo) FindByContactEmail(context.Context, string) ([]order.Order, error) {
	return nil, nil
}
func (s *stubOssOrderRepo) List(context.Context, int, int) ([]order.Order, error) { return nil, nil }
func (s *stubOssOrderRepo) Save(context.Context, *order.Order) error               { return nil }
func (s *stubOssOrderRepo) UpdateStatus(context.Context, *order.Order) error       { return nil }
func (s *stubOssOrderRepo) LinkToCustomer(context.Context, *order.Order) error   { return nil }
func (s *stubOssOrderRepo) LinkToCustomerByContactEmail(context.Context, string, string, time.Time) (int64, error) {
	return 0, nil
}
func (s *stubOssOrderRepo) ListPaidTaxSnapshots(_ context.Context, _, _ time.Time) ([]order.TaxSnapshotRow, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.rows, nil
}

func TestOssExport_DetailRows(t *testing.T) {
	created := time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC)
	repo := &stubOssOrderRepo{
		rows: []order.TaxSnapshotRow{{
			OrderID:            "ord-1",
			CreatedAt:          created,
			DestinationCountry: "FR",
			Currency:           "EUR",
			SubtotalAmount:     10000,
			TaxAmount:          2000,
		}},
	}
	exp := exporter.NewOssExporter(repo)
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)

	var buf bytes.Buffer
	result, err := exp.Export(context.Background(), &buf, exporter.OssExportOptions{From: from, To: to})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if result.Rows != 1 {
		t.Fatalf("rows = %d, want 1", result.Rows)
	}
	body := buf.String()
	if !strings.Contains(body, "ord-1") || !strings.Contains(body, "FR") || !strings.Contains(body, "100.00") {
		t.Fatalf("unexpected csv: %s", body)
	}
}

func TestOssExport_SummaryRows(t *testing.T) {
	repo := &stubOssOrderRepo{
		rows: []order.TaxSnapshotRow{
			{OrderID: "o1", DestinationCountry: "FR", Currency: "EUR", SubtotalAmount: 1000, TaxAmount: 200},
			{OrderID: "o2", DestinationCountry: "FR", Currency: "EUR", SubtotalAmount: 2000, TaxAmount: 400},
			{OrderID: "o3", DestinationCountry: "DE", Currency: "EUR", SubtotalAmount: 500, TaxAmount: 95},
		},
	}
	exp := exporter.NewOssExporter(repo)
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)

	var buf bytes.Buffer
	result, err := exp.Export(context.Background(), &buf, exporter.OssExportOptions{From: from, To: to, Summary: true})
	if err != nil {
		t.Fatalf("Export summary: %v", err)
	}
	if result.Rows != 2 {
		t.Fatalf("summary rows = %d, want 2", result.Rows)
	}
	records := parseCSV(t, &buf)
	if len(records) != 3 {
		t.Fatalf("csv rows = %d", len(records))
	}
	if records[1][0] != "DE" || records[1][2] != "1" {
		t.Fatalf("unexpected DE summary row: %v", records[1])
	}
	if records[2][0] != "FR" || records[2][2] != "2" || records[2][3] != "30.00" {
		t.Fatalf("unexpected FR summary row: %v", records[2])
	}
}
