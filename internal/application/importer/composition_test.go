package importer_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"

	catalogApp "github.com/akarso/shopanda/internal/application/catalog"
	"github.com/akarso/shopanda/internal/application/importer"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

func init() {
	sql.Register("shopanda-import-composition-tx", compositionTxDriver{})
}

type compositionTxDriver struct{}

func (compositionTxDriver) Open(string) (driver.Conn, error) { return compositionTxConn{}, nil }

type compositionTxConn struct{}

func (compositionTxConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (compositionTxConn) Close() error                        { return nil }
func (compositionTxConn) Begin() (driver.Tx, error)           { return compositionTx{}, nil }

type compositionTx struct{}

func (compositionTx) Commit() error   { return nil }
func (compositionTx) Rollback() error { return nil }

var (
	compositionTxOnce sync.Once
	compositionTxDB   *sql.DB
)

func compositionTxStarter() *sql.DB {
	compositionTxOnce.Do(func() {
		db, err := sql.Open("shopanda-import-composition-tx", "")
		if err != nil {
			panic(err)
		}
		compositionTxDB = db
	})
	return compositionTxDB
}

type compositionProductRepo struct {
	catalog.ProductRepository
	bySlug  map[string]*catalog.Product
	slugErr map[string]error
}

func (r *compositionProductRepo) FindByID(_ context.Context, id string) (*catalog.Product, error) {
	for _, p := range r.bySlug {
		if p != nil && p.ID == id {
			return p, nil
		}
	}
	return nil, nil
}
func (r *compositionProductRepo) FindBySlug(_ context.Context, slug string) (*catalog.Product, error) {
	if err, ok := r.slugErr[slug]; ok {
		return nil, err
	}
	if r.bySlug == nil {
		return nil, nil
	}
	return r.bySlug[slug], nil
}
func (r *compositionProductRepo) WithTx(*sql.Tx) catalog.ProductRepository { return r }
func (r *compositionProductRepo) LockForUpdate(_ context.Context, id string) (catalog.Type, error) {
	for _, p := range r.bySlug {
		if p != nil && p.ID == id {
			return p.Type, nil
		}
	}
	return "", apperror.NotFound("product not found")
}

type compositionBundleRepo struct {
	catalog.BundleComponentRepository
	links []*catalog.BundleComponent
}

func (r *compositionBundleRepo) ListByBundleID(context.Context, string) ([]catalog.BundleComponent, error) {
	out := make([]catalog.BundleComponent, 0, len(r.links))
	for _, link := range r.links {
		if link != nil {
			out = append(out, *link)
		}
	}
	return out, nil
}
func (r *compositionBundleRepo) CountByBundleID(context.Context, string) (int, error) { return 0, nil }
func (r *compositionBundleRepo) CountByComponentID(context.Context, string) (int, error) {
	return 0, nil
}
func (r *compositionBundleRepo) Upsert(_ context.Context, c *catalog.BundleComponent) error {
	cp := *c
	r.links = append(r.links, &cp)
	return nil
}
func (r *compositionBundleRepo) Remove(context.Context, string, string) error { return nil }
func (r *compositionBundleRepo) WithTx(*sql.Tx) catalog.BundleComponentRepository {
	return r
}

type compositionGroupedRepo struct {
	catalog.GroupedMemberRepository
	links []*catalog.GroupedMember
}

func (r *compositionGroupedRepo) ListByGroupID(context.Context, string) ([]catalog.GroupedMember, error) {
	return nil, nil
}
func (r *compositionGroupedRepo) CountByGroupID(context.Context, string) (int, error) { return 0, nil }
func (r *compositionGroupedRepo) CountByMemberID(context.Context, string) (int, error) {
	return 0, nil
}
func (r *compositionGroupedRepo) Insert(_ context.Context, m *catalog.GroupedMember) error {
	cp := *m
	r.links = append(r.links, &cp)
	return nil
}
func (r *compositionGroupedRepo) Remove(context.Context, string, string) error   { return nil }
func (r *compositionGroupedRepo) WithTx(*sql.Tx) catalog.GroupedMemberRepository { return r }

func TestBundleComponentImport_AddAndErrors(t *testing.T) {
	products := &compositionProductRepo{bySlug: map[string]*catalog.Product{
		"kit":   {ID: "b1", Slug: "kit", Type: catalog.TypeBundle, Status: catalog.StatusActive},
		"leaf":  {ID: "c1", Slug: "leaf", Type: catalog.TypeSimple, Status: catalog.StatusActive},
		"plain": {ID: "s1", Slug: "plain", Type: catalog.TypeSimple, Status: catalog.StatusActive},
	}}
	components := &compositionBundleRepo{}
	imp := importer.NewBundleComponentImporter(products, catalogApp.NewBundleService(products, components, compositionTxStarter()))
	csv := "parent_slug,component_slug,quantity,required\n" +
		"kit,leaf,2,\n" +
		"kit,leaf,4,false\n" +
		"missing,leaf,1,true\n" +
		"plain,leaf,1,true\n" +
		"kit,leaf,0,true\n"
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Added != 2 {
		t.Fatalf("Added = %d, want 2 (upsert twice); errors=%v", result.Added, result.Errors)
	}
	if len(components.links) != 2 || components.links[0].Required != true || components.links[0].Quantity != 2 {
		t.Fatalf("first link = %+v", components.links[0])
	}
	if components.links[1].Required != false || components.links[1].Quantity != 4 {
		t.Fatalf("second link = %+v", components.links[1])
	}
	joined := strings.Join(result.Errors, "\n")
	for _, want := range []string{"line 4: unknown parent slug", "line 5:", "line 6:"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("errors = %v, want %q", result.Errors, want)
		}
	}
	if result.Skipped != 3 {
		t.Fatalf("Skipped = %d, want 3", result.Skipped)
	}
}

