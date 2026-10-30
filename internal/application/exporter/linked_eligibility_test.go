package exporter_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"strings"
	"testing"

	catalogApp "github.com/akarso/shopanda/internal/application/catalog"
	"github.com/akarso/shopanda/internal/application/exporter"
	"github.com/akarso/shopanda/internal/domain/catalog"
)

type linkedExportRuleRepo struct {
	catalog.LinkedEligibilityRepository
	parents    []catalog.LinkedEligibleParentSlugRow
	categories []catalog.LinkedEligibleCategorySlugRow
}

func (r *linkedExportRuleRepo) ListParentsByChildID(context.Context, string) ([]catalog.LinkedEligibleParent, error) {
	return nil, nil
}
func (r *linkedExportRuleRepo) ListCategoriesByChildID(context.Context, string) ([]catalog.LinkedEligibleCategory, error) {
	return nil, nil
}
func (r *linkedExportRuleRepo) CountRulesByChildID(context.Context, string) (int, error) {
	return 0, nil
}
func (r *linkedExportRuleRepo) InsertParent(context.Context, *catalog.LinkedEligibleParent) error {
	return nil
}
func (r *linkedExportRuleRepo) InsertCategory(context.Context, *catalog.LinkedEligibleCategory) error {
	return nil
}
func (r *linkedExportRuleRepo) RemoveParent(context.Context, string, string) error   { return nil }
func (r *linkedExportRuleRepo) RemoveCategory(context.Context, string, string) error { return nil }
func (r *linkedExportRuleRepo) ListAllParentSlugRows(context.Context) ([]catalog.LinkedEligibleParentSlugRow, error) {
	return append([]catalog.LinkedEligibleParentSlugRow{}, r.parents...), nil
}
func (r *linkedExportRuleRepo) ListAllCategorySlugRows(context.Context) ([]catalog.LinkedEligibleCategorySlugRow, error) {
	return append([]catalog.LinkedEligibleCategorySlugRow{}, r.categories...), nil
}
func (r *linkedExportRuleRepo) IsEligible(context.Context, string, string) (bool, error) {
	return false, nil
}
func (r *linkedExportRuleRepo) WithTx(*sql.Tx) catalog.LinkedEligibilityRepository { return r }

type linkedExportCategoryRepo struct {
	catalog.CategoryRepository
}

func (r *linkedExportCategoryRepo) FindByID(context.Context, string) (*catalog.Category, error) {
	return nil, nil
}

type linkedExportProductRepo struct {
	catalog.ProductRepository
}

func (r *linkedExportProductRepo) FindByID(context.Context, string) (*catalog.Product, error) {
	return nil, nil
}
func (r *linkedExportProductRepo) List(context.Context, catalog.ListFilter) ([]catalog.Product, error) {
	return nil, nil
}
func (r *linkedExportProductRepo) WithTx(*sql.Tx) catalog.ProductRepository { return r }

func TestLinkedEligibleExporters_RoundTripSlugs(t *testing.T) {
	rules := &linkedExportRuleRepo{
		parents: []catalog.LinkedEligibleParentSlugRow{
			{ChildSlug: "ticket", ParentSlug: "card"},
			{ChildSlug: "pass", ParentSlug: "wallet"},
		},
		categories: []catalog.LinkedEligibleCategorySlugRow{
			{ChildSlug: "ticket", CategorySlug: "cards"},
		},
	}
	products := &linkedExportProductRepo{}
	categories := &linkedExportCategoryRepo{}
	svc := catalogApp.NewLinkedEligibilityService(products, categories, rules, exportCompositionTxStarter())

	var parentBuf bytes.Buffer
	parentResult, err := exporter.NewLinkedEligibleParentExporter(products, svc).Export(context.Background(), &parentBuf)
	if err != nil {
		t.Fatalf("parent Export: %v", err)
	}
	if parentResult.Rows != 2 {
		t.Fatalf("parent rows = %d, want 2", parentResult.Rows)
	}
	parentRecords, err := csv.NewReader(strings.NewReader(parentBuf.String())).ReadAll()
	if err != nil {
		t.Fatalf("parse parent csv: %v", err)
	}
	if strings.Join(parentRecords[0], ",") != "child_slug,parent_slug" {
		t.Fatalf("parent header = %v", parentRecords[0])
	}
	if parentRecords[1][0] != "ticket" || parentRecords[1][1] != "card" {
		t.Fatalf("parent row1 = %v", parentRecords[1])
	}

	var catBuf bytes.Buffer
	catResult, err := exporter.NewLinkedEligibleCategoryExporter(products, categories, svc).Export(context.Background(), &catBuf)
	if err != nil {
		t.Fatalf("category Export: %v", err)
	}
	if catResult.Rows != 1 {
		t.Fatalf("category rows = %d, want 1", catResult.Rows)
	}
	catRecords, err := csv.NewReader(strings.NewReader(catBuf.String())).ReadAll()
	if err != nil {
		t.Fatalf("parse category csv: %v", err)
	}
	if strings.Join(catRecords[0], ",") != "child_slug,category_slug" {
		t.Fatalf("category header = %v", catRecords[0])
	}
	if catRecords[1][0] != "ticket" || catRecords[1][1] != "cards" {
		t.Fatalf("category row = %v", catRecords[1])
	}
}
