package http

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"reflect"

	assetsapp "github.com/akarso/shopanda/internal/application/assets"
)

// StorefrontInjectedAsset is a resolved plugin asset for layout templates.
type StorefrontInjectedAsset struct {
	URL string
}

// StorefrontAssets groups injected assets for a single page render.
type StorefrontAssets struct {
	HeadCSS  []StorefrontInjectedAsset
	HeadJS   []StorefrontInjectedAsset
	FooterJS []StorefrontInjectedAsset
}

// WithAssets enables plugin asset injection on storefront pages.
func (h *StorefrontHandler) WithAssets(registry *assetsapp.Registry) *StorefrontHandler {
	h.assets = registry
	return h
}

// WithCSPEnabled enables CSP nonces on injected scripts and sets a script-src header.
func (h *StorefrontHandler) WithCSPEnabled(enabled bool) *StorefrontHandler {
	h.cspEnabled = enabled
	return h
}

func (h *StorefrontHandler) resolveStorefrontAssets(r *http.Request) StorefrontAssets {
	if h.assets == nil {
		return StorefrontAssets{}
	}
	bundle := h.assets.ForRoute(r.URL.Path)
	return StorefrontAssets{
		HeadCSS:  toStorefrontAssets(bundle.HeadCSS),
		HeadJS:   toStorefrontAssets(bundle.HeadJS),
		FooterJS: toStorefrontAssets(bundle.FooterJS),
	}
}

func toStorefrontAssets(in []assetsapp.Resolved) []StorefrontInjectedAsset {
	if len(in) == 0 {
		return nil
	}
	out := make([]StorefrontInjectedAsset, len(in))
	for i, item := range in {
		out[i] = StorefrontInjectedAsset{URL: item.URL}
	}
	return out
}

func (h *StorefrontHandler) newCSPNonce() string {
	if !h.cspEnabled {
		return ""
	}
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return ""
	}
	return base64.RawStdEncoding.EncodeToString(buf)
}

func storefrontLayoutFromData(data interface{}) (StorefrontLayoutData, bool) {
	if data == nil {
		return StorefrontLayoutData{}, false
	}
	v := reflect.ValueOf(data)
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return StorefrontLayoutData{}, false
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return StorefrontLayoutData{}, false
	}
	f := v.FieldByName("Layout")
	if !f.IsValid() || !f.CanInterface() {
		return StorefrontLayoutData{}, false
	}
	layout, ok := f.Interface().(StorefrontLayoutData)
	return layout, ok
}

func storefrontCSPHeader(nonce string) string {
	return fmt.Sprintf("script-src 'self' 'nonce-%s'; object-src 'none'; base-uri 'self'", nonce)
}
