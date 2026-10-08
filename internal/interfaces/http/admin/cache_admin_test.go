package admin_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	adminapp "github.com/akarso/shopanda/internal/application/admin"
	cacheapp "github.com/akarso/shopanda/internal/application/cache"
	"github.com/akarso/shopanda/internal/domain/cache"
	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/domain/rbac"
	"github.com/akarso/shopanda/internal/domain/store"
	"github.com/akarso/shopanda/internal/interfaces/http/admin"
	"github.com/akarso/shopanda/internal/platform/auth/testhelper"
	"github.com/akarso/shopanda/internal/platform/logger"
)

type memCache struct {
	keys      map[string]string
	tags      map[string]map[string]struct{}
	prefixErr error
}

func newMemCache() *memCache {
	return &memCache{keys: make(map[string]string), tags: make(map[string]map[string]struct{})}
}

func (m *memCache) Get(key string, _ any) (bool, error) {
	_, ok := m.keys[key]
	return ok, nil
}
func (m *memCache) Set(key string, _ any, _ time.Duration) error     { m.keys[key] = "v"; return nil }
func (m *memCache) Incr(string, int64, time.Duration) (int64, error) { return 0, nil }
func (m *memCache) CompareAndSubtract(string, int64) (int64, error)  { return 0, nil }
func (m *memCache) Delete(key string) error                          { delete(m.keys, key); return nil }
func (m *memCache) DeleteByPrefix(_ context.Context, prefix string) error {
	if m.prefixErr != nil {
		return m.prefixErr
	}
	for k := range m.keys {
		if strings.HasPrefix(k, prefix) {
			delete(m.keys, k)
		}
	}
	return nil
}
func (m *memCache) SetWithTags(_ context.Context, key string, value any, ttl time.Duration, tags ...string) error {
	_ = ttl
	_ = value
	m.keys[key] = "v"
	for _, tag := range tags {
		if m.tags[tag] == nil {
			m.tags[tag] = make(map[string]struct{})
		}
		m.tags[tag][key] = struct{}{}
	}
	return nil
}
func (m *memCache) DeleteByTag(_ context.Context, tag string) (int64, error) {
	members := m.tags[tag]
	delete(m.tags, tag)
	var n int64
	for k := range members {
		delete(m.keys, k)
		n++
	}
	return n, nil
}
func (m *memCache) Stats(context.Context) (cache.Stats, error) {
	return cache.Stats{Backend: "memory", Keys: int64(len(m.keys))}, nil
}
func (m *memCache) FlushAll(context.Context) (int64, error) {
	n := int64(len(m.keys))
	m.keys = make(map[string]string)
	m.tags = make(map[string]map[string]struct{})
	return n, nil
}

var _ cache.Cache = (*memCache)(nil)

func newCacheAdminRouter(h *admin.CacheAdminHandler) *http.ServeMux {
	return newCacheAdminRouterWithCheck(h, rbac.HasPermission)
}

func newCacheAdminRouterWithCheck(h *admin.CacheAdminHandler, has func(identity.Role, rbac.Permission) bool) *http.ServeMux {
	admin.SetCacheAdminPermissionCheck(h, has)
	requireRead := admin.RequirePermissionUsing(has, rbac.CacheRead)
	requireClear := admin.RequireAnyPermissionUsing(has, rbac.CacheWrite, rbac.CacheClearAll)
	requirePurgeURL := admin.RequirePermissionUsing(has, rbac.CachePurgeURL)
	withAdminContext := admin.AdminContextMiddleware()
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/admin/cache/stats", withAdminContext(requireRead(h.Stats())))
	mux.Handle("POST /api/v1/admin/cache/clear", withAdminContext(requireClear(h.Clear())))
	mux.Handle("POST /api/v1/admin/cache/purge-url", withAdminContext(requirePurgeURL(h.PurgeURL())))
	return mux
}

func allowRole(role identity.Role, perms ...rbac.Permission) func(identity.Role, rbac.Permission) bool {
	granted := make(map[rbac.Permission]struct{}, len(perms))
	for _, p := range perms {
		granted[p] = struct{}{}
	}
	return func(r identity.Role, p rbac.Permission) bool {
		if r != role {
			return false
		}
		_, ok := granted[p]
		return ok
	}
}

func newCacheAdminHandler(t *testing.T, backend cache.Cache, l1 []cacheapp.L1Source) *admin.CacheAdminHandler {
	t.Helper()
	return admin.NewCacheAdminHandler(cacheapp.NewAdminService(backend, l1), adminapp.NewAuditor(logger.New("error")))
}

