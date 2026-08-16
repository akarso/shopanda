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
	customers, _ := postgres.NewCustomerRepo(conn)
	variants, _ := postgres.NewVariantRepo(conn)
	return &plugin.Bootstrap{
		DB:        conn,
		Customers: customers,
		Variants:  variants,
	}, nil
}
