package catalog_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	catalogapp "github.com/akarso/shopanda/internal/application/catalog"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/media"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

type stubDLProductRepo struct {
	byID map[string]*catalog.Product
	typ  map[string]catalog.Type
}

func (s *stubDLProductRepo) Create(context.Context, *catalog.Product) error { return nil }
func (s *stubDLProductRepo) Update(context.Context, *catalog.Product) error { return nil }
func (s *stubDLProductRepo) Delete(context.Context, string) error           { return nil }
func (s *stubDLProductRepo) FindByID(_ context.Context, id string) (*catalog.Product, error) {
	return s.byID[id], nil
}
func (s *stubDLProductRepo) FindBySlug(context.Context, string) (*catalog.Product, error) {
	return nil, nil
}
func (s *stubDLProductRepo) FindByCategoryID(context.Context, string, int, int) ([]catalog.Product, error) {
	return nil, nil
}
func (s *stubDLProductRepo) List(context.Context, catalog.ListFilter) ([]catalog.Product, error) {
	return nil, nil
}
func (s *stubDLProductRepo) WithTx(*sql.Tx) catalog.ProductRepository { return s }
func (s *stubDLProductRepo) LockForUpdate(_ context.Context, id string) (catalog.Type, error) {
	if t, ok := s.typ[id]; ok {
		return t, nil
	}
	if p := s.byID[id]; p != nil {
		return p.Type, nil
	}
	return "", apperror.NotFound("product not found")
}

type stubDLFileRepo struct {
	rows   map[string][]catalog.DownloadableFile
	counts map[string]int
}

func (s *stubDLFileRepo) ListByProductID(_ context.Context, productID string) ([]catalog.DownloadableFile, error) {
	if s.rows == nil {
		return []catalog.DownloadableFile{}, nil
	}
	out := s.rows[productID]
	if out == nil {
		return []catalog.DownloadableFile{}, nil
	}
	return out, nil
}
func (s *stubDLFileRepo) CountByProductID(_ context.Context, productID string) (int, error) {
	if s.counts != nil {
		return s.counts[productID], nil
	}
	return len(s.rows[productID]), nil
}
func (s *stubDLFileRepo) CountByAssetID(context.Context, string) (int, error) { return 0, nil }
func (s *stubDLFileRepo) Upsert(_ context.Context, f *catalog.DownloadableFile) error {
	if s.rows == nil {
		s.rows = map[string][]catalog.DownloadableFile{}
	}
	s.rows[f.ProductID] = append(s.rows[f.ProductID], *f)
	if s.counts == nil {
		s.counts = map[string]int{}
	}
	s.counts[f.ProductID]++
	return nil
}
func (s *stubDLFileRepo) Remove(_ context.Context, productID, assetID string) error {
	rows := s.rows[productID]
	kept := rows[:0]
	found := false
	for _, r := range rows {
		if r.AssetID == assetID {
			found = true
			continue
		}
		kept = append(kept, r)
	}
	if !found {
		return apperror.NotFound("downloadable file not found")
	}
	s.rows[productID] = kept
	if s.counts != nil {
		s.counts[productID] = len(kept)
	}
	return nil
}
func (s *stubDLFileRepo) WithTx(*sql.Tx) catalog.DownloadableFileRepository { return s }

type stubDLAssetRepo struct {
	byID map[string]*media.Asset
}

func (s *stubDLAssetRepo) Save(context.Context, *media.Asset) error { return nil }
func (s *stubDLAssetRepo) FindByID(_ context.Context, id string) (*media.Asset, error) {
	return s.byID[id], nil
}
func (s *stubDLAssetRepo) List(context.Context, int, int) ([]media.Asset, error) { return nil, nil }
func (s *stubDLAssetRepo) Delete(context.Context, string) error                  { return nil }
func (s *stubDLAssetRepo) WithTx(*sql.Tx) media.AssetRepository                 { return s }
func (s *stubDLAssetRepo) LockForUpdate(_ context.Context, id string) error {
	if s.byID == nil || s.byID[id] == nil {
		return apperror.NotFound("asset not found")
	}
	return nil
}

