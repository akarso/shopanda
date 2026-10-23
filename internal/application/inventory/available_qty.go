package inventory

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	catalogapp "github.com/akarso/shopanda/internal/application/catalog"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/inventory"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

// AvailableQtySources are the reads ResolveAvailableQty needs.
// Stock, Variants, and Products are required. Components is required when the
// product is a dynamic-stock bundle.
type AvailableQtySources struct {
	Stock      inventory.StockRepository
	Variants   catalog.VariantRepository
	Products   catalog.ProductRepository
	Components catalog.BundleComponentRepository
}

// RejectQtyAboveAvailable returns a validation error when lineQty is above
// ResolveAvailableQty ("only N available"). lineQty below 1 is
// "quantity must be positive". A leaf-walk failure that already uses
// catalog.BundleCannotBeReservedMessage is returned unchanged. A validation
// or not-found error keeps its Message when that text is set. An empty
// Message on those codes is wrapped as "available quantity" with the
// original error on Unwrap, so err.Error() is not copied onto Message.
// Any other error (stock or repository infrastructure, including Internal)
// is returned unchanged so a guest-cart claim aborts instead of dropping
// the line. Cart add and checkout ValidateCartStep both call this so the
// two gates cannot drift.
func RejectQtyAboveAvailable(ctx context.Context, src AvailableQtySources, productID, variantID string, lineQty int) error {
	if lineQty < 1 {
		return apperror.Validation("quantity must be positive")
	}
	available, err := ResolveAvailableQty(ctx, src, productID, variantID)
	if err != nil {
		var appErr *apperror.Error
		if errors.As(err, &appErr) && appErr.Message == catalog.BundleCannotBeReservedMessage {
			return err
		}
		if apperror.Is(err, apperror.CodeValidation) || apperror.Is(err, apperror.CodeNotFound) {
			if errors.As(err, &appErr) && appErr.Message != "" {
				return apperror.Validation(appErr.Message)
			}
			return apperror.Wrap(apperror.CodeValidation, "available quantity", err)
		}
		return err
	}
	if lineQty > available {
		return apperror.Validation(fmt.Sprintf("only %d available", available))
	}
	return nil
}

// ResolveAvailableQty returns how many kits or units can be sold.
//
// productID and variantID identify the sellable product. Pass the cart line's
// variant when it is known. When only productID is set, a fixed product uses
// the first variant (ListByProductID offset 0, limit 1), the same row GetStock
// uses for a product that has not named a variant.
//
// Fixed products and non-bundles return that variant's stock quantity. A
// missing stock row is 0, matching GetStock. Grouped parents return 0.
//
// A dynamic bundle returns min(floor(componentStock / required quantity))
// across required components. Optional components are ignored. The bundle's
// own stock row is not read. The leaf rules are catalogapp.RequiredLeafVariants
// (active, leaf type, exactly one variant, required count 1–50).
//
// Virtual and downloadable required leaves do not gate the minimum: they are
// skipped. A bundle whose required leaves are all virtual or downloadable
// returns catalog.MaxDigitalBundleLineQty, a finite anti-abuse line cap,
// not a stock-row quantity and not an unlimited count. A physical simple
// leaf still needs a stock row; a missing row or a zero quantity contributes
// 0 and the bundle is unavailable. When at least one simple leaf gates, the
// result is min(floor(stock / required quantity)) and the digital cap is
// not applied. Leaf-walk failures use catalog.BundleCannotBeReservedMessage
// with the component detail on Unwrap.
func ResolveAvailableQty(ctx context.Context, src AvailableQtySources, productID, variantID string) (int, error) {
	if src.Stock == nil || src.Variants == nil || src.Products == nil {
		return 0, apperror.Validation("catalog repositories are required to resolve available quantity")
	}
	productID = strings.TrimSpace(productID)
	variantID = strings.TrimSpace(variantID)
	if productID == "" && variantID == "" {
		return 0, apperror.Validation("product or variant id is required")
	}

	if variantID != "" {
		variant, err := src.Variants.FindByID(ctx, variantID)
		if err != nil {
			return 0, fmt.Errorf("variant %s: %w", variantID, err)
		}
		if variant == nil {
			return 0, apperror.NotFound(fmt.Sprintf("variant %s not found", variantID))
		}
		if productID != "" && productID != variant.ProductID {
			return 0, apperror.Validation("variant does not belong to product")
		}
		productID = variant.ProductID
	}

	product, err := src.Products.FindByID(ctx, productID)
	if err != nil {
		return 0, fmt.Errorf("product %s: %w", productID, err)
	}
	if product == nil {
		return 0, apperror.NotFound(fmt.Sprintf("product %s not found", productID))
	}
	if product.Type == catalog.TypeGrouped {
		return 0, nil
	}
	if product.Type == catalog.TypeBundle && product.StockMode == catalog.StockModeDynamic {
		return dynamicBundleAvailableQty(ctx, src, product.ID)
	}

	stockVariantID := variantID
	if stockVariantID == "" {
		variants, err := src.Variants.ListByProductID(ctx, product.ID, 0, 1)
		if err != nil {
			return 0, fmt.Errorf("product %s variants: %w", product.ID, err)
		}
		if len(variants) == 0 {
			return 0, nil
		}
		stockVariantID = variants[0].ID
	}
	entry, err := src.Stock.GetStock(ctx, stockVariantID)
	if err != nil {
		return 0, fmt.Errorf("variant %s stock: %w", stockVariantID, err)
	}
	return entry.Quantity, nil
}

func dynamicBundleAvailableQty(ctx context.Context, src AvailableQtySources, bundleProductID string) (int, error) {
	leaves, err := catalogapp.RequiredLeafVariants(ctx, src.Products, src.Variants, src.Components, bundleProductID)
	if err != nil {
		return 0, err
	}
	minKits := math.MaxInt
	gated := false
	for _, leaf := range leaves {
		switch leaf.Product.Type {
		case catalog.TypeVirtual, catalog.TypeDownloadable:
			// Digital leaves are not stock-gated. They do not lower minKits.
			continue
		case catalog.TypeSimple:
			entry, err := src.Stock.GetStock(ctx, leaf.Variant.ID)
			if err != nil {
				return 0, fmt.Errorf("bundle component %s stock: %w", leaf.Component.ComponentProductID, err)
			}
			if leaf.Component.Quantity < 1 {
				return 0, catalogapp.BundleCustomerError(apperror.Validation(fmt.Sprintf(
					"dynamic bundle component %s has invalid quantity",
					leaf.Component.ComponentProductID,
				)))
			}
			kits := entry.Quantity / leaf.Component.Quantity
			if kits < 0 {
				kits = 0
			}
			if kits < minKits {
				minKits = kits
			}
			gated = true
		default:
			return 0, catalogapp.BundleCustomerError(apperror.Validation(fmt.Sprintf(
				"dynamic bundle component %s must be a simple, virtual, or downloadable product",
				leaf.Component.ComponentProductID,
			)))
		}
	}
	if !gated {
		return catalog.MaxDigitalBundleLineQty, nil
	}
	return minKits, nil
}
