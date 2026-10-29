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