func newCacheAdminHandlerWithAudit(t *testing.T, backend cache.Cache, l1 []cacheapp.L1Source) (*admin.CacheAdminHandler, *fakeAuditLogRepository) {
	t.Helper()
	auditor := adminapp.NewAuditor(logger.New("error"))
	repo := &fakeAuditLogRepository{}
	auditor.SetAuditLogRepository(repo)
	return admin.NewCacheAdminHandler(cacheapp.NewAdminService(backend, l1), auditor), repo
}

func TestCacheAdminHandler_Stats(t *testing.T) {
	backend := newMemCache()
	_ = backend.Set("a", "1", 0)
	_ = backend.Set("b", "2", 0)
	h := newCacheAdminHandler(t, backend, []cacheapp.L1Source{{
		Name: "rbac.catalog",
		Snap: func() (int, int64, int64) { return 1, 7, 3 },
	}})
	mux := newCacheAdminRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/cache/stats", nil)
	req = testhelper.AdminRequest(req, "admin-1")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data struct {
			L2 struct {
				Backend string `json:"backend"`
				Keys    int64  `json:"keys"`
			} `json:"l2"`
			L1 []struct {
				Name    string `json:"name"`
				Entries int    `json:"entries"`
				Hits    int64  `json:"hits"`
				Misses  int64  `json:"misses"`
			} `json:"l1"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data.L2.Backend != "memory" || resp.Data.L2.Keys != 2 {
		t.Fatalf("l2 = %+v", resp.Data.L2)
	}
	if len(resp.Data.L1) != 1 || resp.Data.L1[0].Name != "rbac.catalog" || resp.Data.L1[0].Hits != 7 {
		t.Fatalf("l1 = %+v", resp.Data.L1)
	}
}

func TestCacheAdminHandler_Stats_Forbidden(t *testing.T) {
	h := newCacheAdminHandler(t, newMemCache(), nil)
	mux := newCacheAdminRouter(h)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/cache/stats", nil)
	req = testhelper.AuthenticatedRequest(req, "support-1", identity.RoleSupport)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body=%s", rec.Code, rec.Body.String())
	}
}

func TestCacheAdminHandler_Clear_PrefixTagKey(t *testing.T) {
	backend := newMemCache()
	ctx := context.Background()
	_ = backend.SetWithTags(ctx, "product:1:en", "a", 0, "product:1")
	_ = backend.SetWithTags(ctx, "product:2:en", "b", 0, "product:2")
	_ = backend.Set("keep", "c", 0)
	h := newCacheAdminHandler(t, backend, nil)
	mux := newCacheAdminRouter(h)

	post := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/cache/clear", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req = testhelper.AdminRequest(req, "admin-1")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	rec := post(`{"prefix":"product:1:"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("prefix status = %d body=%s", rec.Code, rec.Body.String())
	}
	if _, ok := backend.keys["product:1:en"]; ok {
		t.Fatal("prefix left product:1:en")
	}
	if _, ok := backend.keys["product:2:en"]; !ok {
		t.Fatal("prefix removed product:2:en")
	}

	rec = post(`{"tag":"product:2"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("tag status = %d body=%s", rec.Code, rec.Body.String())
	}
	if _, ok := backend.keys["product:2:en"]; ok {
		t.Fatal("tag left product:2:en")
	}
	if _, ok := backend.keys["keep"]; !ok {
		t.Fatal("tag removed keep")
	}

	rec = post(`{"key":"keep"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("key status = %d body=%s", rec.Code, rec.Body.String())
	}
	if _, ok := backend.keys["keep"]; ok {
		t.Fatal("key left keep")
	}
}

func TestCacheAdminHandler_Clear_All(t *testing.T) {
	backend := newMemCache()
	_ = backend.Set("a", "1", 0)
	_ = backend.Set("b", "2", 0)
	l1Cleared := false
	h := newCacheAdminHandler(t, backend, []cacheapp.L1Source{{
		Name:  "rbac.catalog",
		Snap:  func() (int, int64, int64) { return 1, 0, 0 },
		Clear: func() { l1Cleared = true },
	}})
	mux := newCacheAdminRouter(h)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/cache/clear", bytes.NewBufferString(`{"all":true}`))
	req.Header.Set("Content-Type", "application/json")
	req = testhelper.AdminRequest(req, "admin-1")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if len(backend.keys) != 0 {
		t.Fatalf("keys left: %v", backend.keys)
	}
	if !l1Cleared {
		t.Fatal("all must clear L1")
	}
}

func TestCacheAdminHandler_Clear_AllRequiresDistinctPermission(t *testing.T) {
	h := newCacheAdminHandler(t, newMemCache(), nil)
	mux := newCacheAdminRouterWithCheck(h, allowRole(identity.RoleManager, rbac.CacheWrite))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/cache/clear", bytes.NewBufferString(`{"all":true}`))
	req.Header.Set("Content-Type", "application/json")
	req = testhelper.AuthenticatedRequest(req, "mgr-1", identity.RoleManager)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/cache/clear", bytes.NewBufferString(`{"prefix":"p:"}`))
	req.Header.Set("Content-Type", "application/json")
	req = testhelper.AuthenticatedRequest(req, "mgr-1", identity.RoleManager)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("targeted clear status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

func TestCacheAdminHandler_Clear_WriteRequiredForTargeted(t *testing.T) {
	h := newCacheAdminHandler(t, newMemCache(), nil)
	mux := newCacheAdminRouterWithCheck(h, allowRole(identity.RoleManager, rbac.CacheClearAll))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/cache/clear", bytes.NewBufferString(`{"prefix":"p:"}`))
	req.Header.Set("Content-Type", "application/json")
	req = testhelper.AuthenticatedRequest(req, "mgr-1", identity.RoleManager)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body=%s", rec.Code, rec.Body.String())
	}
}

func TestCacheAdminHandler_Clear_InvalidBody(t *testing.T) {
	h := newCacheAdminHandler(t, newMemCache(), nil)
	mux := newCacheAdminRouter(h)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/cache/clear", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req = testhelper.AdminRequest(req, "admin-1")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity && rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 422/400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestCacheAdminHandler_Clear_WhitespaceAndFalseAllAreOmitted(t *testing.T) {
	h := newCacheAdminHandler(t, newMemCache(), nil)
	mux := newCacheAdminRouter(h)
	post := func(body string) int {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/cache/clear", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req = testhelper.AdminRequest(req, "admin-1")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec.Code
	}
	for _, body := range []string{`{"prefix":""}`, `{"prefix":"  "}`, `{"all":false}`, `{"tag":" "}`, `{"key":""}`} {
		if code := post(body); code != http.StatusUnprocessableEntity && code != http.StatusBadRequest {
			t.Fatalf("body %s status = %d, want 422/400", body, code)
		}
	}
}

func TestCacheAdminHandler_Clear_InvalidSelectorBeforePermission(t *testing.T) {
	h := newCacheAdminHandler(t, newMemCache(), nil)
	mux := newCacheAdminRouterWithCheck(h, allowRole(identity.RoleManager, rbac.CacheClearAll))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/cache/clear", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req = testhelper.AuthenticatedRequest(req, "mgr-1", identity.RoleManager)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity && rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 422 (not 403 for cache.write); body=%s", rec.Code, rec.Body.String())
	}
}

func TestCacheAdminHandler_Clear_GlobPrefixIsLiteral(t *testing.T) {
	backend := newMemCache()
	_ = backend.Set("normal", "1", 0)
	_ = backend.Set("*glob", "2", 0)
	h := newCacheAdminHandler(t, backend, nil)
	mux := newCacheAdminRouterWithCheck(h, allowRole(identity.RoleManager, rbac.CacheWrite))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/cache/clear", bytes.NewBufferString(`{"prefix":"*"}`))
	req.Header.Set("Content-Type", "application/json")
	req = testhelper.AuthenticatedRequest(req, "mgr-1", identity.RoleManager)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if _, ok := backend.keys["*glob"]; ok {
		t.Fatal("prefix * must delete keys that literally start with *")
	}
	if _, ok := backend.keys["normal"]; !ok {
		t.Fatal("prefix * with cache.write must not wipe the whole store")
	}
}

func TestCacheAdminHandler_Clear_Forbidden(t *testing.T) {
	h := newCacheAdminHandler(t, newMemCache(), nil)
	mux := newCacheAdminRouter(h)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/cache/clear", bytes.NewBufferString(`{"all":true}`))
	req.Header.Set("Content-Type", "application/json")
	req = testhelper.AuthenticatedRequest(req, "support-1", identity.RoleSupport)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body=%s", rec.Code, rec.Body.String())
	}
}

