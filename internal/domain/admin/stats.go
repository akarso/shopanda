package admin

import "context"

// DashboardStats holds the overview numbers for the admin dashboard.
type DashboardStats struct {
	OrdersToday   int
	RevenueToday  int64
	Currency      string
	TotalProducts int
	LowStockCount int
	RecentOrders  []RecentOrder
}

// RecentOrder is a lightweight read-model for the dashboard feed.
type RecentOrder struct {
	ID          string
	CustomerID  string
	TotalAmount int64
	Currency    string
	Status      string
	CreatedAt   string // RFC 3339
}

// StatsRepository provides read-only aggregations for the admin dashboard.
type StatsRepository interface {
	// GetDashboardStats returns the overview statistics.
	// lowStockThreshold defines the quantity below which a variant is
	// considered low-stock. recentLimit caps the number of recent orders.
	GetDashboardStats(ctx context.Context, lowStockThreshold, recentLimit int) (DashboardStats, error)
}