type stubDLTxStarter struct{}

func (stubDLTxStarter) BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error) {
	// sql.Tx cannot be constructed without a real DB. List tests use this stub
	// because they never BeginTx. Attach/Remove service rules (including
	// last-file-while-active) are exercised via newDLService → testTxStarter()
	// in this file; postgres tests cover DownloadableFileRepo directly, not
	// DownloadableFileService. Media delete + CountByAssetID is covered in
	// internal/application/media/service_test.go.
	return nil, sql.ErrConnDone
}

func TestNewDownloadableFileService_PanicsOnNil(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	catalogapp.NewDownloadableFileService(nil, &stubDLFileRepo{}, &stubDLAssetRepo{}, stubDLTxStarter{})
}

func TestDownloadableFileService_List_OK(t *testing.T) {
	products := &stubDLProductRepo{byID: map[string]*catalog.Product{
		"p1": {ID: "p1", Type: catalog.TypeDownloadable, Status: catalog.StatusDraft},
	}}
	files := &stubDLFileRepo{rows: map[string][]catalog.DownloadableFile{
		"p1": {{ProductID: "p1", AssetID: "a1"}},
	}}
	svc := catalogapp.NewDownloadableFileService(products, files, &stubDLAssetRepo{}, stubDLTxStarter{})
	rows, err := svc.List(context.Background(), "p1")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 1 || rows[0].AssetID != "a1" {
		t.Fatalf("rows = %+v", rows)
	}
}