func TestCacheAdminHandler_Clear_BackendErrorAuditsTarget(t *testing.T) {
	backend := newMemCache()
	backend.prefixErr = errors.New("scan failed")
	h, audits := newCacheAdminHandlerWithAudit(t, backend, nil)
	mux := newCacheAdminRouter(h)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/cache/clear", bytes.NewBufferString(`{"prefix":"product:1:"}`))
	req.Header.Set("Content-Type", "application/json")
	req = testhelper.AdminRequest(req, "admin-1")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code < 500 {
		t.Fatalf("status = %d, want 5xx; body=%s", rec.Code, rec.Body.String())
	}
	if len(audits.records) != 1 {
		t.Fatalf("audits = %d, want 1", len(audits.records))
	}
	got := audits.records[0]
	if got.Result != "error" || got.ResourceID != "product:1:" {
		t.Fatalf("audit = %+v, want error resource product:1:", got)
	}
	if got.Metadata["mode"] != string(cacheapp.ClearPrefix) || got.Metadata["target"] != "product:1:" {
		t.Fatalf("audit metadata = %+v", got.Metadata)
	}
}

func TestCacheAdminHandler_Clear_DeniedTargetAuditsTarget(t *testing.T) {
	h, audits := newCacheAdminHandlerWithAudit(t, newMemCache(), nil)
	mux := newCacheAdminRouterWithCheck(h, allowRole(identity.RoleManager, rbac.CacheClearAll))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/cache/clear", bytes.NewBufferString(`{"prefix":"p:"}`))
	req.Header.Set("Content-Type", "application/json")
	req = testhelper.AuthenticatedRequest(req, "mgr-1", identity.RoleManager)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body=%s", rec.Code, rec.Body.String())
	}
	if len(audits.records) != 1 {
		t.Fatalf("audits = %d, want 1", len(audits.records))
	}
	got := audits.records[0]
	if got.Result != "error" || got.ResourceID != "p:" {
		t.Fatalf("audit = %+v, want error resource p:", got)
	}
	if got.Metadata["mode"] != string(cacheapp.ClearPrefix) || got.Metadata["target"] != "p:" {
		t.Fatalf("audit metadata = %+v", got.Metadata)
	}
}

