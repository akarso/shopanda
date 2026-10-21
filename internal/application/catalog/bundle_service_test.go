package catalog_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"sync"
	"testing"

	catalogApp "github.com/akarso/shopanda/internal/application/catalog"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

func init() {
	sql.Register("shopanda-bundle-fake-tx", fakeTxDriver{})
}

type fakeTxDriver struct{}

func (fakeTxDriver) Open(string) (driver.Conn, error) { return fakeTxConn{}, nil }

type fakeTxConn struct{}

func (fakeTxConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (fakeTxConn) Close() error                        { return nil }
func (fakeTxConn) Begin() (driver.Tx, error)           { return fakeTx{}, nil }

type fakeTx struct{}

func (fakeTx) Commit() error   { return nil }
func (fakeTx) Rollback() error { return nil }

var (
	fakeTxDBOnce sync.Once
	fakeTxDB     *sql.DB
)

func testTxStarter() *sql.DB {
	fakeTxDBOnce.Do(func() {
		db, err := sql.Open("shopanda-bundle-fake-tx", "")
		if err != nil {
			panic(err)
		}
		fakeTxDB = db
	})
	return fakeTxDB
}

func newBundleService(products catalog.ProductRepository, components catalog.BundleComponentRepository) *catalogApp.BundleService {
	return catalogApp.NewBundleService(products, components, testTxStarter())
}

type stubBundleComponentRepo struct {
	catalog.BundleComponentRepository
	list         []catalog.BundleComponent
	listBundleID string
	upsert       *catalog.BundleComponent
	removed      struct {
		bundleID    string
		componentID string
	}
	removeErr error
	calls     *[]string
}

func (s *stubBundleComponentRepo) ListByBundleID(_ context.Context, bundleProductID string) ([]catalog.BundleComponent, error) {
	s.listBundleID = bundleProductID
	return s.list, nil
}

func (s *stubBundleComponentRepo) CountByBundleID(context.Context, string) (int, error) {
	return 0, nil
}

func (s *stubBundleComponentRepo) CountByComponentID(context.Context, string) (int, error) {
	return 0, nil
}

func (s *stubBundleComponentRepo) Upsert(_ context.Context, c *catalog.BundleComponent) error {
	if s.calls != nil {
		*s.calls = append(*s.calls, "upsert")
	}
	cp := *c
	s.upsert = &cp
	return nil
}

func (s *stubBundleComponentRepo) WithTx(_ *sql.Tx) catalog.BundleComponentRepository { return s }

func (s *stubBundleComponentRepo) Remove(_ context.Context, bundleProductID, componentProductID string) error {
	s.removed.bundleID = bundleProductID
	s.removed.componentID = componentProductID
	return s.removeErr
}

type stubProductRepo struct {
	catalog.ProductRepository
	byID  map[string]*catalog.Product
	calls *[]string
}

func (s *stubProductRepo) FindByID(_ context.Context, id string) (*catalog.Product, error) {
	if s.byID == nil {
		return nil, nil
	}
	return s.byID[id], nil
}

func (s *stubProductRepo) WithTx(_ *sql.Tx) catalog.ProductRepository { return s }

func (s *stubProductRepo) LockForUpdate(_ context.Context, id string) (catalog.Type, error) {
	if s.calls != nil {
		*s.calls = append(*s.calls, "lock:"+id)
	}
	if s.byID == nil {
		return "", apperror.NotFound("product not found")
	}
	p := s.byID[id]
	if p == nil {
		return "", apperror.NotFound("product not found")
	}
	return p.Type, nil
}

func TestBundleService_Add_LocksBeforeUpsert(t *testing.T) {
	var calls []string
	products := &stubProductRepo{
		byID: map[string]*catalog.Product{
			"zzz": {ID: "zzz", Type: catalog.TypeBundle},
			"aaa": {ID: "aaa", Type: catalog.TypeSimple},
		},
		calls: &calls,
	}
	components := &stubBundleComponentRepo{calls: &calls}
	svc := newBundleService(products, components)

	c, err := catalog.NewBundleComponent("zzz", "aaa", 1)
	if err != nil {
		t.Fatalf("NewBundleComponent: %v", err)
	}
	if err := svc.Add(context.Background(), c); err != nil {
		t.Fatalf("Add: %v", err)
	}
	want := []string{"lock:aaa", "lock:zzz", "upsert"}
	if len(calls) != len(want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Fatalf("calls = %v, want %v", calls, want)
		}
	}
}