// List+Remove on a simple product with illicit join rows: operators can see
// asset IDs then clear them before a type change (Attach stays type-gated).
func TestDownloadableFileService_ListAndRemove_IllicitRowsOnSimple(t *testing.T) {
	ctx := catalog.WithOperatorProductReadScope(context.Background())
	products := &stubDLProductRepo{byID: map[string]*catalog.Product{
		"p1": {ID: "p1", Type: catalog.TypeSimple, Status: catalog.StatusActive},
	}, typ: map[string]catalog.Type{"p1": catalog.TypeSimple}}
	files := &stubDLFileRepo{
		rows: map[string][]catalog.DownloadableFile{
			"p1": {{ProductID: "p1", AssetID: "a1"}, {ProductID: "p1", AssetID: "a2"}},
		},
		counts: map[string]int{"p1": 2},
	}
	svc := newDLService(products, files, &stubDLAssetRepo{})

	rows, err := svc.List(ctx, "p1")
	if err != nil {
		t.Fatalf("List illicit rows on simple: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("List rows = %+v, want 2 illicit attachments", rows)
	}
	if err := svc.Remove(ctx, "p1", "a1"); err != nil {
		t.Fatalf("Remove illicit row on simple: %v", err)
	}
	if err := svc.Remove(ctx, "p1", "a2"); err != nil {
		t.Fatalf("Remove second illicit row: %v", err)
	}
	rows, err = svc.List(ctx, "p1")
	if err != nil {
		t.Fatalf("List after clear: %v", err)
	}
	if len(rows) != 0 || files.counts["p1"] != 0 {
		t.Fatalf("after clear rows=%+v counts=%#v", rows, files.counts)
	}
}

func newDLService(products catalog.ProductRepository, files catalog.DownloadableFileRepository, assets media.AssetRepository) *catalogapp.DownloadableFileService {
	return catalogapp.NewDownloadableFileService(products, files, assets, testTxStarter())
}

func TestDownloadableFileService_Attach_TypeMismatch(t *testing.T) {
	ctx := catalog.WithOperatorProductReadScope(context.Background())
	products := &stubDLProductRepo{byID: map[string]*catalog.Product{
		"p1": {ID: "p1", Type: catalog.TypeSimple, Status: catalog.StatusActive},
	}, typ: map[string]catalog.Type{"p1": catalog.TypeSimple}}
	assets := &stubDLAssetRepo{byID: map[string]*media.Asset{
		"a1": {ID: "a1"},
	}}
	svc := newDLService(products, &stubDLFileRepo{}, assets)
	f, err := catalog.NewDownloadableFile("p1", "a1")
	if err != nil {
		t.Fatalf("NewDownloadableFile: %v", err)
	}
	err = svc.Attach(ctx, f)
	if err == nil || !apperror.Is(err, apperror.CodeValidation) || !strings.Contains(err.Error(), "downloadable") {
		t.Fatalf("Attach = %v, want type validation", err)
	}
}

func TestDownloadableFileService_Attach_MissingAsset(t *testing.T) {
	ctx := catalog.WithOperatorProductReadScope(context.Background())
	products := &stubDLProductRepo{byID: map[string]*catalog.Product{
		"p1": {ID: "p1", Type: catalog.TypeDownloadable, Status: catalog.StatusDraft},
	}, typ: map[string]catalog.Type{"p1": catalog.TypeDownloadable}}
	svc := newDLService(products, &stubDLFileRepo{}, &stubDLAssetRepo{byID: map[string]*media.Asset{}})
	f, err := catalog.NewDownloadableFile("p1", "missing")
	if err != nil {
		t.Fatalf("NewDownloadableFile: %v", err)
	}
	err = svc.Attach(ctx, f)
	if !apperror.Is(err, apperror.CodeNotFound) {
		t.Fatalf("Attach = %v, want not found", err)
	}
}

func TestDownloadableFileService_Attach_OK(t *testing.T) {
	ctx := catalog.WithOperatorProductReadScope(context.Background())
	products := &stubDLProductRepo{byID: map[string]*catalog.Product{
		"p1": {ID: "p1", Type: catalog.TypeDownloadable, Status: catalog.StatusDraft},
	}, typ: map[string]catalog.Type{"p1": catalog.TypeDownloadable}}
	files := &stubDLFileRepo{}
	assets := &stubDLAssetRepo{byID: map[string]*media.Asset{"a1": {ID: "a1"}}}
	svc := newDLService(products, files, assets)
	f, err := catalog.NewDownloadableFile("p1", "a1")
	if err != nil {
		t.Fatalf("NewDownloadableFile: %v", err)
	}
	if err := svc.Attach(ctx, f); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if files.counts["p1"] != 1 {
		t.Fatalf("counts = %#v", files.counts)
	}
}

func TestDownloadableFileService_Remove_LastFileOnActiveRejected(t *testing.T) {
	ctx := catalog.WithOperatorProductReadScope(context.Background())
	products := &stubDLProductRepo{byID: map[string]*catalog.Product{
		"p1": {ID: "p1", Type: catalog.TypeDownloadable, Status: catalog.StatusActive},
	}, typ: map[string]catalog.Type{"p1": catalog.TypeDownloadable}}
	files := &stubDLFileRepo{
		rows: map[string][]catalog.DownloadableFile{
			"p1": {{ProductID: "p1", AssetID: "a1"}},
		},
		counts: map[string]int{"p1": 1},
	}
	svc := newDLService(products, files, &stubDLAssetRepo{})
	err := svc.Remove(ctx, "p1", "a1")
	if !apperror.Is(err, apperror.CodeValidation) || !strings.Contains(err.Error(), catalog.DownloadableRequiresFileMessage) {
		t.Fatalf("Remove = %v, want last-file validation", err)
	}
}

func TestDownloadableFileService_Remove_NonMatchingAssetNotFound(t *testing.T) {
	ctx := catalog.WithOperatorProductReadScope(context.Background())
	products := &stubDLProductRepo{byID: map[string]*catalog.Product{
		"p1": {ID: "p1", Type: catalog.TypeDownloadable, Status: catalog.StatusActive},
	}, typ: map[string]catalog.Type{"p1": catalog.TypeDownloadable}}
	files := &stubDLFileRepo{
		rows: map[string][]catalog.DownloadableFile{
			"p1": {{ProductID: "p1", AssetID: "a1"}},
		},
		counts: map[string]int{"p1": 1},
	}
	svc := newDLService(products, files, &stubDLAssetRepo{})
	err := svc.Remove(ctx, "p1", "other-asset")
	if !apperror.Is(err, apperror.CodeNotFound) {
		t.Fatalf("Remove = %v, want not found for non-matching asset", err)
	}
}

func TestDownloadableFileService_Remove_OKWhenDraft(t *testing.T) {
	ctx := catalog.WithOperatorProductReadScope(context.Background())
	products := &stubDLProductRepo{byID: map[string]*catalog.Product{
		"p1": {ID: "p1", Type: catalog.TypeDownloadable, Status: catalog.StatusDraft},
	}, typ: map[string]catalog.Type{"p1": catalog.TypeDownloadable}}
	files := &stubDLFileRepo{
		rows: map[string][]catalog.DownloadableFile{
			"p1": {{ProductID: "p1", AssetID: "a1"}},
		},
		counts: map[string]int{"p1": 1},
	}
	svc := newDLService(products, files, &stubDLAssetRepo{})
	if err := svc.Remove(ctx, "p1", "a1"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if files.counts["p1"] != 0 {
		t.Fatalf("counts = %#v", files.counts)
	}
}

func TestDownloadableFileService_Remove_OneOfTwoOnActiveOK(t *testing.T) {
	ctx := catalog.WithOperatorProductReadScope(context.Background())
	products := &stubDLProductRepo{byID: map[string]*catalog.Product{
		"p1": {ID: "p1", Type: catalog.TypeDownloadable, Status: catalog.StatusActive},
	}, typ: map[string]catalog.Type{"p1": catalog.TypeDownloadable}}
	files := &stubDLFileRepo{
		rows: map[string][]catalog.DownloadableFile{
			"p1": {{ProductID: "p1", AssetID: "a1"}, {ProductID: "p1", AssetID: "a2"}},
		},
		counts: map[string]int{"p1": 2},
	}
	svc := newDLService(products, files, &stubDLAssetRepo{})
	if err := svc.Remove(ctx, "p1", "a1"); err != nil {
		t.Fatalf("Remove one of two: %v", err)
	}
	if files.counts["p1"] != 1 {
		t.Fatalf("counts = %#v, want 1 remaining", files.counts)
	}
}

func TestDownloadableFileService_Remove_NonDownloadableTypeOK(t *testing.T) {
	ctx := catalog.WithOperatorProductReadScope(context.Background())
	products := &stubDLProductRepo{byID: map[string]*catalog.Product{
		"p1": {ID: "p1", Type: catalog.TypeSimple, Status: catalog.StatusActive},
	}, typ: map[string]catalog.Type{"p1": catalog.TypeSimple}}
	files := &stubDLFileRepo{
		rows: map[string][]catalog.DownloadableFile{
			"p1": {{ProductID: "p1", AssetID: "a1"}},
		},
		counts: map[string]int{"p1": 1},
	}
	svc := newDLService(products, files, &stubDLAssetRepo{})
	if err := svc.Remove(ctx, "p1", "a1"); err != nil {
		t.Fatalf("Remove illicit row on simple: %v", err)
	}
	if files.counts["p1"] != 0 {
		t.Fatalf("counts = %#v", files.counts)
	}
}

func TestDownloadableFileService_Attach_DraftWithoutOperatorScope(t *testing.T) {
	products := &stubDLProductRepo{byID: map[string]*catalog.Product{
		"p1": {ID: "p1", Type: catalog.TypeDownloadable, Status: catalog.StatusDraft},
	}, typ: map[string]catalog.Type{"p1": catalog.TypeDownloadable}}
	assets := &stubDLAssetRepo{byID: map[string]*media.Asset{"a1": {ID: "a1"}}}
	svc := newDLService(products, &stubDLFileRepo{}, assets)
	f, err := catalog.NewDownloadableFile("p1", "a1")
	if err != nil {
		t.Fatalf("NewDownloadableFile: %v", err)
	}
	err = svc.Attach(context.Background(), f)
	if !apperror.Is(err, apperror.CodeNotFound) {
		t.Fatalf("Attach without operator scope = %v, want not found for draft", err)
	}
}
