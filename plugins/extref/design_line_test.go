package extref_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/platform/auth"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/pluginsdk"
	"github.com/akarso/shopanda/pkg/storefrontsession"
	"github.com/akarso/shopanda/plugins/extref"
)

func TestDesignLine_IdempotentAndConflict(t *testing.T) {
	h, _ := bootExtrefCartHost(t)
	handler, err := extref.DesignLineHandlerForTest(h)
	if err != nil {
		t.Fatal(err)
	}

	body := []byte(`{"variant_id":"var-1","design_id":"d-1","quantity":1}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/extref/design-line", bytes.NewReader(body))
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("first add status = %d body=%s", rec.Code, rec.Body.String())
	}
	var first map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	if first["status"] != "added" {
		t.Fatalf("status = %q, want added", first["status"])
	}
	if bytes.Contains(rec.Body.Bytes(), []byte(`"cart_id"`)) {
		t.Fatal("response must not expose cart_id")
	}
	var cookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == storefrontsession.CartCookieName {
			cookie = c
			break
		}
	}
	if cookie == nil || cookie.Value == "" {
		t.Fatal("expected storefront cart cookie after EnsureStorefrontCart")
	}

	// Same design → unchanged
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/extref/design-line", bytes.NewReader(body))
	req2.AddCookie(cookie)
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("idempotent status = %d body=%s", rec2.Code, rec2.Body.String())
	}
	var second map[string]string
	_ = json.Unmarshal(rec2.Body.Bytes(), &second)
	if second["status"] != "unchanged" {
		t.Fatalf("status = %q, want unchanged", second["status"])
	}

	// Other design → 409
	rec3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodPost, "/extref/design-line", bytes.NewReader([]byte(
		`{"variant_id":"var-1","design_id":"d-2","quantity":1}`,
	)))
	req3.AddCookie(cookie)
	handler.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusConflict {
		t.Fatalf("conflict status = %d body=%s", rec3.Code, rec3.Body.String())
	}
}

func TestDesignLine_AttachWithoutQtyBump(t *testing.T) {
	h, cartID := bootExtrefCartHost(t)
	port, ok := h.Cart()
	if !ok {
		t.Fatal("Cart()")
	}
	// Storefront-style line without design_id.
	if err := port.AddItem(context.Background(), cartID, "cust-1", "var-1", 2, pluginsdk.CartAddItemOpts{}); err != nil {
		t.Fatal(err)
	}

	handler, err := extref.DesignLineHandlerForTest(h)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/extref/design-line", bytes.NewReader([]byte(
		`{"variant_id":"var-1","design_id":"d-attach","quantity":5}`,
	)))
	// Signed-in customer cart — EnsureActive reuses cartID via customer.
	req = req.WithContext(auth.WithIdentity(req.Context(), mustID(t, "cust-1", identity.RoleCustomer)))
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["status"] != "attached" {
		t.Fatalf("status = %q, want attached", body["status"])
	}
	view, err := port.Get(context.Background(), cartID, "cust-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Items) != 1 || view.Items[0].Quantity != 2 {
		t.Fatalf("qty bumped: %+v", view.Items)
	}
}

func TestDesignLine_FailClosedWithoutCart(t *testing.T) {
	app := &plugin.App{}
	h := pluginsdk.AsHostFor(app, extref.PluginName)
	handler, err := extref.DesignLineHandlerForTest(h)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/extref/design-line", bytes.NewReader([]byte(
		`{"variant_id":"var-1","design_id":"d-1"}`,
	)))
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 when Cart unbound", rec.Code)
	}
}
