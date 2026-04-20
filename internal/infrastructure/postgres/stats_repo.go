package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/akarso/shopanda/internal/domain/admin"
)

// Compile-time check.
var _ admin.StatsRepository = (*StatsRepo)(nil)

// StatsRepo implements admin.StatsRepository using PostgreSQL.
type StatsRepo struct {
	db *sql.DB
}

// NewStatsRepo returns a new StatsRepo backed by db.
func NewStatsRepo(db *sql.DB) (*StatsRepo, error) {
	if db == nil {
		return nil, fmt.Errorf("NewStatsRepo: nil *sql.DB")
	}
	return &StatsRepo{db: db}, nil
}

// GetDashboardStats returns the dashboard overview aggregations.
func (r *StatsRepo) GetDashboardStats(ctx context.Context, lowStockThreshold, recentLimit int) (admin.DashboardStats, error) {
	if recentLimit <= 0 {
		recentLimit = 10
	}
	if recentLimit > 50 {
		recentLimit = 50
	}

	var stats admin.DashboardStats

	// 1. Orders today + revenue today.
	todayStart := time.Now().UTC().Truncate(24 * time.Hour)
	const orderQ = `SELECT COALESCE(COUNT(*), 0), COALESCE(SUM(total_amount), 0), COALESCE(MIN(total_currency), '')
		FROM orders WHERE created_at >= $1`
	if err := r.db.QueryRowContext(ctx, orderQ, todayStart).Scan(
		&stats.OrdersToday, &stats.RevenueToday, &stats.Currency,
	); err != nil {
		return admin.DashboardStats{}, fmt.Errorf("stats_repo: orders today: %w", err)
	}

	// 2. Total products.
	const prodQ = `SELECT COALESCE(COUNT(*), 0) FROM products`
	if err := r.db.QueryRowContext(ctx, prodQ).Scan(&stats.TotalProducts); err != nil {
		return admin.DashboardStats{}, fmt.Errorf("stats_repo: total products: %w", err)
	}

	// 3. Low stock count.
	const stockQ = `SELECT COALESCE(COUNT(*), 0) FROM stock WHERE quantity > 0 AND quantity < $1`
	if err := r.db.QueryRowContext(ctx, stockQ, lowStockThreshold).Scan(&stats.LowStockCount); err != nil {
		return admin.DashboardStats{}, fmt.Errorf("stats_repo: low stock: %w", err)
	}

	// 4. Recent orders.
	const recentQ = `SELECT id, customer_id, total_amount, total_currency, status, created_at
		FROM orders ORDER BY created_at DESC LIMIT $1`
	rows, err := r.db.QueryContext(ctx, recentQ, recentLimit)
	if err != nil {
		return admin.DashboardStats{}, fmt.Errorf("stats_repo: recent orders: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var ro admin.RecentOrder
		var createdAt time.Time
		if err := rows.Scan(&ro.ID, &ro.CustomerID, &ro.TotalAmount, &ro.Currency, &ro.Status, &createdAt); err != nil {
			return admin.DashboardStats{}, fmt.Errorf("stats_repo: recent orders scan: %w", err)
		}
		ro.CreatedAt = createdAt.UTC().Format(time.RFC3339)
		stats.RecentOrders = append(stats.RecentOrders, ro)
	}
	if err := rows.Err(); err != nil {
		return admin.DashboardStats{}, fmt.Errorf("stats_repo: recent orders rows: %w", err)
	}

	return stats, nil
}
