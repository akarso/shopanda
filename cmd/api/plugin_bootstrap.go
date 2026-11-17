package main

import (
	"database/sql"
	"fmt"

	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

// newPluginBootstrap builds plugin Init handles from an open DB.
// Domain repos are constructed here (composition root) so plugins do not
// import internal/infrastructure. NewCustomerRepo/NewVariantRepo only error on
// nil db (already rejected).
func newPluginBootstrap(conn *sql.DB) (*plugin.Bootstrap, error) {
	if conn == nil {
		return nil, fmt.Errorf("plugin bootstrap: nil db")
	}
	customers, err := postgres.NewCustomerRepo(conn)
	if err != nil {
		return nil, fmt.Errorf("plugin bootstrap: customers: %w", err)
	}
	variants, err := postgres.NewVariantRepo(conn)
	if err != nil {
		return nil, fmt.Errorf("plugin bootstrap: variants: %w", err)
	}
	products, err := postgres.NewProductRepo(conn)
	if err != nil {
		return nil, fmt.Errorf("plugin bootstrap: products: %w", err)
	}
	orders, err := postgres.NewOrderRepo(conn)
	if err != nil {
		return nil, fmt.Errorf("plugin bootstrap: orders: %w", err)
	}
	carts, err := postgres.NewCartRepo(conn)
	if err != nil {
		return nil, fmt.Errorf("plugin bootstrap: carts: %w", err)
	}
	prices, err := postgres.NewPriceRepo(conn)
	if err != nil {
		return nil, fmt.Errorf("plugin bootstrap: prices: %w", err)
	}
	priceHistory, err := postgres.NewPriceHistoryRepo(conn)
	if err != nil {
		return nil, fmt.Errorf("plugin bootstrap: price history: %w", err)
	}
	stock, err := postgres.NewStockRepo(conn)
	if err != nil {
		return nil, fmt.Errorf("plugin bootstrap: stock: %w", err)
	}
	return &plugin.Bootstrap{
		DB:           conn,
		Customers:    customers,
		Variants:     variants,
		Products:     products,
		Prices:       prices,
		PriceHistory: priceHistory,
		Stock:        stock,
		Orders:       orders,
		Carts:        carts,
	}, nil
}
