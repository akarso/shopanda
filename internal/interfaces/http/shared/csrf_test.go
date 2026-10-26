package shared_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/interfaces/http/shared"
)

func TestCSRFMiddleware_AcceptsHeaderOnStorefrontPath(t *testing.T) {
	next := shared.CSRFMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	mint := httptest.NewRecorder()
	mintReq := httptest.NewRequest(http.MethodGet, "/storefront/csrf", nil)
	next.ServeHTTP(mint, mintReq)
	var csrf *http.Cookie
	for _, c := range mint.Result().Cookies() {
		if c.Name == "shopanda_csrf" {
			csrf = c
			break
		}
	}
	if csrf == nil {
		t.Fatal("expected csrf cookie")
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/storefront/cart/items", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(shared.CSRFHeaderName, csrf.Value)
	req.AddCookie(csrf)
	next.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
}

func TestCSRFMiddleware_StillAcceptsFormToken(t *testing.T) {
	next := shared.CSRFMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	mint := httptest.NewRecorder()
	mintReq := httptest.NewRequest(http.MethodGet, "/account/login", nil)
	next.ServeHTTP(mint, mintReq)
	var csrf *http.Cookie
	for _, c := range mint.Result().Cookies() {
		if c.Name == "shopanda_csrf" {
			csrf = c
			break
		}
	}
	if csrf == nil {
		t.Fatal("expected csrf cookie")
	}

	form := url.Values{"csrf_token": {csrf.Value}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/account/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(csrf)
	next.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
}

func mintCSRFCookie(t *testing.T, path string) *http.Cookie {
	t.Helper()
	next := shared.CSRFMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	mint := httptest.NewRecorder()
	next.ServeHTTP(mint, httptest.NewRequest(http.MethodGet, path, nil))
	for _, c := range mint.Result().Cookies() {
		if c.Name == "shopanda_csrf" {
			return c
		}
	}
	t.Fatal("expected csrf cookie")
	return nil
}

func assertStorefrontCSRFForbiddenJSON(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("Content-Type = %q, want JSON", rec.Header().Get("Content-Type"))
	}
	var env struct {
		Error *struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v body=%s", err, rec.Body.String())
	}
	if env.Error == nil || env.Error.Code != "forbidden" {
		t.Fatalf("error = %+v", env.Error)
	}
}

func TestCSRFMiddleware_StorefrontMissingHeaderJSON(t *testing.T) {
	next := shared.CSRFMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler must not run")
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/storefront/cart/items", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	next.ServeHTTP(rec, req)
	assertStorefrontCSRFForbiddenJSON(t, rec)
}

func TestCSRFMiddleware_StorefrontMismatchedHeaderJSON(t *testing.T) {
	next := shared.CSRFMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler must not run")
	}))
	csrf := mintCSRFCookie(t, "/storefront/csrf")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/storefront/cart/items/var-1", nil)
	req.Header.Set(shared.CSRFHeaderName, "deadbeef")
	req.AddCookie(csrf)
	next.ServeHTTP(rec, req)
	assertStorefrontCSRFForbiddenJSON(t, rec)
}

func TestCSRFMiddleware_StorefrontRejectsQueryToken(t *testing.T) {
	next := shared.CSRFMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler must not run")
	}))
	csrf := mintCSRFCookie(t, "/storefront/csrf")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/storefront/cart/items?csrf_token="+csrf.Value, strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(csrf)
	next.ServeHTTP(rec, req)
	assertStorefrontCSRFForbiddenJSON(t, rec)
}

func TestCSRFMiddleware_StorefrontMintFailureJSON(t *testing.T) {
	next := shared.CSRFMiddlewareWithGenerator(func() (string, error) {
		return "", errors.New("rand failed")
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler must not run")
	}))
	for _, tc := range []struct {
		method, path string
	}{
		{http.MethodGet, "/storefront/csrf"},
		{http.MethodPost, "/storefront/cart/items"},
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		next.ServeHTTP(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("%s %s status = %d, want 500 body=%s", tc.method, tc.path, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Header().Get("Content-Type"), "application/json") {
			t.Fatalf("%s %s Content-Type = %q, want JSON", tc.method, tc.path, rec.Header().Get("Content-Type"))
		}
		var env struct {
			Error *struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&env); err != nil {
			t.Fatalf("%s %s decode: %v body=%s", tc.method, tc.path, err, rec.Body.String())
		}
		if env.Error == nil || env.Error.Code != "internal" {
			t.Fatalf("%s %s error = %+v", tc.method, tc.path, env.Error)
		}
	}
}

func TestCSRFMiddleware_IgnoresCartHTMLPath(t *testing.T) {
	called := false
	next := shared.CSRFMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodPost, "/cart/add", strings.NewReader("variant_id=v1"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	next.ServeHTTP(rec, req)
	if !called || rec.Code != http.StatusOK {
		t.Fatalf("HTML /cart/add must stay outside CSRF (called=%v code=%d)", called, rec.Code)
	}
}
