package storefront_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	downloadapp "github.com/akarso/shopanda/internal/application/download"
	domain "github.com/akarso/shopanda/internal/domain/download"
	"github.com/akarso/shopanda/internal/domain/media"
	storefront "github.com/akarso/shopanda/internal/interfaces/http/storefront"
	"github.com/akarso/shopanda/internal/platform/auth/testhelper"
	"github.com/akarso/shopanda/internal/platform/id"
)

type dlGrantRepo struct {
	mu   sync.Mutex
	byID map[string]*domain.DownloadGrant
}

var _ domain.Repository = (*dlGrantRepo)(nil)

func (m *dlGrantRepo) Insert(_ context.Context, g *domain.DownloadGrant) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *g
	m.byID[g.ID] = &cp
	return nil
}
func (m *dlGrantRepo) Update(_ context.Context, g *domain.DownloadGrant) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *g
	if existing := m.byID[g.ID]; existing != nil {
		cp.DownloadCount = existing.DownloadCount
	}
	m.byID[g.ID] = &cp
	return nil
}
func (m *dlGrantRepo) UpdateCount(_ context.Context, id string, newCount, expectedCount int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	g := m.byID[id]
	if g == nil || g.DownloadCount != expectedCount {
		return domain.ErrCountConflict
	}
	g.DownloadCount = newCount
	return nil
}
func (m *dlGrantRepo) ClaimDownload(_ context.Context, id, customerID string) (*domain.DownloadGrant, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	g := m.byID[id]
	if g == nil || g.CustomerID != customerID || g.Expired(time.Now().UTC()) || g.Exhausted() {
		return nil, domain.ErrCountConflict
	}
	g.DownloadCount++
	cp := *g
	return &cp, nil
}
func (m *dlGrantRepo) FindByID(_ context.Context, id string) (*domain.DownloadGrant, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	g := m.byID[id]
	if g == nil {
		return nil, nil
	}
	cp := *g
	return &cp, nil
}
func (m *dlGrantRepo) FindByOrderItemAndAsset(context.Context, string, string) (*domain.DownloadGrant, error) {
	return nil, nil
}
func (m *dlGrantRepo) ListByCustomerID(context.Context, string) ([]domain.DownloadGrant, error) {
	return nil, nil
}
func (m *dlGrantRepo) ListByOrderItemID(context.Context, string) ([]domain.DownloadGrant, error) {
	return nil, nil
}
func (m *dlGrantRepo) CountByAssetID(context.Context, string) (int, error) { return 0, nil }
func (m *dlGrantRepo) DeleteExpiredOrRevokedByAssetID(context.Context, string) (int, error) {
	return 0, nil
}

type dlAssetRepo struct {
	asset *media.Asset
}

func (r *dlAssetRepo) Save(context.Context, *media.Asset) error { return nil }
func (r *dlAssetRepo) FindByID(_ context.Context, id string) (*media.Asset, error) {
	if r.asset == nil || r.asset.ID != id {
		return nil, nil
	}
	cp := *r.asset
	return &cp, nil
}
func (r *dlAssetRepo) List(context.Context, int, int) ([]media.Asset, error) { return nil, nil }
func (r *dlAssetRepo) Delete(context.Context, string) error                  { return nil }

type memReadableStorage struct {
	files map[string][]byte
	url   string
}

func (s *memReadableStorage) Name() string                 { return "mem" }
func (s *memReadableStorage) Save(string, io.Reader) error { return nil }
func (s *memReadableStorage) Delete(string) error          { return nil }
func (s *memReadableStorage) URL(path string) string {
	if s.url != "" {
		return s.url + path
	}
	return "https://public.example/" + path
}
func (s *memReadableStorage) Read(path string) ([]byte, error) {
	b, ok := s.files[path]
	if !ok {
		return nil, os.ErrNotExist
	}
	return append([]byte(nil), b...), nil
}

type memSignedStorage struct {
	memReadableStorage
	url      string
	err      error
	headErr  error
	lastOpts media.SignURLOptions
	heads    atomic.Int64
	signs    atomic.Int64
}

