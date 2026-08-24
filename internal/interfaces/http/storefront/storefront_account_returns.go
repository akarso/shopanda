package storefront

import (
	"net/http"
	"strconv"
	"strings"

	httpshared "github.com/akarso/shopanda/internal/interfaces/http/shared"

	returnsApp "github.com/akarso/shopanda/internal/application/returns"
	"github.com/akarso/shopanda/internal/domain/order"
	domainReturns "github.com/akarso/shopanda/internal/domain/returns"
	"github.com/akarso/shopanda/internal/domain/theme"
)

type StorefrontAccountReturnRow struct {
	ID        string
	OrderID   string
	Status    string
	Reason    string
	DateText  string
	URL       string
	CanCancel bool
}

type StorefrontAccountReturnsPageData struct {
	Layout       StorefrontLayoutData
	Theme        theme.Theme
	AccountNav   StorefrontAccountNavData
	CSRFToken    string
	Returns      []StorefrontAccountReturnRow
	EmptyMessage string
}

func (h *StorefrontHandler) AccountReturns() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.returns == nil || !h.engine.HasTemplate("account_returns") {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}
		customerID, ok := h.requireStorefrontAccount(w, r)
		if !ok {
			return
		}
		list, err := h.returns.ListByCustomerID(r.Context(), customerID)
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		rows := make([]StorefrontAccountReturnRow, 0, len(list))
		for i := range list {
			rows = append(rows, storefrontAccountReturnRow(&list[i]))
		}
		h.renderPage(w, "account_returns", StorefrontAccountReturnsPageData{
			Layout:       h.layoutDataBestEffort(r),
			Theme:        h.engine.Theme(),
			AccountNav:   storefrontAccountNav("returns"),
			CSRFToken:    httpshared.CSRFToken(r),
			Returns:      rows,
			EmptyMessage: "You have not requested any returns yet.",
		})
	}
}

func (h *StorefrontHandler) AccountOrderReturnRequest() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.returns == nil || h.orders == nil {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}
		customerID, ok := h.requireStorefrontAccount(w, r)
		if !ok {
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form body", http.StatusBadRequest)
			return
		}
		orderID := strings.TrimSpace(r.PathValue("orderId"))
		if orderID == "" {
			http.NotFound(w, r)
			return
		}

		reason := strings.TrimSpace(r.FormValue("reason"))
		lines, err := storefrontParseReturnLines(r, orderID, customerID, h)
		if err != nil {
			h.renderOrderDetailWithMessage(w, r, orderID, customerID, storefrontAccountErrorMessage(err), "")
			return
		}

		if _, err := h.returns.RequestReturn(r.Context(), orderID, customerID, reason, lines); err != nil {
			h.renderOrderDetailWithMessage(w, r, orderID, customerID, storefrontAccountErrorMessage(err), "")
			return
		}
		http.Redirect(w, r, "/account/orders/"+orderID+"?return_requested=1", http.StatusSeeOther)
	}
}

func (h *StorefrontHandler) AccountReturnCancel() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.returns == nil {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}
		customerID, ok := h.requireStorefrontAccount(w, r)
		if !ok {
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form body", http.StatusBadRequest)
			return
		}
		returnID := strings.TrimSpace(r.PathValue("returnId"))
		ret, err := h.returns.Get(r.Context(), returnID)
		if err != nil || ret == nil || (ret.CustomerID != "" && ret.CustomerID != customerID) {
			http.NotFound(w, r)
			return
		}
		if _, err := h.returns.Cancel(r.Context(), returnID); err != nil {
			http.Redirect(w, r, "/account/returns?error=1", http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/account/returns?cancelled=1", http.StatusSeeOther)
	}
}

func storefrontParseReturnLines(r *http.Request, orderID, customerID string, h *StorefrontHandler) ([]returnsApp.RequestLine, error) {
	returnable, err := h.returns.ReturnableLines(r.Context(), orderID, customerID)
	if err != nil {
		return nil, err
	}
	allowed := make(map[string]int, len(returnable))
	for _, line := range returnable {
		allowed[line.VariantID] = line.Returnable
	}

	var lines []returnsApp.RequestLine
	for key, values := range r.Form {
		if !strings.HasPrefix(key, "qty_") {
			continue
		}
		variantID := strings.TrimPrefix(key, "qty_")
		if variantID == "" || len(values) == 0 {
			continue
		}
		qty, err := strconv.Atoi(strings.TrimSpace(values[0]))
		if err != nil || qty <= 0 {
			continue
		}
		maxQty, ok := allowed[variantID]
		if !ok || qty > maxQty {
			continue
		}
		lines = append(lines, returnsApp.RequestLine{VariantID: variantID, Quantity: qty})
	}
	return lines, nil
}