func TestBundleService_Add_ValidComposition(t *testing.T) {
	products := &stubProductRepo{byID: map[string]*catalog.Product{
		"b1": {ID: "b1", Type: catalog.TypeBundle},
		"c1": {ID: "c1", Type: catalog.TypeSimple},
	}}
	components := &stubBundleComponentRepo{}
	svc := newBundleService(products, components)

	c, err := catalog.NewBundleComponent("b1", "c1", 3)
	if err != nil {
		t.Fatalf("NewBundleComponent: %v", err)
	}
	c.Required = false
	if err := svc.Add(context.Background(), c); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if components.upsert == nil {
		t.Fatal("expected upsert")
	}
	if components.upsert.Quantity != 3 || components.upsert.Required != false {
		t.Errorf("upsert = %+v", components.upsert)
	}
}

func TestBundleService_Add_TrimsIDs(t *testing.T) {
	products := &stubProductRepo{byID: map[string]*catalog.Product{
		"b1": {ID: "b1", Type: catalog.TypeBundle},
		"c1": {ID: "c1", Type: catalog.TypeSimple},
	}}
	components := &stubBundleComponentRepo{}
	svc := newBundleService(products, components)

	c := &catalog.BundleComponent{
		BundleProductID:    "  b1  ",
		ComponentProductID: " c1 ",
		Quantity:           1,
		Required:           true,
	}
	if err := svc.Add(context.Background(), c); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if c.BundleProductID != "b1" || c.ComponentProductID != "c1" {
		t.Fatalf("ids = %q %q, want trimmed", c.BundleProductID, c.ComponentProductID)
	}
	if components.upsert == nil || components.upsert.BundleProductID != "b1" || components.upsert.ComponentProductID != "c1" {
		t.Fatalf("upsert = %+v", components.upsert)
	}
}

func TestBundleService_List_EmptyID(t *testing.T) {
	svc := newBundleService(&stubProductRepo{}, &stubBundleComponentRepo{})

	_, err := svc.List(context.Background(), "")
	if !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("List = %v, want validation", err)
	}
}

func TestBundleService_Add_ProductNotFound(t *testing.T) {
	products := &stubProductRepo{byID: map[string]*catalog.Product{}}
	svc := newBundleService(products, &stubBundleComponentRepo{})

	c, _ := catalog.NewBundleComponent("missing", "c1", 1)
	err := svc.Add(context.Background(), c)
	if !apperror.Is(err, apperror.CodeNotFound) {
		t.Fatalf("Add = %v, want not found", err)
	}
}

func TestBundleService_Add_MissingComponent(t *testing.T) {
	products := &stubProductRepo{byID: map[string]*catalog.Product{
		"b1": {ID: "b1", Type: catalog.TypeBundle},
	}}
	svc := newBundleService(products, &stubBundleComponentRepo{})

	c, _ := catalog.NewBundleComponent("b1", "missing", 1)
	err := svc.Add(context.Background(), c)
	if !apperror.Is(err, apperror.CodeNotFound) {
		t.Fatalf("Add = %v, want not found", err)
	}
}

func TestBundleService_Add_InvalidComposition(t *testing.T) {
	products := &stubProductRepo{byID: map[string]*catalog.Product{
		"b1": {ID: "b1", Type: catalog.TypeSimple},
		"c1": {ID: "c1", Type: catalog.TypeSimple},
	}}
	svc := newBundleService(products, &stubBundleComponentRepo{})

	c, _ := catalog.NewBundleComponent("b1", "c1", 1)
	err := svc.Add(context.Background(), c)
	if !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("Add = %v, want validation", err)
	}
}

func TestBundleService_Add_RejectsGroupedComponent(t *testing.T) {
	products := &stubProductRepo{byID: map[string]*catalog.Product{
		"b1": {ID: "b1", Type: catalog.TypeBundle},
		"g1": {ID: "g1", Type: catalog.TypeGrouped},
	}}
	svc := newBundleService(products, &stubBundleComponentRepo{})

	c, _ := catalog.NewBundleComponent("b1", "g1", 1)
	err := svc.Add(context.Background(), c)
	if !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("Add = %v, want validation", err)
	}
}