func (s *memSignedStorage) Name() string { return "s3" }
func (s *memSignedStorage) SignedURLHosts() []string {
	if u, err := url.Parse(s.url); err == nil && u.Hostname() != "" {
		return []string{u.Hostname()}
	}
	return nil
}
func (s *memSignedStorage) Head(_ context.Context, path string) error {
	s.heads.Add(1)
	if s.headErr != nil {
		return s.headErr
	}
	if _, ok := s.files[path]; !ok && len(s.files) > 0 {
		// When files map is populated, require key presence (Head-miss tests).
		if _, exists := s.files[path]; !exists {
			return os.ErrNotExist
		}
	}
	if s.files != nil {
		if _, ok := s.files[path]; !ok {
			return os.ErrNotExist
		}
	}
	return nil
}
func (s *memSignedStorage) SignedURL(_ context.Context, _ string, opts media.SignURLOptions) (string, error) {
	s.signs.Add(1)
	s.lastOpts = opts
	return s.url, s.err
}

func postDownload(h http.HandlerFunc, grantID, customerID string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/account/downloads/"+grantID, nil)
	req.SetPathValue("grantID", grantID)
	if customerID != "" {
		req = testhelper.CustomerRequest(req, customerID)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func TestDownloadAccount_Deliver_StreamsAndRecords(t *testing.T) {
	repo := &dlGrantRepo{byID: map[string]*domain.DownloadGrant{}}
	svc := downloadapp.NewService(repo, nil)
	expires := time.Now().UTC().Add(time.Hour)
	g, err := svc.Issue(context.Background(), domain.OrderItemID(id.New(), "v"), "cust-1", "asset-1", expires, 3)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	asset := &media.Asset{
		ID: "asset-1", Path: "files/a.pdf", Filename: "guide.pdf",
		MimeType: "application/pdf", Size: 4, CreatedAt: time.Now().UTC(),
	}
	storage := &memReadableStorage{files: map[string][]byte{"files/a.pdf": []byte("%PDF")}}
	h := storefront.NewDownloadAccountHandler(svc, &dlAssetRepo{asset: asset}, storage, nil)

	rr := postDownload(h.Deliver(), g.ID, "cust-1")

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	if rr.Header().Get("Content-Type") != "application/pdf" {
		t.Fatalf("content-type = %q", rr.Header().Get("Content-Type"))
	}
	if !strings.Contains(rr.Header().Get("Content-Disposition"), "guide.pdf") {
		t.Fatalf("disposition = %q", rr.Header().Get("Content-Disposition"))
	}
	if rr.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("Cache-Control = %q", rr.Header().Get("Cache-Control"))
	}
	if rr.Body.String() != "%PDF" {
		t.Fatalf("body = %q", rr.Body.String())
	}
	stored, _ := repo.FindByID(context.Background(), g.ID)
	if stored.DownloadCount != 1 {
		t.Fatalf("download_count = %d, want 1", stored.DownloadCount)
	}
	if strings.Contains(rr.Body.String(), "public.example") {
		t.Fatal("must not leak permanent public URL")
	}
}

func TestDownloadAccount_Deliver_TableStatusCodes(t *testing.T) {
	now := time.Now().UTC()
	expires := now.Add(time.Hour)

	tests := []struct {
		name       string
		setup      func(t *testing.T, repo *dlGrantRepo, svc *downloadapp.Service) (grantID, customerID string, assets *dlAssetRepo, storage media.Storage)
		wantStatus int
		wantCode   string
		checkCount func(t *testing.T, repo *dlGrantRepo, grantID string)
	}{
		{
			name: "401 empty identity",
			setup: func(t *testing.T, repo *dlGrantRepo, svc *downloadapp.Service) (string, string, *dlAssetRepo, media.Storage) {
				g, err := svc.Issue(context.Background(), domain.OrderItemID(id.New(), "v"), "cust-1", "asset-1", expires, 1)
				if err != nil {
					t.Fatalf("Issue: %v", err)
				}
				return g.ID, "", &dlAssetRepo{}, &memReadableStorage{files: map[string][]byte{}}
			},
			wantStatus: http.StatusUnauthorized,
			wantCode:   "unauthorized",
		},
		{
			name: "403 wrong owner",
			setup: func(t *testing.T, repo *dlGrantRepo, svc *downloadapp.Service) (string, string, *dlAssetRepo, media.Storage) {
				g, err := svc.Issue(context.Background(), domain.OrderItemID(id.New(), "v"), "cust-1", "asset-1", expires, 1)
				if err != nil {
					t.Fatalf("Issue: %v", err)
				}
				return g.ID, "other", &dlAssetRepo{}, &memReadableStorage{files: map[string][]byte{}}
			},
			wantStatus: http.StatusForbidden,
			wantCode:   "forbidden",
		},
		{
			name: "404 missing grant",
			setup: func(t *testing.T, repo *dlGrantRepo, svc *downloadapp.Service) (string, string, *dlAssetRepo, media.Storage) {
				return id.New(), "cust-1", &dlAssetRepo{}, &memReadableStorage{files: map[string][]byte{}}
			},
			wantStatus: http.StatusNotFound,
			wantCode:   "not_found",
		},
		{
			name: "422 expired",
			setup: func(t *testing.T, repo *dlGrantRepo, svc *downloadapp.Service) (string, string, *dlAssetRepo, media.Storage) {
				g, err := svc.Issue(context.Background(), domain.OrderItemID(id.New(), "v"), "cust-1", "asset-1", expires, 1)
				if err != nil {
					t.Fatalf("Issue: %v", err)
				}
				g.ExpiresAt = now.Add(-time.Second)
				_ = repo.Update(context.Background(), g)
				return g.ID, "cust-1", &dlAssetRepo{}, &memReadableStorage{files: map[string][]byte{}}
			},
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   "validation",
		},
		{
			name: "422 exhausted",
			setup: func(t *testing.T, repo *dlGrantRepo, svc *downloadapp.Service) (string, string, *dlAssetRepo, media.Storage) {
				g, err := svc.Issue(context.Background(), domain.OrderItemID(id.New(), "v"), "cust-1", "asset-1", expires, 1)
				if err != nil {
					t.Fatalf("Issue: %v", err)
				}
				if _, err := svc.ClaimDelivery(context.Background(), "cust-1", g.ID); err != nil {
					t.Fatalf("ClaimDelivery: %v", err)
				}
				return g.ID, "cust-1", &dlAssetRepo{asset: &media.Asset{ID: "asset-1", Path: "f", Filename: "f", CreatedAt: now}}, &memReadableStorage{files: map[string][]byte{"f": []byte("x")}}
			},
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   "validation",
		},
		{
			name: "404 missing blob local no increment",
			setup: func(t *testing.T, repo *dlGrantRepo, svc *downloadapp.Service) (string, string, *dlAssetRepo, media.Storage) {
				g, err := svc.Issue(context.Background(), domain.OrderItemID(id.New(), "v"), "cust-1", "asset-1", expires, 2)
				if err != nil {
					t.Fatalf("Issue: %v", err)
				}
				return g.ID, "cust-1", &dlAssetRepo{asset: &media.Asset{ID: "asset-1", Path: "missing.pdf", Filename: "m.pdf", CreatedAt: now}}, &memReadableStorage{files: map[string][]byte{}}
			},
			wantStatus: http.StatusNotFound,
			wantCode:   "not_found",
			checkCount: func(t *testing.T, repo *dlGrantRepo, grantID string) {
				stored, _ := repo.FindByID(context.Background(), grantID)
				if stored.DownloadCount != 0 {
					t.Fatalf("download_count = %d, want 0", stored.DownloadCount)
				}
			},
		},
		{
			name: "404 head miss no increment",
			setup: func(t *testing.T, repo *dlGrantRepo, svc *downloadapp.Service) (string, string, *dlAssetRepo, media.Storage) {
				g, err := svc.Issue(context.Background(), domain.OrderItemID(id.New(), "v"), "cust-1", "asset-1", expires, 2)
				if err != nil {
					t.Fatalf("Issue: %v", err)
				}
				storage := &memSignedStorage{
					memReadableStorage: memReadableStorage{files: map[string][]byte{}},
					url:                "https://signed.example/a.pdf",
				}
				return g.ID, "cust-1", &dlAssetRepo{asset: &media.Asset{ID: "asset-1", Path: "gone.pdf", Filename: "g.pdf", CreatedAt: now}}, storage
			},
			wantStatus: http.StatusNotFound,
			wantCode:   "not_found",
			checkCount: func(t *testing.T, repo *dlGrantRepo, grantID string) {
				stored, _ := repo.FindByID(context.Background(), grantID)
				if stored.DownloadCount != 0 {
					t.Fatalf("download_count = %d, want 0", stored.DownloadCount)
				}
			},
		},
		{
			name: "500 presign error no increment",
			setup: func(t *testing.T, repo *dlGrantRepo, svc *downloadapp.Service) (string, string, *dlAssetRepo, media.Storage) {
				g, err := svc.Issue(context.Background(), domain.OrderItemID(id.New(), "v"), "cust-1", "asset-1", expires, 2)
				if err != nil {
					t.Fatalf("Issue: %v", err)
				}
				storage := &memSignedStorage{
					memReadableStorage: memReadableStorage{files: map[string][]byte{"ok.pdf": []byte("x")}},
					url:                "",
					err:                errors.New("presign boom"),
				}
				return g.ID, "cust-1", &dlAssetRepo{asset: &media.Asset{ID: "asset-1", Path: "ok.pdf", Filename: "ok.pdf", CreatedAt: now}}, storage
			},
			wantStatus: http.StatusInternalServerError,
			wantCode:   "internal",
			checkCount: func(t *testing.T, repo *dlGrantRepo, grantID string) {
				stored, _ := repo.FindByID(context.Background(), grantID)
				if stored.DownloadCount != 0 {
					t.Fatalf("download_count = %d, want 0", stored.DownloadCount)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := &dlGrantRepo{byID: map[string]*domain.DownloadGrant{}}
			svc := downloadapp.NewService(repo, nil)
			grantID, customerID, assets, storage := tc.setup(t, repo, svc)
			h := storefront.NewDownloadAccountHandler(svc, assets, storage, nil)
			rr := postDownload(h.Deliver(), grantID, customerID)
			if rr.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", rr.Code, tc.wantStatus, rr.Body.String())
			}
			var env struct {
				Error *struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
				t.Fatalf("json: %v body=%s", err, rr.Body.String())
			}
			if env.Error == nil || env.Error.Code != tc.wantCode {
				t.Fatalf("error = %+v, want code %q", env.Error, tc.wantCode)
			}
			if tc.checkCount != nil {
				tc.checkCount(t, repo, grantID)
			}
		})
	}
}

func TestDownloadAccount_Deliver_SignedRedirect(t *testing.T) {
	repo := &dlGrantRepo{byID: map[string]*domain.DownloadGrant{}}
	svc := downloadapp.NewService(repo, nil)
	g, err := svc.Issue(context.Background(), domain.OrderItemID(id.New(), "v"), "cust-1", "asset-1", time.Now().UTC().Add(time.Hour), 2)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	asset := &media.Asset{
		ID: "asset-1", Path: "files/a.pdf", Filename: "guide.pdf",
		MimeType: "application/pdf", Size: 4, CreatedAt: time.Now().UTC(),
	}
	storage := &memSignedStorage{
		memReadableStorage: memReadableStorage{files: map[string][]byte{"files/a.pdf": []byte("%PDF")}},
		url:                "https://signed.example/a.pdf?X-Amz-Expires=60",
	}
	h := storefront.NewDownloadAccountHandler(svc, &dlAssetRepo{asset: asset}, storage, nil)
	rr := postDownload(h.Deliver(), g.ID, "cust-1")

	if rr.Code != http.StatusFound {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	if loc := rr.Header().Get("Location"); loc != storage.url {
		t.Fatalf("Location = %q", loc)
	}
	if storage.lastOpts.Filename != "guide.pdf" {
		t.Fatalf("Sign filename = %q", storage.lastOpts.Filename)
	}
	stored, _ := repo.FindByID(context.Background(), g.ID)
	if stored.DownloadCount != 1 {
		t.Fatalf("download_count = %d", stored.DownloadCount)
	}
}

func TestDownloadAccount_Deliver_RejectsInvalidSignedURL(t *testing.T) {
	repo := &dlGrantRepo{byID: map[string]*domain.DownloadGrant{}}
	svc := downloadapp.NewService(repo, nil)
	g, err := svc.Issue(context.Background(), domain.OrderItemID(id.New(), "v"), "cust-1", "asset-1", time.Now().UTC().Add(time.Hour), 2)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	asset := &media.Asset{ID: "asset-1", Path: "files/a.pdf", Filename: "g.pdf", CreatedAt: time.Now().UTC()}
	storage := &memSignedStorage{
		memReadableStorage: memReadableStorage{files: map[string][]byte{"files/a.pdf": []byte("x")}},
		url:                "javascript:alert(1)",
	}
	h := storefront.NewDownloadAccountHandler(svc, &dlAssetRepo{asset: asset}, storage, nil)
	rr := postDownload(h.Deliver(), g.ID, "cust-1")
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", rr.Code)
	}
	stored, _ := repo.FindByID(context.Background(), g.ID)
	if stored.DownloadCount != 0 {
		t.Fatalf("count = %d", stored.DownloadCount)
	}
}

func TestDownloadAccount_Deliver_ContentDispositionSanitized(t *testing.T) {
	repo := &dlGrantRepo{byID: map[string]*domain.DownloadGrant{}}
	svc := downloadapp.NewService(repo, nil)
	g, err := svc.Issue(context.Background(), domain.OrderItemID(id.New(), "v"), "cust-1", "asset-1", time.Now().UTC().Add(time.Hour), 1)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	asset := &media.Asset{
		ID: "asset-1", Path: "f.bin", Filename: "bad\"\r\nfile.bin",
		MimeType: "application/octet-stream", CreatedAt: time.Now().UTC(),
	}
	storage := &memReadableStorage{files: map[string][]byte{"f.bin": []byte("x")}}
	h := storefront.NewDownloadAccountHandler(svc, &dlAssetRepo{asset: asset}, storage, nil)
	rr := postDownload(h.Deliver(), g.ID, "cust-1")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	disp := rr.Header().Get("Content-Disposition")
	if strings.Contains(disp, "\r") || strings.Contains(disp, "\n") || strings.Contains(disp, `"`) && !strings.HasPrefix(disp, `attachment; filename="`) {
		t.Fatalf("disposition = %q", disp)
	}
	if disp != `attachment; filename="badfile.bin"` {
		t.Fatalf("disposition = %q", disp)
	}
}

func TestDownloadAccount_Deliver_ContentDispositionNonASCII(t *testing.T) {
	repo := &dlGrantRepo{byID: map[string]*domain.DownloadGrant{}}
	svc := downloadapp.NewService(repo, nil)
	g, err := svc.Issue(context.Background(), domain.OrderItemID(id.New(), "v"), "cust-1", "asset-1", time.Now().UTC().Add(time.Hour), 1)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	asset := &media.Asset{
		ID: "asset-1", Path: "f.pdf", Filename: "café.pdf",
		MimeType: "application/pdf", CreatedAt: time.Now().UTC(),
	}
	storage := &memReadableStorage{files: map[string][]byte{"f.pdf": []byte("%PDF")}}
	h := storefront.NewDownloadAccountHandler(svc, &dlAssetRepo{asset: asset}, storage, nil)
	rr := postDownload(h.Deliver(), g.ID, "cust-1")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	want := media.ContentDispositionAttachment("café.pdf")
	if !strings.Contains(want, "filename*=UTF-8''") {
		t.Fatalf("helper missing filename*: %q", want)
	}
	if got := rr.Header().Get("Content-Disposition"); got != want {
		t.Fatalf("disposition = %q, want %q", got, want)
	}
}

func TestDownloadAccount_Deliver_NeverUsesStorageURL(t *testing.T) {
	repo := &dlGrantRepo{byID: map[string]*domain.DownloadGrant{}}
	svc := downloadapp.NewService(repo, nil)
	g, err := svc.Issue(context.Background(), domain.OrderItemID(id.New(), "v"), "cust-1", "asset-1", time.Now().UTC().Add(time.Hour), 1)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	asset := &media.Asset{ID: "asset-1", Path: "f.bin", Filename: "f.bin", CreatedAt: time.Now().UTC()}
	storage := &memReadableStorage{
		files: map[string][]byte{"f.bin": []byte("data")},
		url:   "https://cdn.leaked.example/",
	}
	h := storefront.NewDownloadAccountHandler(svc, &dlAssetRepo{asset: asset}, storage, nil)
	rr := postDownload(h.Deliver(), g.ID, "cust-1")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	if strings.Contains(rr.Body.String(), "cdn.leaked") || strings.Contains(rr.Header().Get("Location"), "cdn.leaked") {
		t.Fatal("must not use Storage.URL()")
	}
}

func TestDownloadAccount_Deliver_ConcurrentUpToLimit(t *testing.T) {
	repo := &dlGrantRepo{byID: map[string]*domain.DownloadGrant{}}
	svc := downloadapp.NewService(repo, nil)
	limit := 5
	g, err := svc.Issue(context.Background(), domain.OrderItemID(id.New(), "v"), "cust-1", "asset-1", time.Now().UTC().Add(time.Hour), limit)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	asset := &media.Asset{ID: "asset-1", Path: "f.bin", Filename: "f.bin", CreatedAt: time.Now().UTC()}
	storage := &memReadableStorage{files: map[string][]byte{"f.bin": []byte("x")}}
	h := storefront.NewDownloadAccountHandler(svc, &dlAssetRepo{asset: asset}, storage, nil)

	var okCount atomic.Int64
	var wg sync.WaitGroup
	workers := 20
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			rr := postDownload(h.Deliver(), g.ID, "cust-1")
			if rr.Code == http.StatusOK {
				okCount.Add(1)
			}
		}()
	}
	wg.Wait()
	if got := okCount.Load(); got != int64(limit) {
		t.Fatalf("successful downloads = %d, want %d", got, limit)
	}
	stored, _ := repo.FindByID(context.Background(), g.ID)
	if stored.DownloadCount != limit {
		t.Fatalf("download_count = %d, want %d", stored.DownloadCount, limit)
	}
}

