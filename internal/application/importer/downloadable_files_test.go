package importer_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"strings"
	"sync"
	"testing"

	catalogApp "github.com/akarso/shopanda/internal/application/catalog"
	"github.com/akarso/shopanda/internal/application/importer"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/media"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

func init() {
	sql.Register("shopanda-dl-files-import-tx", dlFilesImportTxDriver{})
}

type dlFilesImportTxDriver struct{}

func (dlFilesImportTxDriver) Open(string) (driver.Conn, error) { return dlFilesImportTxConn{}, nil }

type dlFilesImportTxConn struct{}

func (dlFilesImportTxConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (dlFilesImportTxConn) Close() error                        { return nil }
func (dlFilesImportTxConn) Begin() (driver.Tx, error)           { return dlFilesImportTx{}, nil }

type dlFilesImportTx struct{}

func (dlFilesImportTx) Commit() error   { return nil }
func (dlFilesImportTx) Rollback() error { return nil }

var (
	dlFilesImportTxOnce sync.Once
	dlFilesImportTxDB   *sql.DB
)

func dlFilesImportTxStarter() *sql.DB {
	dlFilesImportTxOnce.Do(func() {
		db, err := sql.Open("shopanda-dl-files-import-tx", "")
		if err != nil {
			panic(err)
		}
		dlFilesImportTxDB = db
	})
	return dlFilesImportTxDB
}

type dlImportProductRepo struct {
	catalog.ProductRepository
	bySlug map[string]*catalog.Product
}

func (r *dlImportProductRepo) FindBySlug(_ context.Context, slug string) (*catalog.Product, error) {
	if r.bySlug == nil {
		return nil, nil
	}
	return r.bySlug[slug], nil
}
func (r *dlImportProductRepo) FindByID(_ context.Context, id string) (*catalog.Product, error) {
	for _, p := range r.bySlug {
		if p.ID == id {
			return p, nil
		}
	}
	return nil, nil
}
func (r *dlImportProductRepo) WithTx(*sql.Tx) catalog.ProductRepository { return r }
func (r *dlImportProductRepo) LockForUpdate(_ context.Context, id string) (catalog.Type, error) {
	p, _ := r.FindByID(context.Background(), id)
	if p == nil {
		return "", apperror.NotFound("product not found")
	}
	return p.Type, nil
}

type dlImportFileRepo struct {
	catalog.DownloadableFileRepository
	rows []*catalog.DownloadableFile
}

func (r *dlImportFileRepo) ListByProductID(context.Context, string) ([]catalog.DownloadableFile, error) {
	out := make([]catalog.DownloadableFile, 0, len(r.rows))
	for _, row := range r.rows {
		out = append(out, *row)
	}
	return out, nil
}
func (r *dlImportFileRepo) CountByProductID(context.Context, string) (int, error) { return len(r.rows), nil }
func (r *dlImportFileRepo) CountByAssetID(context.Context, string) (int, error)  { return 0, nil }
func (r *dlImportFileRepo) Upsert(_ context.Context, f *catalog.DownloadableFile) error {
	cp := *f
	for i, row := range r.rows {
		if row.ProductID == f.ProductID && row.AssetID == f.AssetID {
			r.rows[i] = &cp
			return nil
		}
	}
	r.rows = append(r.rows, &cp)
	return nil
}
func (r *dlImportFileRepo) Remove(context.Context, string, string) error { return nil }
func (r *dlImportFileRepo) WithTx(*sql.Tx) catalog.DownloadableFileRepository { return r }

type dlImportAssetRepo struct {
	media.AssetRepository
	byID map[string]*media.Asset
}

func (r *dlImportAssetRepo) FindByID(_ context.Context, id string) (*media.Asset, error) {
	return r.byID[id], nil
}
func (r *dlImportAssetRepo) WithTx(*sql.Tx) media.AssetRepository { return r }
func (r *dlImportAssetRepo) LockForUpdate(_ context.Context, id string) (*media.Asset, error) {
	a := r.byID[id]
	if a == nil {
		return nil, apperror.NotFound("asset not found")
	}
	return a, nil
}

func TestDownloadableFileImport_Attach(t *testing.T) {
	products := &dlImportProductRepo{bySlug: map[string]*catalog.Product{
		"ebook": {ID: "p1", Slug: "ebook", Type: catalog.TypeDownloadable, Status: catalog.StatusDraft},
	}}
	files := &dlImportFileRepo{}
	assets := &dlImportAssetRepo{byID: map[string]*media.Asset{
		"a1": {ID: "a1", Path: "private/downloads/a1/file.pdf", Filename: "file.pdf", MimeType: "application/pdf", Size: 10},
	}}
	svc := catalogApp.NewDownloadableFileService(products, files, assets, dlFilesImportTxStarter())
	imp := importer.NewDownloadableFileImporter(products, svc)
	csv := "product_slug,asset_id,position,label\nebook,a1,1,Manual\n"
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Added != 1 || len(result.Errors) != 0 {
		t.Fatalf("result = %+v", result)
	}
	if len(files.rows) != 1 || files.rows[0].AssetID != "a1" || files.rows[0].Position != 1 {
		t.Fatalf("rows = %+v", files.rows)
	}
}

func TestDownloadableFileImport_ReimportUpdatedRetainsMeta(t *testing.T) {
	products := &dlImportProductRepo{bySlug: map[string]*catalog.Product{
		"ebook": {ID: "p1", Slug: "ebook", Type: catalog.TypeDownloadable, Status: catalog.StatusDraft},
	}}
	files := &dlImportFileRepo{}
	assets := &dlImportAssetRepo{byID: map[string]*media.Asset{
		"a1": {ID: "a1", Path: "private/downloads/a1/file.pdf", Filename: "file.pdf", MimeType: "application/pdf", Size: 10},
	}}
	svc := catalogApp.NewDownloadableFileService(products, files, assets, dlFilesImportTxStarter())
	imp := importer.NewDownloadableFileImporter(products, svc)

	first, err := imp.Import(context.Background(), strings.NewReader(
		"product_slug,asset_id,position,label\nebook,a1,3,Manual\n",
	))
	if err != nil {
		t.Fatalf("first Import: %v", err)
	}
	if first.Added != 1 || first.Updated != 0 {
		t.Fatalf("first = %+v", first)
	}

	second, err := imp.Import(context.Background(), strings.NewReader(
		"product_slug,asset_id\nebook,a1\n",
	))
	if err != nil {
		t.Fatalf("second Import: %v", err)
	}
	if second.Updated != 1 || second.Added != 0 || len(second.Errors) != 0 {
		t.Fatalf("second = %+v", second)
	}
	if len(files.rows) != 1 {
		t.Fatalf("rows len = %d, want 1", len(files.rows))
	}
	if files.rows[0].Position != 3 || files.rows[0].Label != "Manual" {
		t.Fatalf("retained meta = %+v, want position=3 label=Manual", files.rows[0])
	}
}

func TestDownloadableFileImport_SkippedErrors(t *testing.T) {
	products := &dlImportProductRepo{bySlug: map[string]*catalog.Product{
		"ebook": {ID: "p1", Slug: "ebook", Type: catalog.TypeDownloadable, Status: catalog.StatusDraft},
	}}
	files := &dlImportFileRepo{}
	assets := &dlImportAssetRepo{byID: map[string]*media.Asset{
		"a1": {ID: "a1", Path: "private/downloads/a1/file.pdf", Filename: "file.pdf", MimeType: "application/pdf", Size: 10},
		"pub": {ID: "pub", Path: "public/pub/file.pdf", Filename: "file.pdf", MimeType: "application/pdf", Size: 10},
	}}
	svc := catalogApp.NewDownloadableFileService(products, files, assets, dlFilesImportTxStarter())
	imp := importer.NewDownloadableFileImporter(products, svc)

	cases := []struct {
		name string
		csv  string
		want string
	}{
		{"unknown slug", "product_slug,asset_id\nmissing,a1\n", "unknown product slug"},
		{"negative position", "product_slug,asset_id,position\nebook,a1,-1\n", "position must be an integer >= 0"},
		{"asset outside private/downloads", "product_slug,asset_id\nebook,pub\n", "private/downloads"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := imp.Import(context.Background(), strings.NewReader(tc.csv))
			if err != nil {
				t.Fatalf("Import: %v", err)
			}
			if result.Skipped != 1 || result.Added != 0 || result.Updated != 0 {
				t.Fatalf("result = %+v", result)
			}
			if len(result.Errors) != 1 || !strings.Contains(result.Errors[0], tc.want) {
				t.Fatalf("errors = %v, want containing %q", result.Errors, tc.want)
			}
		})
	}
}
