package http

import (
	"bytes"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/akarso/shopanda/internal/domain/cart"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/domain/store"
	"github.com/akarso/shopanda/internal/platform/apperror"
	platformAuth "github.com/akarso/shopanda/internal/platform/auth"
)

const (
	storefrontCartCookieName   = "shopanda_storefront_cart"
	storefrontCartCookieMaxAge = 60 * 60 * 24 * 30
)

var storefrontMiniCartTemplate = template.Must(template.New("storefront-mini-cart").Parse(`
<section class="mini-cart-panel" aria-label="Mini cart">
    <div class="mini-cart-panel__header">
        <strong>Cart</strong>
        <a href="{{ .CartURL }}">View cart</a>
    </div>
    {{ if .Items }}
    <ul class="mini-cart-panel__items">
        {{ range .Items }}
        <li>
            <div>
                <strong>{{ .ProductName }}</strong>
                {{ if .VariantName }}<span>{{ .VariantName }}</span>{{ end }}
            </div>
            <div>
                <span>{{ .Quantity }} x {{ .UnitPriceText }}</span>
            </div>
        </li>
        {{ end }}
    </ul>
    <div class="mini-cart-panel__footer">
        <span>{{ .Summary.TotalQuantity }} item(s)</span>
        <strong>{{ .Summary.SubtotalText }}</strong>
    </div>
    {{ else }}
    <p class="muted-note">{{ .EmptyMessage }}</p>
    {{ end }}
</section>`))

// StorefrontCartFormData provides the first available add-to-cart action from the PDP.
type StorefrontCartFormData struct {
	Action     string
	VariantID  string
	Quantity   int
	RedirectTo string
}

type StorefrontCartItem struct {
	ProductName   string
	ProductSlug   string
	VariantName   string
	VariantSKU    string
	VariantID     string
	Quantity      int
	UnitPriceText string
	LineTotalText string
}

type StorefrontCartSummary struct {
	ItemCount     int
	TotalQuantity int
	SubtotalText  string
}

type StorefrontCartPageData struct {
	Layout       StorefrontLayoutData
	Theme        interface{}
	Items        []StorefrontCartItem
	Summary      StorefrontCartSummary
	EmptyMessage string
}

type storefrontMiniCartData struct {
	CartURL      string
	Items        []StorefrontCartItem
	Summary      StorefrontCartSummary
	EmptyMessage string
}

func (h *StorefrontHandler) Cart() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !h.engine.HasTemplate("cart") {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}
		page, err := h.buildCartPageResponse(r)
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		h.renderPage(w, "cart", page)
	}
}

func (h *StorefrontHandler) CartCountFragment() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(h.cartLabelBestEffort(r, "Cart (0)")))
	}
}

func (h *StorefrontHandler) MiniCartFragment() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		page, err := h.buildCartPageResponse(r)
		if err != nil {
			h.log.Error("storefront.mini_cart.build_failed", err, map[string]interface{}{"path": r.URL.Path})
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		var buf bytes.Buffer
		if err := storefrontMiniCartTemplate.Execute(&buf, storefrontMiniCartData{
			CartURL:      page.Layout.CartURL,
			Items:        page.Items,
			Summary:      page.Summary,
			EmptyMessage: page.EmptyMessage,
		}); err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(buf.Bytes())
	}
}

func (h *StorefrontHandler) AddToCart() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form body", http.StatusBadRequest)
			return
		}
		variantID := strings.TrimSpace(r.FormValue("variant_id"))
		if variantID == "" {
			http.Error(w, "variant_id is required", http.StatusBadRequest)
			return
		}
		quantity := 1
		if raw := strings.TrimSpace(r.FormValue("quantity")); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed <= 0 {
				http.Error(w, "quantity must be a positive integer", http.StatusBadRequest)
				return
			}
			quantity = parsed
		}
		currentCart, err := h.ensureCartForRequest(w, r)
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		customerID := storefrontCustomerID(r)
		if _, err := h.carts.AddItem(r.Context(), currentCart.ID, customerID, variantID, quantity); err != nil {
			status := http.StatusInternalServerError
			if apperror.Is(err, apperror.CodeValidation) {
				status = http.StatusUnprocessableEntity
			}
			http.Error(w, err.Error(), status)
			return
		}
		h.writeCartMutationResponse(w, r, strings.TrimSpace(r.FormValue("redirect_to")), false, false)
	}
}