func TestGroupedMemberImport_InsertIgnoreShape(t *testing.T) {
	products := &compositionProductRepo{bySlug: map[string]*catalog.Product{
		"group":  {ID: "g1", Slug: "group", Type: catalog.TypeGrouped, Status: catalog.StatusActive},
		"member": {ID: "m1", Slug: "member", Type: catalog.TypeVirtual, Status: catalog.StatusActive},
	}}
	members := &compositionGroupedRepo{}
	imp := importer.NewGroupedMemberImporter(products, catalogApp.NewGroupedService(products, members, compositionTxStarter()))
	csv := "parent_slug,member_slug\ngroup,member\nnope,member\n"
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Added != 1 || len(members.links) != 1 || members.links[0].MemberProductID != "m1" {
		t.Fatalf("result=%+v links=%+v", result, members.links)
	}
	if len(result.Errors) != 1 || !strings.Contains(result.Errors[0], "line 3: unknown parent slug \"nope\"") {
		t.Fatalf("errors = %v", result.Errors)
	}
}

func TestBundleComponentImport_RowErrors(t *testing.T) {
	products := &compositionProductRepo{bySlug: map[string]*catalog.Product{
		"kit":  {ID: "b1", Slug: "kit", Type: catalog.TypeBundle, Status: catalog.StatusActive},
		"leaf": {ID: "c1", Slug: "leaf", Type: catalog.TypeSimple, Status: catalog.StatusActive},
	}}
	components := &compositionBundleRepo{}
	imp := importer.NewBundleComponentImporter(products, catalogApp.NewBundleService(products, components, compositionTxStarter()))

	missing, err := imp.Import(context.Background(), strings.NewReader("parent_slug,component_slug\nkit,leaf,1,true\n"))
	if err == nil || missing != nil {
		t.Fatalf("missing column: result=%v err=%v", missing, err)
	}
	if !strings.Contains(err.Error(), "missing required column") {
		t.Fatalf("err = %v", err)
	}

	csv := "parent_slug,component_slug,quantity,required\n" +
		"kit,leaf,1,maybe\n" +
		"kit,missing,1,true\n" +
		"kit,kit,1,true\n" +
		"kit,leaf,10000,true\n"
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Added != 0 || result.Skipped != 4 {
		t.Fatalf("result = %+v", result)
	}
	joined := strings.Join(result.Errors, "\n")
	for _, want := range []string{
		"line 2: invalid required",
		"line 3: unknown component slug",
		"line 4:",
		"line 5:",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("errors = %v, want %q", result.Errors, want)
		}
	}
	if !strings.Contains(joined, "itself") || !strings.Contains(joined, "9999") {
		t.Fatalf("errors = %v, want self-link and quantity cap", result.Errors)
	}
}

