package exporter_test

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/application/exporter"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/config"
)

// --- config mock for attribute tests ---

type mockConfigRepoForAttrExport struct {
	store  map[string]interface{}
	getErr error
}

func newMockConfigRepoForAttrExport() *mockConfigRepoForAttrExport {
	return &mockConfigRepoForAttrExport{store: make(map[string]interface{})}
}

func (m *mockConfigRepoForAttrExport) Get(_ context.Context, key string) (interface{}, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	return m.store[key], nil
}
func (m *mockConfigRepoForAttrExport) Set(_ context.Context, key string, value interface{}) error {
	m.store[key] = value
	return nil
}
func (m *mockConfigRepoForAttrExport) Delete(_ context.Context, key string) error {
	delete(m.store, key)
	return nil
}
func (m *mockConfigRepoForAttrExport) All(_ context.Context) ([]config.Entry, error) {
	entries := make([]config.Entry, 0, len(m.store))
	for k, v := range m.store {
		entries = append(entries, config.Entry{Key: k, Value: v})
	}
	return entries, nil
}

// --- tests ---

func TestAttrExport_Basic(t *testing.T) {
	repo := newMockConfigRepoForAttrExport()
	repo.store["catalog.attributes"] = []catalog.Attribute{
		{Code: "color", Label: "Color", Type: catalog.AttributeTypeSelect, Required: true, Options: []string{"red", "blue"}},
		{Code: "weight", Label: "Weight", Type: catalog.AttributeTypeNumber},
	}
	repo.store["catalog.attribute_groups"] = []catalog.AttributeGroup{
		{Code: "apparel", Label: "Apparel", Attributes: []string{"color"}},
	}

	exp := exporter.NewAttributeExporter(repo)
	var buf bytes.Buffer
	result, err := exp.Export(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if result.Entries != 2 {
		t.Errorf("Entries = %d, want 2", result.Entries)
	}

	records := parseCSV(t, &buf)
	if len(records) != 3 { // header + 2 rows
		t.Fatalf("rows = %d, want 3", len(records))
	}
	if strings.Join(records[0], ",") != "code,label,type,required,options,group,group_label" {
		t.Errorf("header = %v", records[0])
	}
	// First data row is color (sorted by code).
	if records[1][0] != "color" {
		t.Errorf("row1 code = %q, want color", records[1][0])
	}
	if records[1][3] != "true" {
		t.Errorf("row1 required = %q, want true", records[1][3])
	}
	if records[1][5] != "apparel" {
		t.Errorf("row1 group = %q, want apparel", records[1][5])
	}
	// Second row is weight (ungrouped).
	if records[2][0] != "weight" {
		t.Errorf("row2 code = %q, want weight", records[2][0])
	}
	if records[2][5] != "" {
		t.Errorf("row2 group = %q, want empty", records[2][5])
	}
}

func TestAttrExport_Empty(t *testing.T) {
	repo := newMockConfigRepoForAttrExport()

	exp := exporter.NewAttributeExporter(repo)
	var buf bytes.Buffer
	result, err := exp.Export(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if result.Entries != 0 {
		t.Errorf("Entries = %d, want 0", result.Entries)
	}

	records := parseCSV(t, &buf)
	if len(records) != 1 { // header only
		t.Fatalf("rows = %d, want 1 (header only)", len(records))
	}
}

func TestAttrExport_MultiGroup(t *testing.T) {
	repo := newMockConfigRepoForAttrExport()
	repo.store["catalog.attributes"] = []catalog.Attribute{
		{Code: "color", Label: "Color", Type: catalog.AttributeTypeSelect, Options: []string{"red"}},
	}
	repo.store["catalog.attribute_groups"] = []catalog.AttributeGroup{
		{Code: "apparel", Label: "Apparel", Attributes: []string{"color"}},
		{Code: "display", Label: "Display", Attributes: []string{"color"}},
	}

	exp := exporter.NewAttributeExporter(repo)
	var buf bytes.Buffer
	result, err := exp.Export(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	// color appears in two groups -> two rows.
	if result.Entries != 2 {
		t.Errorf("Entries = %d, want 2", result.Entries)
	}

	records := parseCSV(t, &buf)
	if len(records) != 3 { // header + 2
		t.Fatalf("rows = %d, want 3", len(records))
	}
	if records[1][5] != "apparel" {
		t.Errorf("row1 group = %q, want apparel", records[1][5])
	}
	if records[2][5] != "display" {
		t.Errorf("row2 group = %q, want display", records[2][5])
	}
}

func TestAttrExport_ConfigError(t *testing.T) {
	repo := newMockConfigRepoForAttrExport()
	repo.getErr = fmt.Errorf("db down")

	exp := exporter.NewAttributeExporter(repo)
	var buf bytes.Buffer
	_, err := exp.Export(context.Background(), &buf)
	if err == nil {
		t.Fatal("expected error on config read failure")
	}
}

func TestAttrExport_FormulaInjection(t *testing.T) {
	repo := newMockConfigRepoForAttrExport()
	repo.store["catalog.attributes"] = []catalog.Attribute{
		{Code: "=cmd", Label: "+Inject", Type: catalog.AttributeTypeSelect, Options: []string{"=evil"}},
	}

	exp := exporter.NewAttributeExporter(repo)
	var buf bytes.Buffer
	_, err := exp.Export(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}

	records := parseCSV(t, &buf)
	if len(records) < 2 {
		t.Fatal("expected at least 2 rows")
	}
	if !strings.HasPrefix(records[1][0], "'") {
		t.Errorf("code = %q, expected formula injection prefix", records[1][0])
	}
	if !strings.HasPrefix(records[1][1], "'") {
		t.Errorf("label = %q, expected formula injection prefix", records[1][1])
	}
	if !strings.HasPrefix(records[1][4], "'") {
		t.Errorf("options = %q, expected formula injection prefix", records[1][4])
	}
}