type purgeURLStoreRepo struct {
	stores []store.Store
	err    error
}

func (r *purgeURLStoreRepo) FindByID(context.Context, string) (*store.Store, error) {
	return nil, nil
}
func (r *purgeURLStoreRepo) FindByCode(context.Context, string) (*store.Store, error) {
	return nil, nil
}
func (r *purgeURLStoreRepo) FindByDomain(context.Context, string) (*store.Store, error) {
	return nil, nil
}
func (r *purgeURLStoreRepo) FindDefault(context.Context) (*store.Store, error) { return nil, nil }
func (r *purgeURLStoreRepo) FindAll(context.Context) ([]store.Store, error) {
	if r.err != nil {
		return nil, r.err
	}
	return append([]store.Store(nil), r.stores...), nil
}
func (r *purgeURLStoreRepo) Create(context.Context, *store.Store) error { return nil }
func (r *purgeURLStoreRepo) Update(context.Context, *store.Store) error { return nil }

func TestCacheAdminHandler_PurgeURL(t *testing.T) {
	backend := newMemCache()
	path := "/products/widget"
	guest := cacheapp.Key(cacheapp.RoutePDP, path, "", cacheapp.Vary{
		Store: "s1", Language: "en", Currency: "EUR", AuthState: cacheapp.AuthGuest,
	})
	auth := cacheapp.Key(cacheapp.RoutePDP, path, "", cacheapp.Vary{
		Store: "s1", Language: "en", Currency: "EUR", AuthState: cacheapp.AuthAuthenticated,
	})
	_ = backend.Set(guest, "g", 0)
	_ = backend.Set(auth, "a", 0)
	_ = backend.Set("keep", "k", 0)

	h, audits := newCacheAdminHandlerWithAudit(t, backend, nil)
	h.WithStores(&purgeURLStoreRepo{stores: []store.Store{{
		ID: "s1", Language: "en", Currency: "EUR",
	}}})
	mux := newCacheAdminRouter(h)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/cache/purge-url", bytes.NewBufferString(`{"path":"/products/widget"}`))
	req.Header.Set("Content-Type", "application/json")
	req = testhelper.AdminRequest(req, "admin-1")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if _, ok := backend.keys[guest]; ok {
		t.Fatal("guest key remained")
	}
	if _, ok := backend.keys[auth]; ok {
		t.Fatal("auth key remained")
	}
	if _, ok := backend.keys["keep"]; !ok {
		t.Fatal("unrelated key removed")
	}
	if len(audits.records) != 1 || audits.records[0].Result != "success" {
		t.Fatalf("audit = %+v", audits.records)
	}
}

