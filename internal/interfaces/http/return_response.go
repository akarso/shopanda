package http

import (
	"time"

	domainReturns "github.com/akarso/shopanda/internal/domain/returns"
)

type returnItemResp struct {
	VariantID string `json:"variant_id"`
	SKU       string `json:"sku"`
	Name      string `json:"name"`
	Quantity  int    `json:"quantity"`
	UnitPrice int64  `json:"unit_price"`
	Currency  string `json:"currency"`
}

type returnResp struct {
	ID          string           `json:"id"`
	OrderID     string           `json:"order_id"`
	CustomerID  string           `json:"customer_id"`
	Reason      string           `json:"reason"`
	Status      string           `json:"status"`
	Currency    string           `json:"currency"`
	Items       []returnItemResp `json:"items"`
	TotalAmount int64            `json:"total_amount"`
	RestockedAt string           `json:"restocked_at,omitempty"`
	RefundedAt  string           `json:"refunded_at,omitempty"`
	CreatedAt   string           `json:"created_at"`
	UpdatedAt   string           `json:"updated_at"`
}

func ToReturnResponse(ret *domainReturns.Return) returnResp {
	items := make([]returnItemResp, 0, len(ret.Items()))
	var total int64
	for _, item := range ret.Items() {
		line, err := item.LineTotal()
		if err == nil {
			total += line.Amount()
		}
		items = append(items, returnItemResp{
			VariantID: item.VariantID,
			SKU:       item.SKU,
			Name:      item.Name,
			Quantity:  item.Quantity,
			UnitPrice: item.UnitPrice.Amount(),
			Currency:  item.UnitPrice.Currency(),
		})
	}
	resp := returnResp{
		ID:          ret.ID,
		OrderID:     ret.OrderID,
		CustomerID:  ret.CustomerID,
		Reason:      ret.Reason,
		Status:      string(ret.Status()),
		Currency:    ret.Currency,
		Items:       items,
		TotalAmount: total,
		CreatedAt:   ret.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:   ret.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if ret.RestockedAt != nil {
		resp.RestockedAt = ret.RestockedAt.UTC().Format(time.RFC3339)
	}
	if ret.RefundedAt != nil {
		resp.RefundedAt = ret.RefundedAt.UTC().Format(time.RFC3339)
	}
	return resp
}

func ToReturnResponses(list []domainReturns.Return) []returnResp {
	out := make([]returnResp, 0, len(list))
	for i := range list {
		out = append(out, ToReturnResponse(&list[i]))
	}
	return out
}