func (h *StorefrontHandler) renderOrderDetailWithMessage(w http.ResponseWriter, r *http.Request, orderID, customerID, errMsg, successMsg string) {
	o, err := h.orders.FindByID(r.Context(), orderID)
	if err != nil || o == nil || o.CustomerID != customerID {
		http.NotFound(w, r)
		return
	}
	page, err := h.buildAccountOrderDetailPage(r, o, customerID)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	page.ErrorMessage = errMsg
	page.SuccessMessage = successMsg
	h.renderPageStatus(w, "account_order_detail", page, http.StatusUnprocessableEntity)
}

func (h *StorefrontHandler) buildAccountOrderDetailPage(r *http.Request, o *order.Order, customerID string) (StorefrontAccountOrderDetailPageData, error) {
	items := make([]StorefrontAccountOrderItem, 0, len(o.Items()))
	for _, item := range o.Items() {
		lineTotal, err := item.LineTotal()
		if err != nil {
			return StorefrontAccountOrderDetailPageData{}, err
		}
		items = append(items, StorefrontAccountOrderItem{
			Name:          item.Name,
			SKU:           item.SKU,
			Quantity:      item.Quantity,
			UnitPriceText: formatStorefrontMoney(item.UnitPrice.Amount(), item.UnitPrice.Currency()),
			LineTotalText: formatStorefrontMoney(lineTotal.Amount(), lineTotal.Currency()),
		})
	}

	page := StorefrontAccountOrderDetailPageData{
		Layout:     h.layoutDataBestEffort(r),
		Theme:      h.engine.Theme(),
		AccountNav: storefrontAccountNav("orders"),
		CSRFToken:  httpshared.CSRFToken(r),
		OrderID:    o.ID,
		DateText:   o.CreatedAt.UTC().Format("2006-01-02"),
		Status:     storefrontAccountOrderStatus(o.Status()),
		TotalText:  formatStorefrontMoney(o.TotalAmount.Amount(), o.TotalAmount.Currency()),
		Items:      items,
		BackURL:    "/account/orders",
		ReturnsURL: "/account/returns",
	}

	if h.returns != nil {
		if list, err := h.returns.ListByOrderForCustomer(r.Context(), o.ID, customerID); err == nil {
			page.Returns = make([]StorefrontAccountOrderReturnRow, 0, len(list))
			for i := range list {
				page.Returns = append(page.Returns, StorefrontAccountOrderReturnRow{
					ID:       list[i].ID,
					Status:   storefrontReturnStatus(list[i].Status()),
					Reason:   list[i].Reason,
					DateText: list[i].CreatedAt.UTC().Format("2006-01-02"),
					URL:      "/account/returns",
				})
			}
		}
		if lines, err := h.returns.ReturnableLines(r.Context(), o.ID, customerID); err == nil && len(lines) > 0 {
			page.CanRequestReturn = true
			page.ReturnableLines = make([]StorefrontAccountReturnableLine, 0, len(lines))
			for _, line := range lines {
				page.ReturnableLines = append(page.ReturnableLines, StorefrontAccountReturnableLine{
					VariantID:  line.VariantID,
					SKU:        line.SKU,
					Name:       line.Name,
					Returnable: line.Returnable,
				})
			}
		}
	}
	return page, nil
}

func storefrontAccountReturnRow(ret *domainReturns.Return) StorefrontAccountReturnRow {
	return StorefrontAccountReturnRow{
		ID:        ret.ID,
		OrderID:   ret.OrderID,
		Status:    storefrontReturnStatus(ret.Status()),
		Reason:    ret.Reason,
		DateText:  ret.CreatedAt.UTC().Format("2006-01-02"),
		URL:       "/account/orders/" + ret.OrderID,
		CanCancel: ret.Status() == domainReturns.StatusRequested,
	}
}

func storefrontReturnStatus(status domainReturns.Status) string {
	s := string(status)
	if s == "" {
		return ""
	}
	return strings.ToUpper(string(s[0])) + s[1:]
}