func (h *StorefrontHandler) UpdateCart() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form body", http.StatusBadRequest)
			return
		}
		currentCart, err := h.currentCart(r)
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		if currentCart == nil {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}
		variantID := strings.TrimSpace(r.FormValue("variant_id"))
		quantity, err := strconv.Atoi(strings.TrimSpace(r.FormValue("quantity")))
		if variantID == "" || err != nil || quantity <= 0 {
			http.Error(w, "invalid cart update", http.StatusBadRequest)
			return
		}
		if _, err := h.carts.UpdateItemQuantity(r.Context(), currentCart.ID, storefrontCustomerID(r), variantID, quantity); err != nil {
			status := http.StatusInternalServerError
			if apperror.Is(err, apperror.CodeValidation) {
				status = http.StatusUnprocessableEntity
			}
			if apperror.Is(err, apperror.CodeNotFound) {
				status = http.StatusNotFound
			}
			http.Error(w, err.Error(), status)
			return
		}
		h.writeCartMutationResponse(w, r, "/cart", true, true)
	}
}

func (h *StorefrontHandler) RemoveCartItem() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form body", http.StatusBadRequest)
			return
		}
		currentCart, err := h.currentCart(r)
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		if currentCart == nil {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}
		variantID := strings.TrimSpace(r.FormValue("variant_id"))
		if variantID == "" {
			http.Error(w, "variant_id is required", http.StatusBadRequest)
			return
		}
		if _, err := h.carts.RemoveItem(r.Context(), currentCart.ID, storefrontCustomerID(r), variantID); err != nil {
			status := http.StatusInternalServerError
			if apperror.Is(err, apperror.CodeValidation) {
				status = http.StatusUnprocessableEntity
			}
			if apperror.Is(err, apperror.CodeNotFound) {
				status = http.StatusNotFound
			}
			http.Error(w, err.Error(), status)
			return
		}
		h.writeCartMutationResponse(w, r, "/cart", true, true)
	}
}

func (h *StorefrontHandler) writeCartMutationResponse(w http.ResponseWriter, r *http.Request, fallbackRedirect string, renderCartPage bool, emitCartUpdated bool) {
	if emitCartUpdated {
		w.Header().Set("HX-Trigger", "cart-updated")
	}
	if storefrontIsHTMX(r) && renderCartPage {
		page, err := h.buildCartPageResponse(r)
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		h.renderPage(w, "cart", page)
		return
	}
	if storefrontIsHTMX(r) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	redirectTo := strings.TrimSpace(r.FormValue("redirect_to"))
	if redirectTo == "" {
		redirectTo = fallbackRedirect
	}
	redirectTo = storefrontSafeRedirectPath(redirectTo, fallbackRedirect)
	http.Redirect(w, r, redirectTo, http.StatusSeeOther)
}

func (h *StorefrontHandler) buildCartPageResponse(r *http.Request) (StorefrontCartPageData, error) {
	layout := h.layoutDataBestEffort(r)
	currentCart, err := h.currentCart(r)
	if err != nil {
		return StorefrontCartPageData{}, err
	}
	return h.buildCartPageData(r, layout, currentCart)
}

func (h *StorefrontHandler) buildCartPageData(r *http.Request, layout StorefrontLayoutData, currentCart *cart.Cart) (StorefrontCartPageData, error) {
	page := StorefrontCartPageData{
		Layout:       layout,
		Theme:        h.engine.Theme(),
		EmptyMessage: "Your cart is empty.",
	}
	if currentCart == nil || len(currentCart.Items) == 0 {
		page.Summary.SubtotalText = formatStorefrontMoney(0, storefrontCurrency(r))
		return page, nil
	}
	items := make([]StorefrontCartItem, 0, len(currentCart.Items))
	subtotal := shared.MustZero(currentCart.Currency)
	for _, item := range currentCart.Items {
		lineTotal, err := item.UnitPrice.MulChecked(int64(item.Quantity))
		if err != nil {
			return StorefrontCartPageData{}, err
		}
		subtotal, err = subtotal.AddChecked(lineTotal)
		if err != nil {
			return StorefrontCartPageData{}, err
		}
		cartItem, err := h.storefrontCartItem(r, item, lineTotal)
		if err != nil {
			return StorefrontCartPageData{}, err
		}
		items = append(items, cartItem)
	}
	page.Items = items
	page.Summary = StorefrontCartSummary{
		ItemCount:     len(currentCart.Items),
		TotalQuantity: currentCart.TotalQuantity(),
		SubtotalText:  formatStorefrontMoney(subtotal.Amount(), subtotal.Currency()),
	}
	return page, nil
}