func TestCacheAdminHandler_PurgeURL_Forbidden(t *testing.T) {
	h := newCacheAdminHandler(t, newMemCache(), nil)
	mux := newCacheAdminRouterWithCheck(h, allowRole(identity.RoleManager, rbac.CacheWrite))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/cache/purge-url", bytes.NewBufferString(`{"path":"/products/widget"}`))
	req.Header.Set("Content-Type", "application/json")
	req = testhelper.AuthenticatedRequest(req, "mgr-1", identity.RoleManager)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body=%s", rec.Code, rec.Body.String())
	}
}

func TestCacheAdminHandler_PurgeURL_Validation(t *testing.T) {
	h := newCacheAdminHandler(t, newMemCache(), nil)
	mux := newCacheAdminRouter(h)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/cache/purge-url", bytes.NewBufferString(`{"path":"/cart"}`))
	req.Header.Set("Content-Type", "application/json")
	req = testhelper.AdminRequest(req, "admin-1")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity && rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
}

func TestCacheAdminHandler_PurgeURL_DeletedZeroWhenAbsent(t *testing.T) {
	h := newCacheAdminHandler(t, newMemCache(), nil)
	h.WithStores(&purgeURLStoreRepo{stores: []store.Store{{
		ID: "s1", Language: "en", Currency: "EUR",
	}}})
	mux := newCacheAdminRouter(h)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/cache/purge-url", bytes.NewBufferString(`{"path":"/products/missing"}`))
	req.Header.Set("Content-Type", "application/json")
	req = testhelper.AdminRequest(req, "admin-1")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data struct {
			Deleted int      `json:"deleted"`
			Keys    []string `json:"keys"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Data.Deleted != 0 {
		t.Fatalf("deleted = %d, want 0 for absent keys", resp.Data.Deleted)
	}
	if len(resp.Data.Keys) < 2 {
		t.Fatalf("keys = %d, want at least guest+auth", len(resp.Data.Keys))
	}
}

func TestCacheAdminHandler_PurgeURL_MultiStoreExpand(t *testing.T) {
	backend := newMemCache()
	path := "/products/widget"
	keys := []string{
		cacheapp.Key(cacheapp.RoutePDP, path, "", cacheapp.Vary{Store: "s1", Language: "en", Currency: "EUR", AuthState: cacheapp.AuthGuest}),
		cacheapp.Key(cacheapp.RoutePDP, path, "", cacheapp.Vary{Store: "s1", Language: "en", Currency: "EUR", AuthState: cacheapp.AuthAuthenticated}),
		cacheapp.Key(cacheapp.RoutePDP, path, "", cacheapp.Vary{Store: "s2", Language: "de", Currency: "EUR", AuthState: cacheapp.AuthGuest}),
		cacheapp.Key(cacheapp.RoutePDP, path, "", cacheapp.Vary{Store: "s2", Language: "de", Currency: "EUR", AuthState: cacheapp.AuthAuthenticated}),
	}
	for _, k := range keys {
		_ = backend.Set(k, "v", 0)
	}

	h := newCacheAdminHandler(t, backend, nil)
	h.WithStores(&purgeURLStoreRepo{stores: []store.Store{
		{ID: "s1", Language: "en", Currency: "EUR"},
		{ID: "s2", Language: "de", Currency: "EUR"},
	}})
	mux := newCacheAdminRouter(h)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/cache/purge-url", bytes.NewBufferString(`{"path":"/products/widget"}`))
	req.Header.Set("Content-Type", "application/json")
	req = testhelper.AdminRequest(req, "admin-1")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data struct {
			Deleted int      `json:"deleted"`
			Keys    []string `json:"keys"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Data.Deleted != 4 {
		t.Fatalf("deleted=%d, want 4 existing keys; body=%s", resp.Data.Deleted, rec.Body.String())
	}
	if len(resp.Data.Keys) < 4 {
		t.Fatalf("keys=%d, want expand covering both stores; body=%s", len(resp.Data.Keys), rec.Body.String())
	}
	for _, k := range keys {
		if _, ok := backend.keys[k]; ok {
			t.Fatalf("key remained: %s", k)
		}
	}
}
