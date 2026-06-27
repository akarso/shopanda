package exporter_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/application/exporter"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/legal"
)

type stubEprConfig map[string]interface{}

func (s stubEprConfig) Get(_ context.Context, key string) (interface{}, error) {
	if s == nil {
		return nil, nil
	}
	v, ok := s[key]
	if !ok {
		return nil, nil
	}
	return v, nil
}

func TestEprExport_WritesPackagingRows(t *testing.T) {
	prodRepo := &mockProductRepo{
		products: []catalog.Product{
			{
				ID:   "p1",
				Name: "Cable",
				Slug: "usb-c-cable",
				Attributes: map[string]interface{}{
					legal.AttrEprPackagingMaterial:  "plastic",
					legal.AttrEprRecyclable:         true,
					legal.AttrEprRecycledContentPct: 25,
				},
			},
			{ID: "p2", Name: "Shirt", Slug: "tshirt"},
		},
	}
	varRepo := &mockVariantRepo{
		variants: map[string][]catalog.Variant{
			"p1": {{ID: "v1", ProductID: "p1", SKU: "USBC-1M", Attributes: map[string]interface{}{
				legal.AttrEprPackagingWeightG: 45,
			}}},
			"p2": {{ID: "v2", ProductID: "p2", SKU: "TSHIRT-M"}},
		},
	}
	cfg := stubEprConfig{
		legal.ScopedConfigKey("store-de", legal.EprSchemeRegistrationConfigKey): "DE-LUCID-99",
	}

	exp := exporter.NewEprExporter(prodRepo, varRepo, cfg)
	var buf bytes.Buffer
	result, err := exp.Export(context.Background(), &buf, exporter.EprExportOptions{StoreID: "store-de"})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if result.Rows != 2 {
		t.Fatalf("rows = %d, want 2 (cable + store-scheme-only shirt)", result.Rows)
	}

	records := parseCSV(t, &buf)
	if len(records) != 3 {
		t.Fatalf("csv rows = %d, want header + 2 data rows", len(records))
	}
	if records[1][0] != "USBC-1M" || records[1][3] != "plastic" || records[1][4] != "45" {
		t.Fatalf("unexpected row: %v", records[1])
	}
	if records[1][7] != "DE-LUCID-99" {
		t.Fatalf("scheme = %q", records[1][7])
	}
}

func TestEprExport_IncludeEmpty(t *testing.T) {
	prodRepo := &mockProductRepo{
		products: []catalog.Product{{ID: "p1", Name: "Shirt", Slug: "tshirt"}},
	}
	varRepo := &mockVariantRepo{
		variants: map[string][]catalog.Variant{
			"p1": {{ID: "v1", ProductID: "p1", SKU: "TSHIRT-M"}},
		},
	}

	exp := exporter.NewEprExporter(prodRepo, varRepo, nil)
	var buf bytes.Buffer
	result, err := exp.Export(context.Background(), &buf, exporter.EprExportOptions{IncludeEmpty: true})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if result.Rows != 1 {
		t.Fatalf("rows = %d, want 1", result.Rows)
	}
	body := buf.String()
	if !strings.Contains(body, "TSHIRT-M") {
		t.Fatalf("missing sku in csv: %s", body)
	}
}
