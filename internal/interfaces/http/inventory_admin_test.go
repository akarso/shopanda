package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/akarso/shopanda/internal/testutil"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/akarso/shopanda/internal/application/admin"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/inventory"
	"github.com/akarso/shopanda/internal/domain/rbac"
	"github.com/akarso/shopanda/internal/platform/auth/testhelper"

	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
)

type mockStockRepo struct {
	listInventoryFn    func(ctx context.Context, offset, limit int, search string) ([]inventory.InventoryListItem, error)
	getInventoryItemFn func(ctx context.Context, variantID string) (inventory.InventoryListItem, error)
	getStockFn         func(ctx context.Context, variantID string) (inventory.StockEntry, error)
	setStockFn         func(ctx context.Context, entry *inventory.StockEntry) error
	setStockCalls      int
}

func (m *mockStockRepo) GetStock(ctx context.Context, variantID string) (inventory.StockEntry, error) {
	if m.getStockFn != nil {
		return m.getStockFn(ctx, variantID)
	}
	return inventory.StockEntry{VariantID: variantID}, nil
}

func (m *mockStockRepo) SetStock(ctx context.Context, entry *inventory.StockEntry) error {
	m.setStockCalls++
	if m.setStockFn != nil {
		return m.setStockFn(ctx, entry)
	}
	return nil
}

func (m *mockStockRepo) ListStock(context.Context, int, int) ([]inventory.StockEntry, error) {
	return nil, nil
}

func (m *mockStockRepo) ListInventory(ctx context.Context, offset, limit int, search string) ([]inventory.InventoryListItem, error) {
	if m.listInventoryFn != nil {
		return m.listInventoryFn(ctx, offset, limit, search)
	}
	return nil, nil
}

func (m *mockStockRepo) GetInventoryItem(ctx context.Context, variantID string) (inventory.InventoryListItem, error) {
	if m.getInventoryItemFn != nil {
		return m.getInventoryItemFn(ctx, variantID)
	}
	return inventory.InventoryListItem{VariantID: variantID}, nil
}

type mockVariantRepoForInventory struct {
	findByIDFn func(ctx context.Context, id string) (*catalog.Variant, error)
}

func (m *mockVariantRepoForInventory) Create(context.Context, *catalog.Variant) error { return nil }
func (m *mockVariantRepoForInventory) Update(context.Context, *catalog.Variant) error { return nil }
func (m *mockVariantRepoForInventory) FindByID(ctx context.Context, id string) (*catalog.Variant, error) {
	if m.findByIDFn != nil {
		return m.findByIDFn(ctx, id)
	}
	return nil, nil
}
func (m *mockVariantRepoForInventory) FindBySKU(context.Context, string) (*catalog.Variant, error) {
	return nil, nil
}
func (m *mockVariantRepoForInventory) ListByProductID(ctx context.Context, productID string, offset, limit int) ([]catalog.Variant, error) {
	return nil, nil
}

func (m *mockVariantRepoForInventory) ListByProductIDs(ctx context.Context, productIDs []string, limitPerProduct int) (map[string][]catalog.Variant, error) {
	return testutil.ListByProductIDsFromList(ctx, m.ListByProductID, productIDs, limitPerProduct)
}

func newInventoryAdminRouter(h *shophttp.InventoryAdminHandler) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/admin/inventory", h.List())
	mux.HandleFunc("PUT /api/v1/admin/inventory/{variantId}", h.Adjust())
	return mux
}

