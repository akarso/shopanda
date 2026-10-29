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
func (s *stubDLFileRepo) CountByAssetID(_ context.Context, assetID string) (int, error) {
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
func (s *stubDLFileRepo) Upsert(_ context.Context, f *catalog.DownloadableFile) error {
	if s.rows == nil {
		s.rows = map[string][]catalog.DownloadableFile{}
	}
	rows := s.rows[f.ProductID]
	for i := range rows {
		if rows[i].AssetID == f.AssetID {
			rows[i] = *f
			s.rows[f.ProductID] = rows
			return nil
		}
	}
	s.rows[f.ProductID] = append(rows, *f)
	if s.counts == nil {
		s.counts = map[string]int{}
	}
	s.counts[f.ProductID] = len(s.rows[f.ProductID])
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
func (s *stubDLAssetRepo) WithTx(*sql.Tx) media.AssetRepository                  { return s }
func (s *stubDLAssetRepo) LockForUpdate(_ context.Context, id string) (*media.Asset, error) {
	if s.byID == nil || s.byID[id] == nil {
		return nil, apperror.NotFound("asset not found")
	}
	return s.byID[id], nil
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
		"a1": {ID: "a1", Path: media.PrivateDownloadsPrefix + "a1/file.pdf"},
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
	assets := &stubDLAssetRepo{byID: map[string]*media.Asset{
		"a1": {ID: "a1", Path: media.PrivateDownloadsPrefix + "a1/guide.pdf"},
	}}
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

func TestDownloadableFileService_Attach_RejectsPublicUploadPath(t *testing.T) {
	ctx := catalog.WithOperatorProductReadScope(context.Background())
	products := &stubDLProductRepo{byID: map[string]*catalog.Product{
		"p1": {ID: "p1", Type: catalog.TypeDownloadable, Status: catalog.StatusDraft},
	}, typ: map[string]catalog.Type{"p1": catalog.TypeDownloadable}}
	assets := &stubDLAssetRepo{byID: map[string]*media.Asset{
		"a1": {ID: "a1", Path: "uploads/a1/public.pdf"},
	}}
	svc := newDLService(products, &stubDLFileRepo{}, assets)
	f, err := catalog.NewDownloadableFile("p1", "a1")
	if err != nil {
		t.Fatalf("NewDownloadableFile: %v", err)
	}
	err = svc.Attach(ctx, f)
	if err == nil || !apperror.Is(err, apperror.CodeValidation) || !strings.Contains(err.Error(), "private/downloads/") {
		t.Fatalf("Attach = %v, want private/downloads/ validation", err)
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
	assets := &stubDLAssetRepo{byID: map[string]*media.Asset{
		"a1": {ID: "a1", Path: media.PrivateDownloadsPrefix + "a1/file.pdf"},
	}}
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

func TestDownloadableFileService_Replace_PreservesMetaAndRetargets(t *testing.T) {
	ctx := catalog.WithOperatorProductReadScope(context.Background())
	products := &stubDLProductRepo{byID: map[string]*catalog.Product{
		"p1": {ID: "p1", Type: catalog.TypeDownloadable, Status: catalog.StatusDraft},
	}, typ: map[string]catalog.Type{"p1": catalog.TypeDownloadable}}
	files := &stubDLFileRepo{
		rows: map[string][]catalog.DownloadableFile{
			"p1": {{ProductID: "p1", AssetID: "old", Position: 3, Label: "Guide"}},
		},
		counts: map[string]int{"p1": 1},
	}
	assets := &stubDLAssetRepo{byID: map[string]*media.Asset{
		"old": {ID: "old", Path: media.PrivateDownloadsPrefix + "old/a.pdf"},
		"new": {ID: "new", Path: media.PrivateDownloadsPrefix + "new/b.pdf"},
	}}
	var retargeted bool
	svc := newDLService(products, files, assets).WithGrantRetarget(
		func(_ context.Context, _ *sql.Tx, oldAssetID, newAssetID string) (catalogapp.GrantRetargetOutcome, error) {
			if oldAssetID != "old" || newAssetID != "new" {
				t.Fatalf("retarget args = %s %s", oldAssetID, newAssetID)
			}
			retargeted = true
			return catalogapp.GrantRetargetOutcome{Moved: 2}, nil
		},
	)
	f, err := catalog.NewDownloadableFile("p1", "new")
	if err != nil {
		t.Fatalf("NewDownloadableFile: %v", err)
	}
	result, err := svc.Replace(ctx, "p1", "old", f, catalogapp.ReplaceMeta{})
	if err != nil {
		t.Fatalf("Replace: %v", err)
	}
	if !retargeted {
		t.Fatal("expected grant retarget in Replace TX")
	}
	if result.GrantsMoved != 2 || result.GrantsRevoked != 0 {
		t.Fatalf("result = %+v, want moved=2 revoked=0", result)
	}
	rows, err := files.ListByProductID(ctx, "p1")
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
	if rows[0].AssetID != "new" || rows[0].Position != 3 || rows[0].Label != "Guide" {
		t.Fatalf("preserved meta = %+v", rows[0])
	}
}

func TestDownloadableFileService_Replace_FailsClosedWithoutRetarget(t *testing.T) {
	ctx := catalog.WithOperatorProductReadScope(context.Background())
	products := &stubDLProductRepo{byID: map[string]*catalog.Product{
		"p1": {ID: "p1", Type: catalog.TypeDownloadable, Status: catalog.StatusDraft},
	}, typ: map[string]catalog.Type{"p1": catalog.TypeDownloadable}}
	files := &stubDLFileRepo{
		rows: map[string][]catalog.DownloadableFile{
			"p1": {{ProductID: "p1", AssetID: "old", Position: 1, Label: "A"}},
		},
		counts: map[string]int{"p1": 1},
	}
	assets := &stubDLAssetRepo{byID: map[string]*media.Asset{
		"old": {ID: "old", Path: media.PrivateDownloadsPrefix + "old/a.pdf"},
		"new": {ID: "new", Path: media.PrivateDownloadsPrefix + "new/b.pdf"},
	}}
	svc := newDLService(products, files, assets) // no WithGrantRetarget
	f, err := catalog.NewDownloadableFile("p1", "new")
	if err != nil {
		t.Fatalf("NewDownloadableFile: %v", err)
	}
	_, err = svc.Replace(ctx, "p1", "old", f, catalogapp.ReplaceMeta{})
	if err == nil || !apperror.Is(err, apperror.CodeInternal) {
		t.Fatalf("Replace without retarget = %v, want internal", err)
	}
}

func TestDownloadableFileService_Replace_SharedAssetRequiresGlobalRetarget(t *testing.T) {
	ctx := catalog.WithOperatorProductReadScope(context.Background())
	products := &stubDLProductRepo{byID: map[string]*catalog.Product{
		"p1": {ID: "p1", Type: catalog.TypeDownloadable, Status: catalog.StatusDraft},
		"p2": {ID: "p2", Type: catalog.TypeDownloadable, Status: catalog.StatusDraft},
	}, typ: map[string]catalog.Type{
		"p1": catalog.TypeDownloadable,
		"p2": catalog.TypeDownloadable,
	}}
	files := &stubDLFileRepo{
		rows: map[string][]catalog.DownloadableFile{
			"p1": {{ProductID: "p1", AssetID: "old", Position: 1, Label: "A"}},
			"p2": {{ProductID: "p2", AssetID: "old", Position: 0, Label: "Shared"}},
		},
	}
	assets := &stubDLAssetRepo{byID: map[string]*media.Asset{
		"old": {ID: "old", Path: media.PrivateDownloadsPrefix + "old/a.pdf"},
		"new": {ID: "new", Path: media.PrivateDownloadsPrefix + "new/b.pdf"},
	}}
	var retargeted bool
	svc := newDLService(products, files, assets).WithGrantRetarget(
		func(context.Context, *sql.Tx, string, string) (catalogapp.GrantRetargetOutcome, error) {
			retargeted = true
			return catalogapp.GrantRetargetOutcome{Moved: 1}, nil
		},
	)
	f, err := catalog.NewDownloadableFile("p1", "new")
	if err != nil {
		t.Fatalf("NewDownloadableFile: %v", err)
	}
	_, err = svc.Replace(ctx, "p1", "old", f, catalogapp.ReplaceMeta{})
	if err == nil || !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("shared without global_retarget = %v, want validation", err)
	}
	if retargeted {
		t.Fatal("must not retarget when shared asset refused")
	}
	if len(files.rows["p1"]) != 1 || files.rows["p1"][0].AssetID != "old" {
		t.Fatalf("join must be unchanged after refuse: %+v", files.rows["p1"])
	}

	result, err := svc.Replace(ctx, "p1", "old", f, catalogapp.ReplaceMeta{GlobalRetarget: true})
	if err != nil {
		t.Fatalf("Replace with global_retarget: %v", err)
	}
	if !retargeted {
		t.Fatal("expected retarget when global_retarget=true")
	}
	if result.GrantsMoved != 1 {
		t.Fatalf("result = %+v", result)
	}
	if len(files.rows["p2"]) != 1 || files.rows["p2"][0].AssetID != "old" {
		t.Fatalf("other product join unchanged: %+v", files.rows["p2"])
	}
}

func TestDownloadableFileService_Replace_FailsClosedWhenRevokeWithoutNotifier(t *testing.T) {
	ctx := catalog.WithOperatorProductReadScope(context.Background())
	products := &stubDLProductRepo{byID: map[string]*catalog.Product{
		"p1": {ID: "p1", Type: catalog.TypeDownloadable, Status: catalog.StatusDraft},
	}, typ: map[string]catalog.Type{"p1": catalog.TypeDownloadable}}
	files := &stubDLFileRepo{
		rows: map[string][]catalog.DownloadableFile{
			"p1": {{ProductID: "p1", AssetID: "old", Position: 1, Label: "A"}},
		},
	}
	assets := &stubDLAssetRepo{byID: map[string]*media.Asset{
		"old": {ID: "old", Path: media.PrivateDownloadsPrefix + "old/a.pdf"},
		"new": {ID: "new", Path: media.PrivateDownloadsPrefix + "new/b.pdf"},
	}}
	svc := newDLService(products, files, assets).WithGrantRetarget(
		func(context.Context, *sql.Tx, string, string) (catalogapp.GrantRetargetOutcome, error) {
			return catalogapp.GrantRetargetOutcome{
				Revoked: []catalogapp.GrantConflictRevoked{{ID: "g1", OrderItemID: "oi", AssetID: "old"}},
			}, nil
		},
	)
	f, err := catalog.NewDownloadableFile("p1", "new")
	if err != nil {
		t.Fatalf("NewDownloadableFile: %v", err)
	}
	_, err = svc.Replace(ctx, "p1", "old", f, catalogapp.ReplaceMeta{})
	if err == nil || !apperror.Is(err, apperror.CodeInternal) {
		t.Fatalf("Replace revoke without notifier = %v, want internal", err)
	}
}

func TestDownloadableFileService_Replace_ConflictRevokeNotifierAndCounts(t *testing.T) {
	ctx := catalog.WithOperatorProductReadScope(context.Background())
	products := &stubDLProductRepo{byID: map[string]*catalog.Product{
		"p1": {ID: "p1", Type: catalog.TypeDownloadable, Status: catalog.StatusDraft},
	}, typ: map[string]catalog.Type{"p1": catalog.TypeDownloadable}}
	files := &stubDLFileRepo{
		rows: map[string][]catalog.DownloadableFile{
			"p1": {{ProductID: "p1", AssetID: "old", Position: 1, Label: "A"}},
		},
	}
	assets := &stubDLAssetRepo{byID: map[string]*media.Asset{
		"old": {ID: "old", Path: media.PrivateDownloadsPrefix + "old/a.pdf"},
		"new": {ID: "new", Path: media.PrivateDownloadsPrefix + "new/b.pdf"},
	}}
	var notified []catalogapp.GrantConflictRevoked
	svc := newDLService(products, files, assets).
		WithGrantRetarget(func(_ context.Context, _ *sql.Tx, oldAssetID, newAssetID string) (catalogapp.GrantRetargetOutcome, error) {
			if oldAssetID != "old" || newAssetID != "new" {
				t.Fatalf("retarget ids = %s %s", oldAssetID, newAssetID)
			}
			return catalogapp.GrantRetargetOutcome{
				Moved: 3,
				Revoked: []catalogapp.GrantConflictRevoked{
					{ID: "g1", OrderItemID: "oi1", CustomerID: "c1", AssetID: "old"},
				},
			}, nil
		}).
		WithGrantConflictRevokeNotifier(func(_ context.Context, revoked []catalogapp.GrantConflictRevoked) {
			notified = append([]catalogapp.GrantConflictRevoked{}, revoked...)
		})
	f, err := catalog.NewDownloadableFile("p1", "new")
	if err != nil {
		t.Fatalf("NewDownloadableFile: %v", err)
	}
	result, err := svc.Replace(ctx, "p1", "old", f, catalogapp.ReplaceMeta{})
	if err != nil {
		t.Fatalf("Replace: %v", err)
	}
	if result.GrantsMoved != 3 || result.GrantsRevoked != 1 {
		t.Fatalf("result = %+v", result)
	}
	if len(notified) != 1 || notified[0].ID != "g1" {
		t.Fatalf("notified = %+v", notified)
	}
}

func TestDownloadableFileService_Replace_ExplicitMeta(t *testing.T) {
	ctx := catalog.WithOperatorProductReadScope(context.Background())
	products := &stubDLProductRepo{byID: map[string]*catalog.Product{
		"p1": {ID: "p1", Type: catalog.TypeDownloadable, Status: catalog.StatusDraft},
	}, typ: map[string]catalog.Type{"p1": catalog.TypeDownloadable}}
	files := &stubDLFileRepo{
		rows: map[string][]catalog.DownloadableFile{
			"p1": {{ProductID: "p1", AssetID: "old", Position: 3, Label: "Guide"}},
		},
		counts: map[string]int{"p1": 1},
	}
	assets := &stubDLAssetRepo{byID: map[string]*media.Asset{
		"old": {ID: "old", Path: media.PrivateDownloadsPrefix + "old/a.pdf"},
		"new": {ID: "new", Path: media.PrivateDownloadsPrefix + "new/b.pdf"},
	}}
	svc := newDLService(products, files, assets).WithGrantRetarget(
		func(context.Context, *sql.Tx, string, string) (catalogapp.GrantRetargetOutcome, error) {
			return catalogapp.GrantRetargetOutcome{}, nil
		},
	)
	f, err := catalog.NewDownloadableFile("p1", "new")
	if err != nil {
		t.Fatalf("NewDownloadableFile: %v", err)
	}
	pos := 7
	label := "Updated"
	if _, err := svc.Replace(ctx, "p1", "old", f, catalogapp.ReplaceMeta{Position: &pos, Label: &label}); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	rows, _ := files.ListByProductID(ctx, "p1")
	if len(rows) != 1 || rows[0].Position != 7 || rows[0].Label != "Updated" {
		t.Fatalf("explicit meta = %+v", rows)
	}
}
