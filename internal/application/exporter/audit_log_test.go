package exporter_test

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/application/exporter"
	domainadmin "github.com/akarso/shopanda/internal/domain/admin"
)

type stubAuditExportRepo struct {
	entries []domainadmin.AuditLogRecord
	calls   int
}

func (s *stubAuditExportRepo) Insert(context.Context, domainadmin.AuditLogRecord) error {
	return nil
}

func (s *stubAuditExportRepo) DeleteBefore(context.Context, time.Time) (int64, error) {
	return 0, nil
}

func (s *stubAuditExportRepo) List(_ context.Context, filter domainadmin.AuditLogFilter) ([]domainadmin.AuditLogRecord, error) {
	s.calls++
	if filter.Offset >= len(s.entries) {
		return nil, nil
	}
	end := filter.Offset + filter.Limit
	if end > len(s.entries) || filter.Limit == 0 {
		end = len(s.entries)
	}
	return s.entries[filter.Offset:end], nil
}

func TestAuditLogExport_CSV(t *testing.T) {
	created := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	repo := &stubAuditExportRepo{
		entries: []domainadmin.AuditLogRecord{{
			ID:           "audit-1",
			CreatedAt:    created,
			AdminID:      "admin-1",
			Action:       "product.update",
			ResourceType: "product",
			ResourceID:   "prod-1",
			Result:       "success",
			StoreID:      "store-eu",
			Metadata:     map[string]interface{}{"name": "Updated"},
		}},
	}
	exp := exporter.NewAuditLogExporter(repo)
	var buf bytes.Buffer
	result, err := exp.Export(context.Background(), &buf, exporter.AuditLogExportOptions{
		Format: exporter.AuditLogFormatCSV,
	})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if result.Entries != 1 {
		t.Fatalf("entries = %d, want 1", result.Entries)
	}
	reader := csv.NewReader(strings.NewReader(buf.String()))
	rows, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want header + 1", len(rows))
	}
	if rows[1][0] != "audit-1" || rows[1][3] != "product.update" {
		t.Fatalf("row = %#v", rows[1])
	}
}

func TestAuditLogExport_JSON(t *testing.T) {
	repo := &stubAuditExportRepo{
		entries: []domainadmin.AuditLogRecord{{
			ID:        "audit-1",
			CreatedAt: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
			AdminID:   "admin-1",
			Action:    "audit.list",
			Result:    "success",
		}},
	}
	exp := exporter.NewAuditLogExporter(repo)
	var buf bytes.Buffer
	_, err := exp.Export(context.Background(), &buf, exporter.AuditLogExportOptions{
		Format: exporter.AuditLogFormatJSON,
	})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	var items []map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &items); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(items) != 1 || items[0]["id"] != "audit-1" {
		t.Fatalf("items = %#v", items)
	}
}

func TestAuditLogExport_BatchesLargeResultSets(t *testing.T) {
	entries := make([]domainadmin.AuditLogRecord, 0, 600)
	for i := 0; i < 600; i++ {
		entries = append(entries, domainadmin.AuditLogRecord{
			ID:     fmt.Sprintf("audit-%d", i),
			Result: "success",
		})
	}
	repo := &stubAuditExportRepo{entries: entries}
	exp := exporter.NewAuditLogExporter(repo)
	var buf bytes.Buffer
	result, err := exp.Export(context.Background(), &buf, exporter.AuditLogExportOptions{
		Format: exporter.AuditLogFormatCSV,
	})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if result.Entries != 600 {
		t.Fatalf("entries = %d, want 600", result.Entries)
	}
	if repo.calls < 2 {
		t.Fatalf("list calls = %d, want at least 2 batch reads", repo.calls)
	}
}
