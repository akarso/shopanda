package storefront_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	extensionapp "github.com/akarso/shopanda/internal/application/extension"
	domainext "github.com/akarso/shopanda/internal/domain/extension"
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/shared"
	storefront "github.com/akarso/shopanda/internal/interfaces/http/storefront"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/auth/testhelper"
)

type orderTestExtensionValueRepo struct {
	values map[string]domainext.Value
}

func orderTestExtKey(target domainext.Target, fieldCode string) string {
	return string(target.Type) + ":" + target.ID + ":" + fieldCode
}

func newOrderTestExtensionValueRepo() *orderTestExtensionValueRepo {
	return &orderTestExtensionValueRepo{values: make(map[string]domainext.Value)}
}

func (m *orderTestExtensionValueRepo) ListByTarget(_ context.Context, target domainext.Target) ([]domainext.Value, error) {
	out := make([]domainext.Value, 0)
	for _, value := range m.values {
		if value.TargetType == target.Type && value.TargetID == target.ID {
			out = append(out, value)
		}
	}
	return out, nil
}

func (m *orderTestExtensionValueRepo) ListByTargets(_ context.Context, targetType domainext.TargetType, targetIDs []string) ([]domainext.Value, error) {
	out := make([]domainext.Value, 0)
	for _, targetID := range targetIDs {
		stored, err := m.ListByTarget(context.Background(), domainext.Target{Type: targetType, ID: targetID})
		if err != nil {
			return nil, err
		}
		out = append(out, stored...)
	}
	return out, nil
}

func (m *orderTestExtensionValueRepo) Upsert(_ context.Context, value domainext.Value) error {
	m.values[orderTestExtKey(domainext.Target{Type: value.TargetType, ID: value.TargetID}, value.FieldCode)] = value
	return nil
}

func (m *orderTestExtensionValueRepo) UpsertBatch(_ context.Context, values []domainext.Value) error {
	for _, value := range values {
		if err := m.Upsert(context.Background(), value); err != nil {
			return err
		}
	}
	return nil
}

func (m *orderTestExtensionValueRepo) Delete(_ context.Context, target domainext.Target, fieldCode string) error {
	key := orderTestExtKey(target, fieldCode)
	if _, ok := m.values[key]; !ok {
		return apperror.NotFound("extension value not found")
	}
	delete(m.values, key)
	return nil
}

func TestOrderHandler_Get_IncludesLineExtensions(t *testing.T) {
	repo := newStubOrderRepo()

	o, err := order.NewOrder("ord-1", "cust-1", "ada@example.com", "EUR", []order.Item{
		mustOrderItem(t, "var-1", "SKU-1", "Widget", 1, 1000),
	})
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}
	repo.orders["ord-1"] = &o

	reg := extensionapp.NewRegistry()
	if err := reg.Register(domainext.FieldDef{
		Code:        "acme.gift.message",
		Label:       "Gift message",
		Type:        domainext.FieldTypeString,
		Scope:       domainext.TargetOrderItem,
		StorageMode: domainext.StorageStored,
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	extRepo := newOrderTestExtensionValueRepo()
	msg := "Hello"
	extRepo.values[orderTestExtKey(domainext.OrderItemTarget("ord-1", "var-1"), "acme.gift.message")] = domainext.Value{
		FieldCode:  "acme.gift.message",
		TargetType: domainext.TargetOrderItem,
		TargetID:   "ord-1:var-1",
		Payload:    domainext.ValuePayload{StringValue: &msg},
	}
	valueSvc := extensionapp.NewValueService(reg, extRepo)
	handler := storefront.NewOrderHandler(repo, valueSvc)
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/orders/{orderId}", storefront.RequireAuth()(handler.Get()))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/orders/ord-1", nil)
	req = testhelper.CustomerRequest(req, "cust-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	data := body["data"].(map[string]interface{})
	orderData := data["order"].(map[string]interface{})
	items := orderData["items"].([]interface{})
	item := items[0].(map[string]interface{})
	exts := item["extensions"].([]interface{})
	if len(exts) != 1 {
		t.Fatalf("extensions = %v", exts)
	}
	ext := exts[0].(map[string]interface{})
	if ext["field_code"] != "acme.gift.message" || ext["value"] != "Hello" {
		t.Fatalf("extension = %+v", ext)
	}
}

func mustOrderItem(t *testing.T, variantID, sku, name string, qty int, unit int64) order.Item {
	t.Helper()
	item, err := order.NewItem(variantID, sku, name, qty, shared.MustNewMoney(unit, "EUR"))
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	return item
}