func (h *StorefrontHandler) storefrontCartItem(r *http.Request, item cart.Item, lineTotal shared.Money) (StorefrontCartItem, error) {
	view := StorefrontCartItem{
		ProductName:   item.VariantID,
		VariantID:     item.VariantID,
		Quantity:      item.Quantity,
		UnitPriceText: formatStorefrontMoney(item.UnitPrice.Amount(), item.UnitPrice.Currency()),
		LineTotalText: formatStorefrontMoney(lineTotal.Amount(), lineTotal.Currency()),
	}
	if h.variants == nil {
		return view, nil
	}
	variant, err := h.variants.FindByID(r.Context(), item.VariantID)
	if err != nil {
		return StorefrontCartItem{}, err
	}
	if variant == nil {
		return view, nil
	}
	view.VariantName = variant.Name
	view.VariantSKU = variant.SKU
	if h.repo == nil {
		return view, nil
	}
	product, err := h.repo.FindByID(r.Context(), variant.ProductID)
	if err != nil {
		return StorefrontCartItem{}, err
	}
	if product == nil {
		return view, nil
	}
	view.ProductName = product.Name
	view.ProductSlug = product.Slug
	return view, nil
}

func (h *StorefrontHandler) resolveCartForm(r *http.Request, productID string) *StorefrontCartFormData {
	if h.variants == nil || h.carts == nil || productID == "" {
		return nil
	}
	variants, err := h.variants.ListByProductID(r.Context(), productID, 0, 1)
	if err != nil {
		h.log.Warn("storefront.cart.variants_load_failed", map[string]interface{}{
			"path":       r.URL.Path,
			"product_id": productID,
			"error":      err.Error(),
		})
		return nil
	}
	if len(variants) == 0 {
		return nil
	}
	return &StorefrontCartFormData{
		Action:     "/cart/add",
		VariantID:  variants[0].ID,
		Quantity:   1,
		RedirectTo: r.URL.Path,
	}
}

func (h *StorefrontHandler) cartLabelBestEffort(r *http.Request, fallback string) string {
	if h.carts == nil {
		return fallback
	}
	currentCart, err := h.currentCart(r)
	if err != nil {
		h.log.Warn("storefront.cart.load_failed", map[string]interface{}{
			"path":  r.URL.Path,
			"error": err.Error(),
		})
		return fallback
	}
	if currentCart == nil {
		return "Cart (0)"
	}
	return fmt.Sprintf("Cart (%d)", currentCart.TotalQuantity())
}

func (h *StorefrontHandler) currentCart(r *http.Request) (*cart.Cart, error) {
	if h.carts == nil {
		return nil, nil
	}
	customerID := storefrontCustomerID(r)
	if customerID != "" {
		currentCart, err := h.carts.GetActiveCartByCustomer(r.Context(), customerID)
		if apperror.Is(err, apperror.CodeNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return currentCart, nil
	}
	cookie, err := r.Cookie(storefrontCartCookieName)
	if err == http.ErrNoCookie {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	currentCart, err := h.carts.GetCart(r.Context(), cookie.Value)
	if apperror.Is(err, apperror.CodeNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if currentCart != nil && currentCart.CustomerID != "" {
		return nil, nil
	}
	return currentCart, nil
}

func (h *StorefrontHandler) ensureCartForRequest(w http.ResponseWriter, r *http.Request) (*cart.Cart, error) {
	if h.carts == nil {
		return nil, fmt.Errorf("storefront cart service is not configured")
	}
	if existing, err := h.currentCart(r); err != nil {
		return nil, err
	} else if existing != nil {
		return existing, nil
	}
	created, err := h.carts.CreateCart(r.Context(), storefrontCustomerID(r), storefrontCurrency(r))
	if err != nil {
		return nil, err
	}
	if storefrontCustomerID(r) == "" {
		storefrontSetCartCookie(w, r, created.ID)
	}
	return created, nil
}

func storefrontCurrency(r *http.Request) string {
	if s := store.FromContext(r.Context()); s != nil && s.Currency != "" {
		return s.Currency
	}
	return "EUR"
}

func storefrontCustomerID(r *http.Request) string {
	identity := platformAuth.IdentityFrom(r.Context())
	if identity.IsGuest() {
		return ""
	}
	return identity.UserID
}

func storefrontSetCartCookie(w http.ResponseWriter, r *http.Request, cartID string) {
	http.SetCookie(w, &http.Cookie{
		Name:     storefrontCartCookieName,
		Value:    cartID,
		Path:     "/",
		MaxAge:   storefrontCartCookieMaxAge,
		HttpOnly: true,
		Secure:   r != nil && r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	})
}

func storefrontIsHTMX(r *http.Request) bool {
	return strings.EqualFold(strings.TrimSpace(r.Header.Get("HX-Request")), "true")
}

func storefrontSafeRedirectPath(raw, fallback string) string {
	raw = strings.TrimSpace(raw)
	fallback = strings.TrimSpace(fallback)
	if storefrontIsSafeLocalRedirect(raw) {
		return raw
	}
	if storefrontIsSafeLocalRedirect(fallback) {
		return fallback
	}
	return "/cart"
}

func storefrontIsSafeLocalRedirect(raw string) bool {
	if raw == "" || !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return u.Scheme == "" && u.Host == ""
}
