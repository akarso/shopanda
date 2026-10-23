package exporter_test

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/csv"
	"errors"
	"strings"
	"sync"
	"testing"

	catalogApp "github.com/akarso/shopanda/internal/application/catalog"
	"github.com/akarso/shopanda/internal/application/exporter"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

func init() {
	sql.Register("shopanda-export-composition-tx", exportCompositionTxDriver{})
}

type exportCompositionTxDriver struct{}

func (exportCompositionTxDriver) Open(string) (driver.Conn, error) {
	return exportCompositionTxConn{}, nil
}

type exportCompositionTxConn struct{}

func (exportCompositionTxConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (exportCompositionTxConn) Close() error                        { return nil }
func (exportCompositionTxConn) Begin() (driver.Tx, error)           { return exportCompositionTx{}, nil }

type exportCompositionTx struct{}

func (exportCompositionTx) Commit() error   { return nil }
func (exportCompositionTx) Rollback() error { return nil }

var (
	exportCompositionTxOnce sync.Once
	exportCompositionTxDB   *sql.DB
)

func exportCompositionTxStarter() *sql.DB {
	exportCompositionTxOnce.Do(func() {
		db, err := sql.Open("shopanda-export-composition-tx", "")
		if err != nil {
			panic(err)
		}
		exportCompositionTxDB = db
	})
	return exportCompositionTxDB
}

type exportCompositionProducts struct {
	catalog.ProductRepository
	page           []catalog.Product
	byID           map[string]*catalog.Product
	filters        []catalog.ListFilter
	listByIDsCalls int
	listByIDsErr   error
	findErr        map[string]error
}

func (r *exportCompositionProducts) List(_ context.Context, filter catalog.ListFilter) ([]catalog.Product, error) {
	r.filters = append(r.filters, filter)
	if filter.Type == "" {
		return r.page, nil
	}
	out := make([]catalog.Product, 0, len(r.page))
	for _, p := range r.page {
		if p.Type == filter.Type {
			out = append(out, p)
		}
	}
	return out, nil
}
func (r *exportCompositionProducts) FindByID(_ context.Context, id string) (*catalog.Product, error) {
	if err := r.findErr[id]; err != nil {
		return nil, err
	}
	if r.byID == nil {
		return nil, nil
	}
	return r.byID[id], nil
}
func (r *exportCompositionProducts) ListByIDs(_ context.Context, ids []string) (map[string]*catalog.Product, error) {
	r.listByIDsCalls++
	if r.listByIDsErr != nil {
		return nil, r.listByIDsErr
	}
	out := map[string]*catalog.Product{}
	for _, id := range ids {
		if p := r.byID[id]; p != nil {
			out[id] = p
		}
	}
	return out, nil
}
func (r *exportCompositionProducts) WithTx(*sql.Tx) catalog.ProductRepository { return r }
func (r *exportCompositionProducts) LockForUpdate(_ context.Context, id string) (catalog.Type, error) {
	p := r.byID[id]
	if p == nil {
		return "", apperror.NotFound("product not found")
	}
	return p.Type, nil
}

type exportBundleRepo struct {
	catalog.BundleComponentRepository
	byBundle map[string][]catalog.BundleComponent
}

func (r *exportBundleRepo) ListByBundleID(_ context.Context, id string) ([]catalog.BundleComponent, error) {
	return r.byBundle[id], nil
}
func (r *exportBundleRepo) CountByBundleID(context.Context, string) (int, error) { return 0, nil }
func (r *exportBundleRepo) CountByComponentID(context.Context, string) (int, error) {
	return 0, nil
}
func (r *exportBundleRepo) Upsert(context.Context, *catalog.BundleComponent) error { return nil }
func (r *exportBundleRepo) Remove(context.Context, string, string) error           { return nil }
func (r *exportBundleRepo) WithTx(*sql.Tx) catalog.BundleComponentRepository       { return r }

type exportGroupedRepo struct {
	catalog.GroupedMemberRepository
	byGroup map[string][]catalog.GroupedMember
}

func (r *exportGroupedRepo) ListByGroupID(_ context.Context, id string) ([]catalog.GroupedMember, error) {
	return r.byGroup[id], nil
}
func (r *exportGroupedRepo) CountByGroupID(context.Context, string) (int, error) { return 0, nil }
func (r *exportGroupedRepo) CountByMemberID(context.Context, string) (int, error) {
	return 0, nil
}
func (r *exportGroupedRepo) Insert(context.Context, *catalog.GroupedMember) error { return nil }
func (r *exportGroupedRepo) Remove(context.Context, string, string) error         { return nil }
func (r *exportGroupedRepo) WithTx(*sql.Tx) catalog.GroupedMemberRepository       { return r }

func TestBundleComponentExport_StoredRows(t *testing.T) {
	products := &exportCompositionProducts{
		page: []catalog.Product{
			{ID: "s1", Slug: "plain", Type: catalog.TypeSimple, Status: catalog.StatusActive},
			{ID: "b1", Slug: "kit", Type: catalog.TypeBundle, Status: catalog.StatusActive},
		},
		byID: map[string]*catalog.Product{
			"s1": {ID: "s1", Slug: "plain", Type: catalog.TypeSimple},
			"b1": {ID: "b1", Slug: "kit", Type: catalog.TypeBundle},
			"c1": {ID: "c1", Slug: "leaf", Type: catalog.TypeSimple},
		},
	}
	components := &exportBundleRepo{byBundle: map[string][]catalog.BundleComponent{
		"b1": {{BundleProductID: "b1", ComponentProductID: "c1", Quantity: 2, Required: false}},
	}}
	exp := exporter.NewBundleComponentExporter(products, catalogApp.NewBundleService(products, components, exportCompositionTxStarter()))
	var buf bytes.Buffer
	result, err := exp.Export(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if result.Rows != 1 {
		t.Fatalf("Rows = %d errors=%v", result.Rows, result.Errors)
	}
	records, err := csv.NewReader(&buf).ReadAll()
	if err != nil {
		t.Fatalf("csv: %v", err)
	}
	if strings.Join(records[0], ",") != "parent_slug,component_slug,quantity,required" {
		t.Fatalf("header = %v", records[0])
	}
	if strings.Join(records[1], ",") != "kit,leaf,2,false" {
		t.Fatalf("row = %v", records[1])
	}
	if len(products.filters) != 1 || products.filters[0].Type != catalog.TypeBundle {
		t.Fatalf("filters = %+v, want one bundle list", products.filters)
	}
}

func TestGroupedMemberExport_StoredRows(t *testing.T) {
	products := &exportCompositionProducts{
		page: []catalog.Product{
			{ID: "g1", Slug: "group", Type: catalog.TypeGrouped, Status: catalog.StatusActive},
		},
		byID: map[string]*catalog.Product{
			"g1": {ID: "g1", Slug: "group", Type: catalog.TypeGrouped},
			"m1": {ID: "m1", Slug: "member", Type: catalog.TypeVirtual},
		},
	}
	members := &exportGroupedRepo{byGroup: map[string][]catalog.GroupedMember{
		"g1": {{GroupProductID: "g1", MemberProductID: "m1"}},
	}}
	exp := exporter.NewGroupedMemberExporter(products, catalogApp.NewGroupedService(products, members, exportCompositionTxStarter()))
	var buf bytes.Buffer
	result, err := exp.Export(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if result.Rows != 1 {
		t.Fatalf("Rows = %d", result.Rows)
	}
	text := buf.String()
	if !strings.Contains(text, "parent_slug,member_slug") || !strings.Contains(text, "group,member") {
		t.Fatalf("csv = %s", text)
	}
	if len(products.filters) != 1 || products.filters[0].Type != catalog.TypeGrouped {
		t.Fatalf("filters = %+v, want one grouped list", products.filters)
	}
}

func TestBundleComponentExport_SkipsChildWithoutSlug(t *testing.T) {
	products := &exportCompositionProducts{
		page: []catalog.Product{
			{ID: "b1", Slug: "kit", Type: catalog.TypeBundle, Status: catalog.StatusActive},
		},
		byID: map[string]*catalog.Product{
			"b1": {ID: "b1", Slug: "kit", Type: catalog.TypeBundle},
			"c1": {ID: "c1", Slug: "", Type: catalog.TypeSimple},
		},
	}
	components := &exportBundleRepo{byBundle: map[string][]catalog.BundleComponent{
		"b1": {{BundleProductID: "b1", ComponentProductID: "c1", Quantity: 1, Required: true}},
	}}
	exp := exporter.NewBundleComponentExporter(products, catalogApp.NewBundleService(products, components, exportCompositionTxStarter()))
	var buf bytes.Buffer
	result, err := exp.Export(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if result.Rows != 0 || result.Skipped != 1 {
		t.Fatalf("result = %+v", result)
	}
	if len(result.Errors) != 1 || !strings.Contains(result.Errors[0], "no slug") {
		t.Fatalf("errors = %v", result.Errors)
	}
	records, err := csv.NewReader(&buf).ReadAll()
	if err != nil {
		t.Fatalf("csv: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("csv rows = %v, want header only", records)
	}
}

func TestBundleComponentExport_BatchesChildrenAndSkipsLookupErrors(t *testing.T) {
	products := &exportCompositionProducts{
		page: []catalog.Product{
			{ID: "b1", Slug: "kit", Type: catalog.TypeBundle, Status: catalog.StatusActive},
			{ID: "b2", Slug: "other", Type: catalog.TypeBundle, Status: catalog.StatusActive},
		},
		byID: map[string]*catalog.Product{
			"b1": {ID: "b1", Slug: "kit", Type: catalog.TypeBundle},
			"b2": {ID: "b2", Slug: "other", Type: catalog.TypeBundle},
			"c1": {ID: "c1", Slug: "leaf", Type: catalog.TypeSimple},
			"c2": {ID: "c2", Slug: "second", Type: catalog.TypeVirtual},
			"c3": {ID: "c3", Slug: "third", Type: catalog.TypeSimple},
		},
	}
	batch := &selectiveBatchProducts{exportCompositionProducts: products, failID: "c1"}
	components := &exportBundleRepo{byBundle: map[string][]catalog.BundleComponent{
		"b1": {
			{BundleProductID: "b1", ComponentProductID: "c1", Quantity: 1, Required: true},
			{BundleProductID: "b1", ComponentProductID: "c2", Quantity: 2, Required: false},
		},
		"b2": {
			{BundleProductID: "b2", ComponentProductID: "c3", Quantity: 1, Required: true},
		},
	}}
	exp := exporter.NewBundleComponentExporter(batch, catalogApp.NewBundleService(batch, components, exportCompositionTxStarter()))
	var buf bytes.Buffer
	result, err := exp.Export(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if result.Rows != 1 || result.Skipped != 2 {
		t.Fatalf("result = %+v", result)
	}
	if batch.listByIDsCalls != 2 {
		t.Fatalf("ListByIDs calls = %d, want 2", batch.listByIDsCalls)
	}
	text := buf.String()
	if !strings.Contains(text, "other,third,1,true") || strings.Contains(text, "leaf") {
		t.Fatalf("csv = %s", text)
	}
	joined := strings.Join(result.Errors, "\n")
	if !strings.Contains(joined, "child batch failed") || !strings.Contains(joined, "c2") {
		t.Fatalf("errors = %v", result.Errors)
	}
}

type selectiveBatchProducts struct {
	*exportCompositionProducts
	failID string
}

func (r *selectiveBatchProducts) ListByIDs(ctx context.Context, ids []string) (map[string]*catalog.Product, error) {
	for _, id := range ids {
		if id == r.failID {
			r.listByIDsCalls++
			return nil, errors.New("child batch failed")
		}
	}
	return r.exportCompositionProducts.ListByIDs(ctx, ids)
}

type findOnlyCompositionProducts struct {
	catalog.ProductRepository
	page    []catalog.Product
	byID    map[string]*catalog.Product
	findErr map[string]error
}

func (r *findOnlyCompositionProducts) List(_ context.Context, filter catalog.ListFilter) ([]catalog.Product, error) {
	out := make([]catalog.Product, 0, len(r.page))
	for _, p := range r.page {
		if filter.Type == "" || p.Type == filter.Type {
			out = append(out, p)
		}
	}
	return out, nil
}
func (r *findOnlyCompositionProducts) FindByID(_ context.Context, id string) (*catalog.Product, error) {
	if err := r.findErr[id]; err != nil {
		return nil, err
	}
	return r.byID[id], nil
}
func (r *findOnlyCompositionProducts) WithTx(*sql.Tx) catalog.ProductRepository { return r }
func (r *findOnlyCompositionProducts) LockForUpdate(_ context.Context, id string) (catalog.Type, error) {
	p := r.byID[id]
	if p == nil {
		return "", apperror.NotFound("product not found")
	}
	return p.Type, nil
}

func TestGroupedMemberExport_SkipsFindByIDError(t *testing.T) {
	products := &findOnlyCompositionProducts{
		page: []catalog.Product{
			{ID: "g1", Slug: "group", Type: catalog.TypeGrouped, Status: catalog.StatusActive},
		},
		byID: map[string]*catalog.Product{
			"g1": {ID: "g1", Slug: "group", Type: catalog.TypeGrouped},
			"m2": {ID: "m2", Slug: "kept", Type: catalog.TypeSimple},
		},
		findErr: map[string]error{"m1": errors.New("member lookup failed")},
	}
	members := &exportGroupedRepo{byGroup: map[string][]catalog.GroupedMember{
		"g1": {
			{GroupProductID: "g1", MemberProductID: "m1"},
			{GroupProductID: "g1", MemberProductID: "m2"},
		},
	}}
	exp := exporter.NewGroupedMemberExporter(products, catalogApp.NewGroupedService(products, members, exportCompositionTxStarter()))
	var buf bytes.Buffer
	result, err := exp.Export(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if result.Rows != 1 || result.Skipped != 1 {
		t.Fatalf("result = %+v", result)
	}
	if !strings.Contains(buf.String(), "group,kept") {
		t.Fatalf("csv = %s", buf.String())
	}
	if len(result.Errors) != 1 || !strings.Contains(result.Errors[0], "member lookup failed") {
		t.Fatalf("errors = %v", result.Errors)
	}
}

func TestCompositionExporters_NilDepsPanic(t *testing.T) {
	products := &exportCompositionProducts{}
	bundles := &catalogApp.BundleService{}
	grouped := &catalogApp.GroupedService{}
	cases := []struct {
		name string
		fn   func()
	}{
		{"bundle exporter nil products", func() { exporter.NewBundleComponentExporter(nil, bundles) }},
		{"bundle exporter nil service", func() { exporter.NewBundleComponentExporter(products, nil) }},
		{"grouped exporter nil products", func() { exporter.NewGroupedMemberExporter(nil, grouped) }},
		{"grouped exporter nil service", func() { exporter.NewGroupedMemberExporter(products, nil) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("expected panic")
				}
			}()
			tc.fn()
		})
	}
}
