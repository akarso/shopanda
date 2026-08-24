package admin

import (
	"net/http"

	httpshared "github.com/akarso/shopanda/internal/interfaces/http/shared"

	adminapp "github.com/akarso/shopanda/internal/application/admin"
	domainadmin "github.com/akarso/shopanda/internal/domain/admin"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// StatsAdminHandler serves the admin dashboard stats endpoint.
type StatsAdminHandler struct {
	stats   domainadmin.StatsRepository
	auditor *adminapp.Auditor
}

// NewStatsAdminHandler creates a StatsAdminHandler.
func NewStatsAdminHandler(stats domainadmin.StatsRepository) *StatsAdminHandler {
	if stats == nil {
		panic("http: stats repository must not be nil")
	}
	return NewStatsAdminHandlerWithAuditor(stats, adminapp.NewAuditor(logger.New("info")))
}

// NewStatsAdminHandlerWithAuditor creates a StatsAdminHandler with a custom auditor.
func NewStatsAdminHandlerWithAuditor(stats domainadmin.StatsRepository, auditor *adminapp.Auditor) *StatsAdminHandler {
	if stats == nil {
		panic("http: stats repository must not be nil")
	}
	if auditor == nil {
		panic("http: auditor must not be nil")
	}
	return &StatsAdminHandler{stats: stats, auditor: auditor}
}

// Overview handles GET /api/v1/admin/stats/overview.
func (h *StatsAdminHandler) Overview() http.HandlerFunc {
	const lowStockThreshold = adminapp.LowStockThreshold
	const recentLimit = 10

	return func(w http.ResponseWriter, r *http.Request) {
		adminID := adminIDFromRequest(r)
		details := fullAdminScopeDetailsFromRequest(r)
		details["low_stock_threshold"] = lowStockThreshold
		details["recent_limit"] = recentLimit

		stats, err := h.stats.GetDashboardStats(r.Context(), lowStockThreshold, recentLimit)
		if err != nil {
			h.auditor.LogAction(r.Context(), adminapp.AuditEntry{
				AdminID:      adminID,
				Action:       adminapp.AuditStatsRead,
				ResourceType: "stats_overview",
				Result:       "error",
				Error:        err.Error(),
				Details:      details,
			})
			httpshared.JSONError(w, err)
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

		h.auditor.LogAction(r.Context(), adminapp.AuditEntry{
			AdminID:      adminID,
			Action:       adminapp.AuditStatsRead,
			ResourceType: "stats_overview",
			Result:       "success",
			Details:      details,
		})

		httpshared.JSON(w, http.StatusOK, statsOverviewResp{
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