func TestContentDispositionAndSignedTTLHelpers(t *testing.T) {
	// Ensure package helpers stay reachable via successful delivery paths above.
	// Additional unit coverage for TTL cap: short grant remaining window.
	repo := &dlGrantRepo{byID: map[string]*domain.DownloadGrant{}}
	svc := downloadapp.NewService(repo, nil)
	expires := time.Now().UTC().Add(10 * time.Second)
	g, err := svc.Issue(context.Background(), domain.OrderItemID(id.New(), "v"), "cust-1", "asset-1", expires, 1)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	asset := &media.Asset{ID: "asset-1", Path: "f.pdf", Filename: "f.pdf", CreatedAt: time.Now().UTC()}
	storage := &memSignedStorage{
		memReadableStorage: memReadableStorage{files: map[string][]byte{"f.pdf": []byte("x")}},
		url:                "https://signed.example/f.pdf",
	}
	h := storefront.NewDownloadAccountHandler(svc, &dlAssetRepo{asset: asset}, storage, nil)
	rr := postDownload(h.Deliver(), g.ID, "cust-1")
	if rr.Code != http.StatusFound {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	if storage.lastOpts.TTL <= 0 || storage.lastOpts.TTL > 10*time.Second {
		t.Fatalf("TTL = %v, want capped to remaining grant expiry (<=10s)", storage.lastOpts.TTL)
	}
}

func TestDownloadAccount_Deliver_PresignDoesNotFallThroughToNotConfigured(t *testing.T) {
	repo := &dlGrantRepo{byID: map[string]*domain.DownloadGrant{}}
	svc := downloadapp.NewService(repo, nil)
	g, err := svc.Issue(context.Background(), domain.OrderItemID(id.New(), "v"), "cust-1", "asset-1", time.Now().UTC().Add(time.Hour), 1)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	asset := &media.Asset{ID: "asset-1", Path: "f.pdf", Filename: "f.pdf", CreatedAt: time.Now().UTC()}
	storage := &memSignedStorage{
		memReadableStorage: memReadableStorage{files: map[string][]byte{"f.pdf": []byte("x")}},
		err:                fmt.Errorf("sts assume role failed"),
	}
	h := storefront.NewDownloadAccountHandler(svc, &dlAssetRepo{asset: asset}, storage, nil)
	rr := postDownload(h.Deliver(), g.ID, "cust-1")
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "not configured") {
		t.Fatalf("must not fall through to not-configured message: %s", rr.Body.String())
	}
}