func TestInventoryAdminHandler_List(t *testing.T) {
	stock := &mockStockRepo{
		listInventoryFn: func(_ context.Context, offset, limit int, search string) ([]inventory.InventoryListItem, error) {
			if offset != 0 || limit != 20 {
				t.Fatalf("offset=%d limit=%d", offset, limit)
			}
			if search != "sku-1" {
				t.Fatalf("search = %q, want sku-1", search)
			}
			return []inventory.InventoryListItem{{
				VariantID: "v1", ProductID: "p1", SKU: "SKU-1", ProductName: "Shirt",
				VariantName: "Red", Quantity: 5, Reserved: 2,
			}}, nil
		},
	}
	h := shophttp.NewInventoryAdminHandler(stock, &mockVariantRepoForInventory{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/inventory?offset=0&limit=20&search=sku-1", nil)
	newInventoryAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var resp struct {
		Data struct {
			Items []struct {
				SKU       string `json:"sku"`
				Quantity  int    `json:"quantity"`
				Reserved  int    `json:"reserved"`
				Available int    `json:"available"`
				LowStock  bool   `json:"low_stock"`
			} `json:"items"`
			LowStockThreshold int `json:"low_stock_threshold"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Data.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(resp.Data.Items))
	}
	item := resp.Data.Items[0]
	if item.SKU != "SKU-1" || item.Quantity != 5 || item.Reserved != 2 || item.Available != 3 {
		t.Fatalf("item = %+v", item)
	}
	if !item.LowStock {
		t.Fatal("expected low_stock badge")
	}
	if resp.Data.LowStockThreshold != admin.LowStockThreshold {
		t.Fatalf("threshold = %d", resp.Data.LowStockThreshold)
	}
}

func TestInventoryAdminHandler_Adjust(t *testing.T) {
	var saved *inventory.StockEntry
	stock := &mockStockRepo{
		getStockFn: func(_ context.Context, variantID string) (inventory.StockEntry, error) {
			return inventory.StockEntry{VariantID: variantID, Quantity: 3}, nil
		},
		setStockFn: func(_ context.Context, entry *inventory.StockEntry) error {
			saved = entry
			return nil
		},
		getInventoryItemFn: func(_ context.Context, variantID string) (inventory.InventoryListItem, error) {
			return inventory.InventoryListItem{
				VariantID: variantID, ProductID: "p1", SKU: "SKU-1", ProductName: "Shirt",
				VariantName: "Red", Quantity: 12, Reserved: 1,
			}, nil
		},
	}
	variants := &mockVariantRepoForInventory{
		findByIDFn: func(_ context.Context, id string) (*catalog.Variant, error) {
			return &catalog.Variant{ID: id, ProductID: "p1", SKU: "SKU-1", Name: "Red"}, nil
		},
	}
	h := shophttp.NewInventoryAdminHandler(stock, variants)

	body, _ := json.Marshal(map[string]interface{}{"quantity": 12})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/inventory/v1", bytes.NewReader(body))
	newInventoryAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if saved == nil || saved.Quantity != 12 {
		t.Fatalf("saved = %+v, want quantity 12", saved)
	}
	var adjustResp struct {
		Data struct {
			Item struct {
				ProductName string `json:"product_name"`
				Reserved    int    `json:"reserved"`
				Available   int    `json:"available"`
			} `json:"item"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &adjustResp); err != nil {
		t.Fatalf("decode adjust response: %v", err)
	}
	if adjustResp.Data.Item.ProductName != "Shirt" || adjustResp.Data.Item.Reserved != 1 || adjustResp.Data.Item.Available != 11 {
		t.Fatalf("item = %+v", adjustResp.Data.Item)
	}
}

func TestInventoryAdminHandler_AdjustNegativeRejected(t *testing.T) {
	stock := &mockStockRepo{
		getStockFn: func(_ context.Context, variantID string) (inventory.StockEntry, error) {
			return inventory.StockEntry{VariantID: variantID, Quantity: 3}, nil
		},
	}
	variants := &mockVariantRepoForInventory{
		findByIDFn: func(_ context.Context, id string) (*catalog.Variant, error) {
			return &catalog.Variant{ID: id, ProductID: "p1", SKU: "SKU-1"}, nil
		},
	}
	h := shophttp.NewInventoryAdminHandler(stock, variants)

	body, _ := json.Marshal(map[string]interface{}{"quantity": -1})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/inventory/v1", bytes.NewReader(body))
	newInventoryAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
	if stock.setStockCalls != 0 {
		t.Fatalf("SetStock calls = %d, want 0", stock.setStockCalls)
	}
}

func TestInventoryAdminHandler_AdjustUnknownFieldRejected(t *testing.T) {
	stock := &mockStockRepo{}
	h := shophttp.NewInventoryAdminHandler(stock, &mockVariantRepoForInventory{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/inventory/v1", bytes.NewReader([]byte(`{"quantitty":5}`)))
	newInventoryAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
	if stock.setStockCalls != 0 {
		t.Fatalf("SetStock calls = %d, want 0", stock.setStockCalls)
	}
}

func TestInventoryAdminHandler_AdjustMissingQuantityRejected(t *testing.T) {
	stock := &mockStockRepo{}
	h := shophttp.NewInventoryAdminHandler(stock, &mockVariantRepoForInventory{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/inventory/v1", bytes.NewReader([]byte(`{}`)))
	newInventoryAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
	if stock.setStockCalls != 0 {
		t.Fatalf("SetStock calls = %d, want 0", stock.setStockCalls)
	}
}

func TestInventoryAdminHandler_ForbiddenWithoutProductsPermission(t *testing.T) {
	h := shophttp.NewInventoryAdminHandler(&mockStockRepo{}, &mockVariantRepoForInventory{})
	requireProductsRead := shophttp.RequirePermission(rbac.ProductsRead)
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/admin/inventory", requireProductsRead(h.List()))

	rec := httptest.NewRecorder()
	req := testhelper.CustomerRequest(httptest.NewRequest("GET", "/api/v1/admin/inventory", nil), "cust-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestInventoryAdminHandler_AdjustForbiddenWithoutProductsWrite(t *testing.T) {
	h := shophttp.NewInventoryAdminHandler(&mockStockRepo{}, &mockVariantRepoForInventory{})
	requireProductsWrite := shophttp.RequirePermission(rbac.ProductsWrite)
	mux := http.NewServeMux()
	mux.Handle("PUT /api/v1/admin/inventory/{variantId}", requireProductsWrite(h.Adjust()))

	body, _ := json.Marshal(map[string]interface{}{"quantity": 5})
	rec := httptest.NewRecorder()
	req := testhelper.CustomerRequest(httptest.NewRequest("PUT", "/api/v1/admin/inventory/v1", bytes.NewReader(body)), "cust-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}