func TestBundleService_Add_RejectsConfigurableComponent(t *testing.T) {
	products := &stubProductRepo{byID: map[string]*catalog.Product{
		"b1": {ID: "b1", Type: catalog.TypeBundle},
		"x1": {ID: "x1", Type: catalog.TypeConfigurable},
	}}
	svc := newBundleService(products, &stubBundleComponentRepo{})

	c, _ := catalog.NewBundleComponent("b1", "x1", 1)
	err := svc.Add(context.Background(), c)
	if !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("Add = %v, want validation", err)
	}
}

func TestBundleService_Add_RejectsNestedBundle(t *testing.T) {
	products := &stubProductRepo{byID: map[string]*catalog.Product{
		"b1": {ID: "b1", Type: catalog.TypeBundle},
		"b2": {ID: "b2", Type: catalog.TypeBundle},
	}}
	svc := newBundleService(products, &stubBundleComponentRepo{})

	c, _ := catalog.NewBundleComponent("b1", "b2", 1)
	err := svc.Add(context.Background(), c)
	if !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("Add = %v, want validation", err)
	}
}

func TestBundleService_Add_EmptyIDs(t *testing.T) {
	svc := newBundleService(&stubProductRepo{}, &stubBundleComponentRepo{})

	err := svc.Add(context.Background(), &catalog.BundleComponent{Quantity: 1})
	if !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("Add = %v, want validation", err)
	}
}

func TestBundleService_List_ForwardsBundleID(t *testing.T) {
	want := []catalog.BundleComponent{{BundleProductID: "b1", ComponentProductID: "c1", Quantity: 1}}
	components := &stubBundleComponentRepo{list: want}
	products := &stubProductRepo{byID: map[string]*catalog.Product{
		"b1": {ID: "b1", Type: catalog.TypeBundle},
	}}
	svc := newBundleService(products, components)

	got, err := svc.List(context.Background(), "b1")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if components.listBundleID != "b1" {
		t.Errorf("listBundleID = %q, want b1", components.listBundleID)
	}
	if len(got) != 1 || got[0].ComponentProductID != "c1" {
		t.Errorf("List = %+v", got)
	}
}

func TestBundleService_List_NotFound(t *testing.T) {
	svc := newBundleService(&stubProductRepo{}, &stubBundleComponentRepo{})

	_, err := svc.List(context.Background(), "missing")
	if !apperror.Is(err, apperror.CodeNotFound) {
		t.Fatalf("List = %v, want not found", err)
	}
}

func TestBundleService_List_NotBundle(t *testing.T) {
	products := &stubProductRepo{byID: map[string]*catalog.Product{
		"p1": {ID: "p1", Type: catalog.TypeSimple},
	}}
	svc := newBundleService(products, &stubBundleComponentRepo{})

	_, err := svc.List(context.Background(), "p1")
	if !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("List = %v, want validation", err)
	}
}

func TestBundleService_List_EmptySlice(t *testing.T) {
	products := &stubProductRepo{byID: map[string]*catalog.Product{
		"b1": {ID: "b1", Type: catalog.TypeBundle},
	}}
	components := &stubBundleComponentRepo{list: []catalog.BundleComponent{}}
	svc := newBundleService(products, components)

	got, err := svc.List(context.Background(), "b1")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil empty slice")
	}
	if len(got) != 0 {
		t.Errorf("List = %+v", got)
	}
}

func TestBundleService_Remove(t *testing.T) {
	components := &stubBundleComponentRepo{}
	svc := newBundleService(&stubProductRepo{}, components)

	if err := svc.Remove(context.Background(), "b1", "c1"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if components.removed.bundleID != "b1" || components.removed.componentID != "c1" {
		t.Errorf("removed = %+v", components.removed)
	}
}

func TestBundleService_Remove_NotFound(t *testing.T) {
	components := &stubBundleComponentRepo{removeErr: apperror.NotFound("bundle component not found")}
	svc := newBundleService(&stubProductRepo{}, components)

	err := svc.Remove(context.Background(), "b1", "c1")
	if !apperror.Is(err, apperror.CodeNotFound) {
		t.Fatalf("Remove = %v, want not found", err)
	}
}
