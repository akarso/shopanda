package postgres

import (
	"fmt"

	"github.com/akarso/shopanda/internal/domain/catalog"
)

// dynamicBundleQuantityPositiveSQL is the boolean expression for a
// dynamic-stock bundle's required-component floor. productIDExpr is the
// bundle product id SQL expression (p.id in visibility facts, sub.id in
// search).
//
// Virtual and downloadable required components pass without a stock row.
// Simple components still need a stock row whose floor(quantity / component
// quantity) is greater than 0. Optional components are ignored. An empty
// required set, more than catalog.MaxBundleRequiredComponents required rows,
// a non-leaf, an inactive component, or more than one variant is false.
// The bundle's own stock row is not read. Reservations are not subtracted;
// fixed-stock search keeps that subtraction in its own branch.
//
// Keep this expression identical for visibility QuantityPositive and search
// in_stock. Go's ResolveAvailableQty uses the same digital pass (no stock
// row). A digital-only kit is still in stock here because the boolean is
// "at least one"; the numeric line cap is catalog.MaxDigitalBundleLineQty.
func dynamicBundleQuantityPositiveSQL(productIDExpr string) string {
	return fmt.Sprintf(`(SELECT COUNT(*) FROM bundle_components bc WHERE bc.bundle_product_id = %s AND bc.required) BETWEEN 1 AND %d
					AND NOT EXISTS (
						SELECT 1
						FROM bundle_components bc
						WHERE bc.bundle_product_id = %s
							AND bc.required
							AND NOT (
								(SELECT COUNT(*) FROM variants v WHERE v.product_id = bc.component_product_id) = 1
								AND EXISTS (
									SELECT 1
									FROM variants v
									JOIN products cp ON cp.id = v.product_id
									WHERE v.product_id = bc.component_product_id
										AND cp.status = 'active'
										AND bc.quantity > 0
										AND (
											cp.type IN ('virtual', 'downloadable')
											OR (
												cp.type = 'simple'
												AND EXISTS (
													SELECT 1
													FROM stock s
													WHERE s.variant_id = v.id
														AND (s.quantity / bc.quantity) > 0
												)
											)
										)
								)
							)
					)`, productIDExpr, catalog.MaxBundleRequiredComponents, productIDExpr)
}
