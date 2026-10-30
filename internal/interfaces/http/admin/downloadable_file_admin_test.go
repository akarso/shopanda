package admin_test

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	adminapp "github.com/akarso/shopanda/internal/application/admin"
	catalogapp "github.com/akarso/shopanda/internal/application/catalog"
	downloadapp "github.com/akarso/shopanda/internal/application/download"
	"github.com/akarso/shopanda/internal/domain/catalog"
	domain "github.com/akarso/shopanda/internal/domain/download"
	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/domain/media"
	"github.com/akarso/shopanda/internal/domain/rbac"
	"github.com/akarso/shopanda/internal/interfaces/http/admin"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/auth/testhelper"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/logger"
)

func init() {
	sql.Register("shopanda-dl-admin-fake-tx", dlAdminFakeTxDriver{})
}

type dlAdminFakeTxDriver struct{}

func (dlAdminFakeTxDriver) Open(string) (driver.Conn, error) { return dlAdminFakeTxConn{}, nil }

type dlAdminFakeTxConn struct{}

func (dlAdminFakeTxConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (dlAdminFakeTxConn) Close() error                        { return nil }
func (dlAdminFakeTxConn) Begin() (driver.Tx, error)           { return dlAdminFakeTx{}, nil }

type dlAdminFakeTx struct{}

func (dlAdminFakeTx) Commit() error   { return nil }
func (dlAdminFakeTx) Rollback() error { return nil }

var (
	dlAdminFakeTxDBOnce sync.Once
	dlAdminFakeTxDB     *sql.DB
)

func dlAdminTestTxDB() *sql.DB {
	dlAdminFakeTxDBOnce.Do(func() {
		db, err := sql.Open("shopanda-dl-admin-fake-tx", "")
		if err != nil {
			panic(err)
		}
		dlAdminFakeTxDB = db
	})
	return dlAdminFakeTxDB
}

type dlAdminProductRepo struct {
	byID map[string]*catalog.Product
}

func (s *dlAdminProductRepo) Create(context.Context, *catalog.Product) error { return nil }
func (s *dlAdminProductRepo) Update(context.Context, *catalog.Product) error { return nil }
func (s *dlAdminProductRepo) Delete(context.Context, string) error           { return nil }
func (s *dlAdminProductRepo) FindByID(_ context.Context, id string) (*catalog.Product, error) {
	return s.byID[id], nil
}
func (s *dlAdminProductRepo) FindBySlug(context.Context, string) (*catalog.Product, error) {
	return nil, nil
}
func (s *dlAdminProductRepo) FindByCategoryID(context.Context, string, int, int) ([]catalog.Product, error) {
	return nil, nil
}
func (s *dlAdminProductRepo) List(context.Context, catalog.ListFilter) ([]catalog.Product, error) {
	return nil, nil
}
func (s *dlAdminProductRepo) WithTx(*sql.Tx) catalog.ProductRepository { return s }
func (s *dlAdminProductRepo) LockForUpdate(_ context.Context, id string) (catalog.Type, error) {
	if p := s.byID[id]; p != nil {
		return p.Type, nil
	}
	return "", apperror.NotFound("product not found")
}

type dlAdminFileRepo struct {
	rows map[string][]catalog.DownloadableFile
}

func (s *dlAdminFileRepo) ListByProductID(_ context.Context, productID string) ([]catalog.DownloadableFile, error) {
	out := s.rows[productID]
	if out == nil {
		return []catalog.DownloadableFile{}, nil
	}
	return out, nil
}
func (s *dlAdminFileRepo) CountByProductID(_ context.Context, productID string) (int, error) {
	return len(s.rows[productID]), nil
}
func (s *dlAdminFileRepo) CountByAssetID(_ context.Context, assetID string) (int, error) {
	n := 0
	for _, rows := range s.rows {
		for _, r := range rows {
			if r.AssetID == assetID {
				n++
			}
		}
	}
	return n, nil
}
func (s *dlAdminFileRepo) Upsert(_ context.Context, f *catalog.DownloadableFile) error {
	rows := s.rows[f.ProductID]
	for i := range rows {
		if rows[i].AssetID == f.AssetID {
			rows[i] = *f
			s.rows[f.ProductID] = rows
			return nil
		}
	}
	if s.rows == nil {
		s.rows = map[string][]catalog.DownloadableFile{}
	}
	s.rows[f.ProductID] = append(rows, *f)
	return nil
}
func (s *dlAdminFileRepo) Remove(_ context.Context, productID, assetID string) error {
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
	return nil
}
func (s *dlAdminFileRepo) WithTx(*sql.Tx) catalog.DownloadableFileRepository { return s }

type dlAdminAssetRepo struct {
	byID map[string]*media.Asset
}

func (s *dlAdminAssetRepo) Save(context.Context, *media.Asset) error { return nil }
func (s *dlAdminAssetRepo) FindByID(_ context.Context, id string) (*media.Asset, error) {
	return s.byID[id], nil
}
func (s *dlAdminAssetRepo) List(context.Context, int, int) ([]media.Asset, error) { return nil, nil }
func (s *dlAdminAssetRepo) Delete(context.Context, string) error                  { return nil }
func (s *dlAdminAssetRepo) WithTx(*sql.Tx) media.AssetRepository                  { return s }
func (s *dlAdminAssetRepo) LockForUpdate(_ context.Context, id string) (*media.Asset, error) {
	if s.byID == nil || s.byID[id] == nil {
		return nil, apperror.NotFound("asset not found")
	}
	return s.byID[id], nil
}

type dlAdminSettingsRepo struct {
	byProduct map[string]*domain.GrantSettings
}

func (s *dlAdminSettingsRepo) FindByProductID(_ context.Context, productID string) (*domain.GrantSettings, error) {
	return s.byProduct[productID], nil
}
func (s *dlAdminSettingsRepo) Upsert(_ context.Context, row *domain.GrantSettings) error {
	if s.byProduct == nil {
		s.byProduct = map[string]*domain.GrantSettings{}
	}
	cp := *row
	s.byProduct[row.ProductID] = &cp
	return nil
}
func (s *dlAdminSettingsRepo) Remove(_ context.Context, productID string) error {
	if _, ok := s.byProduct[productID]; !ok {
		return apperror.NotFound("downloadable grant settings not found")
	}
	delete(s.byProduct, productID)
	return nil
}

type dlAdminRetargetHook struct {
	calls [][2]string
}

func newDownloadableAdminMux(t *testing.T) (*http.ServeMux, *catalogapp.DownloadableFileService, *downloadapp.Service, *dlAdminFileRepo, *dlAdminSettingsRepo, *dlAdminRetargetHook) {
	t.Helper()
	products := &dlAdminProductRepo{byID: map[string]*catalog.Product{
		"p1": {ID: "p1", Type: catalog.TypeDownloadable, Status: catalog.StatusDraft},
		"p2": {ID: "p2", Type: catalog.TypeDownloadable, Status: catalog.StatusDraft},
	}}
	files := &dlAdminFileRepo{rows: map[string][]catalog.DownloadableFile{}}
	assets := &dlAdminAssetRepo{byID: map[string]*media.Asset{
		"a1": {ID: "a1", Filename: "a.pdf", MimeType: "application/pdf", Path: media.PrivateDownloadsPrefix + "a1/a.pdf"},
		"a2": {ID: "a2", Filename: "b.pdf", MimeType: "application/pdf", Path: media.PrivateDownloadsPrefix + "a2/b.pdf"},
	}}
	hook := &dlAdminRetargetHook{}
	fileSvc := catalogapp.NewDownloadableFileService(products, files, assets, dlAdminTestTxDB()).
		WithGrantRetarget(func(_ context.Context, _ *sql.Tx, oldAssetID, newAssetID string) (catalogapp.GrantRetargetOutcome, error) {
			hook.calls = append(hook.calls, [2]string{oldAssetID, newAssetID})
			return catalogapp.GrantRetargetOutcome{Moved: 4, Revoked: nil}, nil
		})
	settings := &dlAdminSettingsRepo{byProduct: map[string]*domain.GrantSettings{}}
	grantSvc := downloadapp.NewService(newGrantAdminMemRepo(), nil).
		WithSettings(settings).
		WithProductExists(downloadapp.ProductExistsFunc(func(_ context.Context, productID string) (bool, error) {
			return products.byID[productID] != nil, nil
		}))
	h := admin.NewDownloadableFileAdminHandler(fileSvc, grantSvc, assets, adminapp.NewAuditor(logger.New("error")))
	wrap := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := catalog.WithOperatorProductReadScope(r.Context())
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/admin/products/{id}/downloadable-files", wrap(admin.RequirePermission(rbac.ProductsRead)(h.ListFiles())))
	mux.Handle("POST /api/v1/admin/products/{id}/downloadable-files", wrap(admin.RequirePermission(rbac.ProductsWrite)(h.AttachFile())))
	mux.Handle("PUT /api/v1/admin/products/{id}/downloadable-files", wrap(admin.RequirePermission(rbac.ProductsWrite)(h.ReplaceFile())))
	mux.Handle("DELETE /api/v1/admin/products/{id}/downloadable-files/{assetId}", wrap(admin.RequirePermission(rbac.ProductsWrite)(h.RemoveFile())))
	mux.Handle("GET /api/v1/admin/products/{id}/downloadable-grant-settings", wrap(admin.RequirePermission(rbac.ProductsRead)(h.GetSettings())))
	mux.Handle("PUT /api/v1/admin/products/{id}/downloadable-grant-settings", wrap(admin.RequirePermission(rbac.ProductsWrite)(h.PutSettings())))
	mux.Handle("DELETE /api/v1/admin/products/{id}/downloadable-grant-settings", wrap(admin.RequirePermission(rbac.ProductsWrite)(h.DeleteSettings())))
	return mux, fileSvc, grantSvc, files, settings, hook
}

func TestDownloadableFileAdmin_AttachReplaceRemoveSettings(t *testing.T) {
	mux, _, grantSvc, files, settings, hook := newDownloadableAdminMux(t)

	serve := func(method, target string, body []byte, role identity.Role) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, target, bytes.NewReader(body))
		req = testhelper.AuthenticatedRequest(req, "user-1", role)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	attach := []byte(`{"asset_id":"a1","position":2,"label":"Guide"}`)
	if rec := serve(http.MethodPost, "/api/v1/admin/products/p1/downloadable-files", attach, identity.RoleEditor); rec.Code != http.StatusOK {
		t.Fatalf("attach = %d body=%s", rec.Code, rec.Body.String())
	}
	if len(files.rows["p1"]) != 1 || files.rows["p1"][0].Position != 2 {
		t.Fatalf("files after attach = %+v", files.rows["p1"])
	}

	replace := []byte(`{"old_asset_id":"a1","asset_id":"a2"}`)
	rec := serve(http.MethodPut, "/api/v1/admin/products/p1/downloadable-files", replace, identity.RoleEditor)
	if rec.Code != http.StatusOK {
		t.Fatalf("replace = %d body=%s", rec.Code, rec.Body.String())
	}
	var replaceEnv struct {
		Data map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &replaceEnv); err != nil {
		t.Fatalf("replace json: %v", err)
	}
	if replaceEnv.Data["grants_moved"] != float64(4) || replaceEnv.Data["grants_revoked"] != float64(0) {
		t.Fatalf("replace counts = %+v", replaceEnv.Data)
	}
	if len(hook.calls) != 1 || hook.calls[0] != [2]string{"a1", "a2"} {
		t.Fatalf("retarget hook calls = %+v, want [[a1 a2]]", hook.calls)
	}
	if len(files.rows["p1"]) != 1 || files.rows["p1"][0].AssetID != "a2" || files.rows["p1"][0].Position != 2 || files.rows["p1"][0].Label != "Guide" {
		t.Fatalf("replace preserve = %+v", files.rows["p1"])
	}

	if rec := serve(http.MethodGet, "/api/v1/admin/products/p1/downloadable-files", nil, identity.RoleEditor); rec.Code != http.StatusOK {
		t.Fatalf("list = %d body=%s", rec.Code, rec.Body.String())
	}

	putSettings := []byte(`{"expiry_days":30,"download_limit":5}`)
	if rec := serve(http.MethodPut, "/api/v1/admin/products/p1/downloadable-grant-settings", putSettings, identity.RoleEditor); rec.Code != http.StatusOK {
		t.Fatalf("put settings = %d body=%s", rec.Code, rec.Body.String())
	}
	if settings.byProduct["p1"] == nil {
		t.Fatal("expected settings row")
	}
	if rec := serve(http.MethodDelete, "/api/v1/admin/products/p1/downloadable-grant-settings", nil, identity.RoleEditor); rec.Code != http.StatusOK {
		t.Fatalf("delete settings = %d", rec.Code)
	}
	// Idempotent clear when already empty.
	if rec := serve(http.MethodDelete, "/api/v1/admin/products/p1/downloadable-grant-settings", nil, identity.RoleEditor); rec.Code != http.StatusOK {
		t.Fatalf("idempotent delete settings = %d body=%s", rec.Code, rec.Body.String())
	}
	if rec := serve(http.MethodGet, "/api/v1/admin/products/missing/downloadable-grant-settings", nil, identity.RoleEditor); rec.Code != http.StatusNotFound {
		t.Fatalf("missing product settings GET = %d, want 404", rec.Code)
	}
	_ = grantSvc

	if rec := serve(http.MethodDelete, "/api/v1/admin/products/p1/downloadable-files/a2", nil, identity.RoleEditor); rec.Code != http.StatusOK {
		t.Fatalf("remove = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestDownloadableFileAdmin_Replace_SharedAssetRequiresGlobalRetarget(t *testing.T) {
	mux, _, _, files, _, hook := newDownloadableAdminMux(t)
	files.rows["p1"] = []catalog.DownloadableFile{{ProductID: "p1", AssetID: "a1", Position: 1, Label: "A"}}
	files.rows["p2"] = []catalog.DownloadableFile{{ProductID: "p2", AssetID: "a1", Position: 0, Label: "Shared"}}

	serve := func(body []byte) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/products/p1/downloadable-files", bytes.NewReader(body))
		req = testhelper.AuthenticatedRequest(req, "user-1", identity.RoleEditor)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	rec := serve([]byte(`{"old_asset_id":"a1","asset_id":"a2"}`))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("shared replace without flag = %d body=%s, want 422", rec.Code, rec.Body.String())
	}
	if len(hook.calls) != 0 {
		t.Fatalf("retarget must not run on refuse: %+v", hook.calls)
	}

	rec = serve([]byte(`{"old_asset_id":"a1","asset_id":"a2","global_retarget":true}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("shared replace with flag = %d body=%s", rec.Code, rec.Body.String())
	}
	if len(hook.calls) != 1 || hook.calls[0] != [2]string{"a1", "a2"} {
		t.Fatalf("retarget hook = %+v", hook.calls)
	}
	var env struct {
		Data map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("json: %v", err)
	}
	if env.Data["grants_moved"] != float64(4) {
		t.Fatalf("grants_moved = %v", env.Data["grants_moved"])
	}
}

func TestDownloadableFileAdmin_SupportForbiddenOnWrites(t *testing.T) {
	mux, _, _, _, _, _ := newDownloadableAdminMux(t)
	serve := func(method, target string, body []byte, role identity.Role) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, bytes.NewReader(body))
		req = testhelper.AuthenticatedRequest(req, "user-1", role)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
	if rec := serve(http.MethodGet, "/api/v1/admin/products/p1/downloadable-files", nil, identity.RoleSupport); rec.Code != http.StatusOK {
		t.Fatalf("support GET files = %d", rec.Code)
	}
	if rec := serve(http.MethodPost, "/api/v1/admin/products/p1/downloadable-files", []byte(`{"asset_id":"a1"}`), identity.RoleSupport); rec.Code != http.StatusForbidden {
		t.Fatalf("support POST = %d, want 403", rec.Code)
	}
	if rec := serve(http.MethodPut, "/api/v1/admin/products/p1/downloadable-files", []byte(`{"old_asset_id":"a1","asset_id":"a2"}`), identity.RoleSupport); rec.Code != http.StatusForbidden {
		t.Fatalf("support PUT = %d, want 403", rec.Code)
	}
	if rec := serve(http.MethodDelete, "/api/v1/admin/products/p1/downloadable-files/a1", nil, identity.RoleSupport); rec.Code != http.StatusForbidden {
		t.Fatalf("support DELETE = %d, want 403", rec.Code)
	}
	if rec := serve(http.MethodPut, "/api/v1/admin/products/p1/downloadable-grant-settings", []byte(`{}`), identity.RoleSupport); rec.Code != http.StatusForbidden {
		t.Fatalf("support settings PUT = %d, want 403", rec.Code)
	}
}

func TestDownloadGrantAdmin_EditorForbiddenOnWrites(t *testing.T) {
	repo := newGrantAdminMemRepo()
	svc := downloadapp.NewService(repo, nil)
	g := seedGrant(t, repo, "cust-1", domain.OrderItemID(id.New(), "v1"))
	h := admin.NewDownloadGrantAdminHandler(svc, &grantAdminOrderRepo{}, adminapp.NewAuditor(logger.New("error")))
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/admin/download-grants", admin.RequirePermission(rbac.DownloadsRead)(h.List()))
	mux.Handle("POST /api/v1/admin/download-grants/{id}/revoke", admin.RequirePermission(rbac.DownloadsWrite)(h.Revoke()))
	mux.Handle("POST /api/v1/admin/download-grants/{id}/reissue", admin.RequirePermission(rbac.DownloadsWrite)(h.Reissue()))

	serve := func(method, target string, body []byte, role identity.Role) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, bytes.NewReader(body))
		req = testhelper.AuthenticatedRequest(req, "user-1", role)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	if rec := serve(http.MethodGet, "/api/v1/admin/download-grants?customer_id=cust-1", nil, identity.RoleSupport); rec.Code != http.StatusOK {
		t.Fatalf("support list = %d", rec.Code)
	}
	if rec := serve(http.MethodGet, "/api/v1/admin/download-grants?customer_id=cust-1", nil, identity.RoleEditor); rec.Code != http.StatusForbidden {
		t.Fatalf("editor list = %d, want 403", rec.Code)
	}
	revokeBody, _ := json.Marshal(map[string]string{"reason": "test"})
	if rec := serve(http.MethodPost, "/api/v1/admin/download-grants/"+g.ID+"/revoke", revokeBody, identity.RoleEditor); rec.Code != http.StatusForbidden {
		t.Fatalf("editor revoke = %d, want 403", rec.Code)
	}
	reissueBody, _ := json.Marshal(map[string]interface{}{"expiry_days": 7, "reason": "test"})
	if rec := serve(http.MethodPost, "/api/v1/admin/download-grants/"+g.ID+"/reissue", reissueBody, identity.RoleEditor); rec.Code != http.StatusForbidden {
		t.Fatalf("editor reissue = %d, want 403", rec.Code)
	}
	if rec := serve(http.MethodPost, "/api/v1/admin/download-grants/"+g.ID+"/reissue", reissueBody, identity.RoleSupport); rec.Code != http.StatusOK {
		t.Fatalf("support reissue = %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestDownloadableFileAdmin_Attach_EmitsProductUpdatedEvent pins PR-1080:
// attaching a downloadable file publishes catalog.EventProductUpdated so
// MissingDownloadableFile / Purchasable are refreshed in the search index.
// Payload includes name/slug/status when SetProducts is wired.
func TestDownloadableFileAdmin_Attach_EmitsProductUpdatedEvent(t *testing.T) {
	products := &dlAdminProductRepo{byID: map[string]*catalog.Product{
		"p1": {ID: "p1", Type: catalog.TypeDownloadable, Status: catalog.StatusDraft, Name: "Guide", Slug: "guide"},
	}}
	files := &dlAdminFileRepo{rows: map[string][]catalog.DownloadableFile{}}
	assets := &dlAdminAssetRepo{byID: map[string]*media.Asset{
		"a1": {ID: "a1", Filename: "a.pdf", MimeType: "application/pdf", Path: media.PrivateDownloadsPrefix + "a1/a.pdf"},
	}}
	fileSvc := catalogapp.NewDownloadableFileService(products, files, assets, dlAdminTestTxDB())
	grantSvc := downloadapp.NewService(newGrantAdminMemRepo(), nil).
		WithSettings(&dlAdminSettingsRepo{byProduct: map[string]*domain.GrantSettings{}}).
		WithProductExists(downloadapp.ProductExistsFunc(func(_ context.Context, productID string) (bool, error) {
			return products.byID[productID] != nil, nil
		}))
	h := admin.NewDownloadableFileAdminHandler(fileSvc, grantSvc, assets, adminapp.NewAuditor(logger.New("error")))
	h.SetProducts(products)
	bus := event.NewBus(logger.New("error"))
	h.SetBus(bus)
	var captured catalog.ProductUpdatedData
	bus.On(catalog.EventProductUpdated, func(_ context.Context, evt event.Event) error {
		data, ok := evt.Data.(catalog.ProductUpdatedData)
		if !ok {
			t.Errorf("data type %T", evt.Data)
			return nil
		}
		captured = data
		return nil
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/products/p1/downloadable-files", bytes.NewReader([]byte(`{"asset_id":"a1"}`)))
	req.SetPathValue("id", "p1")
	req = req.WithContext(catalog.WithOperatorProductReadScope(req.Context()))
	req = testhelper.AuthenticatedRequest(req, "user-1", identity.RoleEditor)
	rec := httptest.NewRecorder()
	h.AttachFile().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("attach = %d body=%s", rec.Code, rec.Body.String())
	}
	if captured.ProductID != "p1" || captured.Name != "Guide" || captured.Slug != "guide" || captured.Status != catalog.StatusDraft {
		t.Fatalf("captured = %+v", captured)
	}
}

// TestDownloadableFileAdmin_Remove_EmitsProductUpdatedEvent is Attach's
// counterpart for last-file remove (Purchasable may flip false).
func TestDownloadableFileAdmin_Remove_EmitsProductUpdatedEvent(t *testing.T) {
	products := &dlAdminProductRepo{byID: map[string]*catalog.Product{
		"p1": {ID: "p1", Type: catalog.TypeDownloadable, Status: catalog.StatusDraft},
	}}
	files := &dlAdminFileRepo{rows: map[string][]catalog.DownloadableFile{
		"p1": {{ProductID: "p1", AssetID: "a1", Position: 0}},
	}}
	assets := &dlAdminAssetRepo{byID: map[string]*media.Asset{
		"a1": {ID: "a1", Filename: "a.pdf", MimeType: "application/pdf", Path: media.PrivateDownloadsPrefix + "a1/a.pdf"},
	}}
	fileSvc := catalogapp.NewDownloadableFileService(products, files, assets, dlAdminTestTxDB())
	grantSvc := downloadapp.NewService(newGrantAdminMemRepo(), nil).
		WithSettings(&dlAdminSettingsRepo{byProduct: map[string]*domain.GrantSettings{}}).
		WithProductExists(downloadapp.ProductExistsFunc(func(_ context.Context, productID string) (bool, error) {
			return products.byID[productID] != nil, nil
		}))
	h := admin.NewDownloadableFileAdminHandler(fileSvc, grantSvc, assets, adminapp.NewAuditor(logger.New("error")))
	bus := event.NewBus(logger.New("error"))
	h.SetBus(bus)
	asyncRan := make(chan struct{})
	bus.OnAsync(catalog.EventProductUpdated, func(context.Context, event.Event) error {
		close(asyncRan)
		return nil
	})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/products/p1/downloadable-files/a1", nil)
	req.SetPathValue("id", "p1")
	req.SetPathValue("assetId", "a1")
	req = req.WithContext(catalog.WithOperatorProductReadScope(req.Context()))
	req = testhelper.AuthenticatedRequest(req, "user-1", identity.RoleEditor)
	rec := httptest.NewRecorder()
	h.RemoveFile().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("remove = %d body=%s", rec.Code, rec.Body.String())
	}
	select {
	case <-asyncRan:
	case <-time.After(2 * time.Second):
		t.Fatal("expected async ProductUpdated after remove")
	}
}

// TestDownloadableFileAdmin_Replace_EmitsProductUpdatedEvent pins replace
// also publishes (MissingDownloadableFile unchanged, but purchasable axes
// stay event-driven consistently with attach/remove).
func TestDownloadableFileAdmin_Replace_EmitsProductUpdatedEvent(t *testing.T) {
	products := &dlAdminProductRepo{byID: map[string]*catalog.Product{
		"p1": {ID: "p1", Type: catalog.TypeDownloadable, Status: catalog.StatusActive, Name: "Pack", Slug: "pack"},
	}}
	files := &dlAdminFileRepo{rows: map[string][]catalog.DownloadableFile{
		"p1": {{ProductID: "p1", AssetID: "a1", Position: 0}},
	}}
	assets := &dlAdminAssetRepo{byID: map[string]*media.Asset{
		"a1": {ID: "a1", Filename: "a.pdf", MimeType: "application/pdf", Path: media.PrivateDownloadsPrefix + "a1/a.pdf"},
		"a2": {ID: "a2", Filename: "b.pdf", MimeType: "application/pdf", Path: media.PrivateDownloadsPrefix + "a2/b.pdf"},
	}}
	fileSvc := catalogapp.NewDownloadableFileService(products, files, assets, dlAdminTestTxDB()).
		WithGrantRetarget(func(_ context.Context, _ *sql.Tx, _, _ string) (catalogapp.GrantRetargetOutcome, error) {
			return catalogapp.GrantRetargetOutcome{}, nil
		})
	grantSvc := downloadapp.NewService(newGrantAdminMemRepo(), nil).
		WithSettings(&dlAdminSettingsRepo{byProduct: map[string]*domain.GrantSettings{}}).
		WithProductExists(downloadapp.ProductExistsFunc(func(_ context.Context, productID string) (bool, error) {
			return products.byID[productID] != nil, nil
		}))
	h := admin.NewDownloadableFileAdminHandler(fileSvc, grantSvc, assets, adminapp.NewAuditor(logger.New("error")))
	h.SetProducts(products)
	bus := event.NewBus(logger.New("error"))
	h.SetBus(bus)
	var captured catalog.ProductUpdatedData
	bus.On(catalog.EventProductUpdated, func(_ context.Context, evt event.Event) error {
		data, ok := evt.Data.(catalog.ProductUpdatedData)
		if ok {
			captured = data
		}
		return nil
	})

	body := []byte(`{"old_asset_id":"a1","asset_id":"a2"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/products/p1/downloadable-files", bytes.NewReader(body))
	req.SetPathValue("id", "p1")
	req = req.WithContext(catalog.WithOperatorProductReadScope(req.Context()))
	req = testhelper.AuthenticatedRequest(req, "user-1", identity.RoleEditor)
	rec := httptest.NewRecorder()
	h.ReplaceFile().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("replace = %d body=%s", rec.Code, rec.Body.String())
	}
	if captured.ProductID != "p1" || captured.Slug != "pack" {
		t.Fatalf("captured = %+v", captured)
	}
}

// TestDownloadableFileAdmin_GrantSettings_NoProductUpdatedEvent pins that
// grant-settings writes do not publish (fulfillment config, not visibility).
func TestDownloadableFileAdmin_GrantSettings_NoProductUpdatedEvent(t *testing.T) {
	products := &dlAdminProductRepo{byID: map[string]*catalog.Product{
		"p1": {ID: "p1", Type: catalog.TypeDownloadable, Status: catalog.StatusDraft},
	}}
	files := &dlAdminFileRepo{rows: map[string][]catalog.DownloadableFile{}}
	assets := &dlAdminAssetRepo{byID: map[string]*media.Asset{}}
	fileSvc := catalogapp.NewDownloadableFileService(products, files, assets, dlAdminTestTxDB())
	grantSvc := downloadapp.NewService(newGrantAdminMemRepo(), nil).
		WithSettings(&dlAdminSettingsRepo{byProduct: map[string]*domain.GrantSettings{}}).
		WithProductExists(downloadapp.ProductExistsFunc(func(_ context.Context, productID string) (bool, error) {
			return products.byID[productID] != nil, nil
		}))
	h := admin.NewDownloadableFileAdminHandler(fileSvc, grantSvc, assets, adminapp.NewAuditor(logger.New("error")))
	bus := event.NewBus(logger.New("error"))
	h.SetBus(bus)
	published := false
	bus.On(catalog.EventProductUpdated, func(context.Context, event.Event) error {
		published = true
		return nil
	})

	body := []byte(`{"expiry_days":14}`)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/products/p1/downloadable-grant-settings", bytes.NewReader(body))
	req.SetPathValue("id", "p1")
	req = req.WithContext(catalog.WithOperatorProductReadScope(req.Context()))
	req = testhelper.AuthenticatedRequest(req, "user-1", identity.RoleEditor)
	rec := httptest.NewRecorder()
	h.PutSettings().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("put settings = %d body=%s", rec.Code, rec.Body.String())
	}
	if published {
		t.Fatal("grant-settings write must not publish ProductUpdated")
	}
}

// TestDownloadableFileAdmin_Attach_NoBusStillSucceeds pins SetBus optional.
func TestDownloadableFileAdmin_Attach_NoBusStillSucceeds(t *testing.T) {
	products := &dlAdminProductRepo{byID: map[string]*catalog.Product{
		"p1": {ID: "p1", Type: catalog.TypeDownloadable, Status: catalog.StatusDraft},
	}}
	files := &dlAdminFileRepo{rows: map[string][]catalog.DownloadableFile{}}
	assets := &dlAdminAssetRepo{byID: map[string]*media.Asset{
		"a1": {ID: "a1", Filename: "a.pdf", MimeType: "application/pdf", Path: media.PrivateDownloadsPrefix + "a1/a.pdf"},
	}}
	fileSvc := catalogapp.NewDownloadableFileService(products, files, assets, dlAdminTestTxDB())
	grantSvc := downloadapp.NewService(newGrantAdminMemRepo(), nil).
		WithSettings(&dlAdminSettingsRepo{byProduct: map[string]*domain.GrantSettings{}}).
		WithProductExists(downloadapp.ProductExistsFunc(func(_ context.Context, productID string) (bool, error) {
			return products.byID[productID] != nil, nil
		}))
	h := admin.NewDownloadableFileAdminHandler(fileSvc, grantSvc, assets, adminapp.NewAuditor(logger.New("error")))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/products/p1/downloadable-files", bytes.NewReader([]byte(`{"asset_id":"a1"}`)))
	req.SetPathValue("id", "p1")
	req = req.WithContext(catalog.WithOperatorProductReadScope(req.Context()))
	req = testhelper.AuthenticatedRequest(req, "user-1", identity.RoleEditor)
	rec := httptest.NewRecorder()
	h.AttachFile().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("attach = %d body=%s", rec.Code, rec.Body.String())
	}
}