func TestBundleComponentImport_FiftyFirstRequired(t *testing.T) {
	bySlug := map[string]*catalog.Product{
		"kit": {ID: "b1", Slug: "kit", Type: catalog.TypeBundle, Status: catalog.StatusActive},
	}
	components := &compositionBundleRepo{}
	for i := 0; i < catalog.MaxBundleRequiredComponents; i++ {
		slug := "leaf-" + strconv.Itoa(i)
		id := "c" + strconv.Itoa(i)
		bySlug[slug] = &catalog.Product{ID: id, Slug: slug, Type: catalog.TypeSimple, Status: catalog.StatusActive}
		components.links = append(components.links, &catalog.BundleComponent{
			BundleProductID: "b1", ComponentProductID: id, Quantity: 1, Required: true,
		})
	}
	bySlug["extra"] = &catalog.Product{ID: "extra", Slug: "extra", Type: catalog.TypeSimple, Status: catalog.StatusActive}
	imp := importer.NewBundleComponentImporter(&compositionProductRepo{bySlug: bySlug}, catalogApp.NewBundleService(&compositionProductRepo{bySlug: bySlug}, components, compositionTxStarter()))
	result, err := imp.Import(context.Background(), strings.NewReader(
		"parent_slug,component_slug,quantity,required\nkit,extra,1,true\n",
	))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Added != 0 || result.Skipped != 1 {
		t.Fatalf("result = %+v", result)
	}
	if len(result.Errors) != 1 || !strings.Contains(result.Errors[0], "line 2:") || !strings.Contains(result.Errors[0], "maximum is 50") {
		t.Fatalf("errors = %v", result.Errors)
	}
}

func TestCompositionImport_FindBySlugErrorContinues(t *testing.T) {
	products := &compositionProductRepo{
		bySlug: map[string]*catalog.Product{
			"kit":    {ID: "b1", Slug: "kit", Type: catalog.TypeBundle, Status: catalog.StatusActive},
			"leaf":   {ID: "c1", Slug: "leaf", Type: catalog.TypeSimple, Status: catalog.StatusActive},
			"group":  {ID: "g1", Slug: "group", Type: catalog.TypeGrouped, Status: catalog.StatusActive},
			"member": {ID: "m1", Slug: "member", Type: catalog.TypeVirtual, Status: catalog.StatusActive},
		},
		slugErr: map[string]error{"missing-db": errors.New("db down")},
	}
	components := &compositionBundleRepo{}
	bundles := importer.NewBundleComponentImporter(products, catalogApp.NewBundleService(products, components, compositionTxStarter()))
	bundleCSV := "parent_slug,component_slug,quantity,required\n" +
		"missing-db,leaf,1,true\n" +
		"kit,missing-db,1,true\n" +
		"kit,leaf,2,true\n"
	bundleResult, err := bundles.Import(context.Background(), strings.NewReader(bundleCSV))
	if err != nil {
		t.Fatalf("bundle Import: %v", err)
	}
	if bundleResult.Added != 1 || bundleResult.Skipped != 2 {
		t.Fatalf("bundle result = %+v", bundleResult)
	}
	if len(components.links) != 1 || components.links[0].Quantity != 2 {
		t.Fatalf("bundle links = %+v", components.links)
	}
	bundleJoined := strings.Join(bundleResult.Errors, "\n")
	for _, want := range []string{
		`line 2: find parent slug "missing-db"`,
		`line 3: find component slug "missing-db"`,
	} {
		if !strings.Contains(bundleJoined, want) {
			t.Fatalf("bundle errors = %v, want %q", bundleResult.Errors, want)
		}
	}

	members := &compositionGroupedRepo{}
	grouped := importer.NewGroupedMemberImporter(products, catalogApp.NewGroupedService(products, members, compositionTxStarter()))
	groupedCSV := "parent_slug,member_slug\nmissing-db,member\ngroup,member\n"
	groupedResult, err := grouped.Import(context.Background(), strings.NewReader(groupedCSV))
	if err != nil {
		t.Fatalf("grouped Import: %v", err)
	}
	if groupedResult.Added != 1 || groupedResult.Skipped != 1 {
		t.Fatalf("grouped result = %+v", groupedResult)
	}
	if len(members.links) != 1 || members.links[0].MemberProductID != "m1" {
		t.Fatalf("grouped links = %+v", members.links)
	}
	if len(groupedResult.Errors) != 1 || !strings.Contains(groupedResult.Errors[0], `line 2: find parent slug "missing-db"`) {
		t.Fatalf("grouped errors = %v", groupedResult.Errors)
	}
}

func TestCompositionImporters_NilDepsPanic(t *testing.T) {
	products := &compositionProductRepo{}
	bundles := &catalogApp.BundleService{}
	grouped := &catalogApp.GroupedService{}
	cases := []struct {
		name string
		fn   func()
	}{
		{"bundle importer nil products", func() { importer.NewBundleComponentImporter(nil, bundles) }},
		{"bundle importer nil service", func() { importer.NewBundleComponentImporter(products, nil) }},
		{"grouped importer nil products", func() { importer.NewGroupedMemberImporter(nil, grouped) }},
		{"grouped importer nil service", func() { importer.NewGroupedMemberImporter(products, nil) }},
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
