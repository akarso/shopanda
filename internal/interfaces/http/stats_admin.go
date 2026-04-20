package http

import (
	"net/http"

	"github.com/akarso/shopanda/internal/domain/admin"
)

// StatsAdminHandler serves the admin dashboard stats endpoint.
type StatsAdminHandler struct {
	stats admin.StatsRepository
}

// NewStatsAdminHandler creates a StatsAdminHandler.
func NewStatsAdminHandler(stats admin.StatsRepository) *StatsAdminHandler {
	if stats == nil {
		panic("http: stats repository must not be nil")
	}
	return &StatsAdminHandler{stats: stats}
}

// Overview handles GET /api/v1/admin/stats/overview.
func (h *StatsAdminHandler) Overview() http.HandlerFunc {
	const lowStockThreshold = 10
	const recentLimit = 10

	return func(w http.ResponseWriter, r *http.Request) {
		stats, err := h.stats.GetDashboardStats(r.Context(), lowStockThreshold, recentLimit)
		if err != nil {
			JSONError(w, err)
			return
		}

		recent := make([]recentOrderResp, 0, len(stats.RecentOrders))
		for _, o := range stats.RecentOrders {
			recent = append(recent, recentOrderResp{
				ID:          o.ID,
				CustomerID:  o.CustomerID,
				TotalAmount: o.TotalAmount,
				Currency:    o.Currency,
				Status:      o.Status,
				CreatedAt:   o.CreatedAt,
			})
		}

		JSON(w, http.StatusOK, statsOverviewResp{
			OrdersToday:   stats.OrdersToday,
			RevenueToday:  revenueResp{Amount: stats.RevenueToday, Currency: stats.Currency},
			TotalProducts: stats.TotalProducts,
			LowStockCount: stats.LowStockCount,
			RecentOrders:  recent,
		})
	}
}

type statsOverviewResp struct {
	OrdersToday   int               `json:"orders_today"`
	RevenueToday  revenueResp       `json:"revenue_today"`
	TotalProducts int               `json:"total_products"`
	LowStockCount int               `json:"low_stock_count"`
	RecentOrders  []recentOrderResp `json:"recent_orders"`
}

type revenueResp struct {
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
}

type recentOrderResp struct {
	ID          string `json:"id"`
	CustomerID  string `json:"customer_id"`
	TotalAmount int64  `json:"total_amount"`
	Currency    string `json:"currency"`
	Status      string `json:"status"`
	CreatedAt   string `json:"created_at"`
}
