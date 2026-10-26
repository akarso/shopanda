package catalog

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

// ComponentVariantProbe is how many variants we load to decide that a required
// component has exactly one. Two is enough to reject a multi-variant component
// without paging the whole catalog. The visibility and search SQL checks
// COUNT(*) = 1 for the same rule; keep those queries in sync with this probe.
const ComponentVariantProbe = 2

// RequiredLeaf is one required bundle component that passed the shared
// active-leaf and exactly-one-variant checks.
type RequiredLeaf struct {
	Component catalog.BundleComponent
	Product   *catalog.Product
	Variant   catalog.Variant
}

// productIDLister is an optional batch read. ProductRepository does not
// require it; postgres implements it so a dynamic bundle does not FindByID
// once per component. Callers that only have FindByID still work.
type productIDLister interface {
	ListByIDs(ctx context.Context, ids []string) (map[string]*catalog.Product, error)
}

// RequiredLeafVariants loads the required components of bundleProductID and
// returns each one with its single leaf variant.
//
// Optional components are ignored. An empty required set, a required quantity
// below 1, more than catalog.MaxBundleRequiredComponents required rows, a
// missing product, an inactive product (unless ctx includes non-active
// products), a non-leaf type, or any component that does not have exactly one
// variant is a validation error. Repository failures are returned wrapped.
//
// Pricing, checkout reservation, and available-quantity resolution all use
// this walk so those checks cannot drift.
func RequiredLeafVariants(
	ctx context.Context,
	products catalog.ProductRepository,
	variants catalog.VariantRepository,
	components catalog.BundleComponentRepository,
	bundleProductID string,
) ([]RequiredLeaf, error) {
	if products == nil || variants == nil {
		return nil, apperror.Validation("catalog repositories are required")
	}
	if components == nil {
		return nil, apperror.Validation("bundle component repository is required")
	}
	rows, err := components.ListByBundleID(ctx, bundleProductID)
	if err != nil {
		return nil, fmt.Errorf("bundle %s components: %w", bundleProductID, err)
	}

	required := make([]catalog.BundleComponent, 0)
	for _, component := range rows {
		if !component.Required {
			continue
		}
		if component.Quantity < 1 {
			return nil, BundleCustomerError(apperror.Validation(fmt.Sprintf("dynamic bundle component %s has invalid quantity", component.ComponentProductID)))
		}
		required = append(required, component)
	}
	if len(required) == 0 {
		return nil, BundleCustomerError(apperror.Validation(fmt.Sprintf("dynamic bundle %s has no required components", bundleProductID)))
	}
	if len(required) > catalog.MaxBundleRequiredComponents {
		return nil, BundleCustomerError(apperror.Validation(fmt.Sprintf(
			"dynamic bundle %s has %d required components; the maximum is %d",
			bundleProductID, len(required), catalog.MaxBundleRequiredComponents,
		)))
	}

	loadCtx := ctx
	if !catalog.IncludeNonActiveProducts(ctx) {
		loadCtx = catalog.WithIncludeNonActiveProducts(ctx)
	}
	allowInactive := catalog.IncludeNonActiveProducts(ctx)
	productIDs := make([]string, len(required))
	for i, component := range required {
		productIDs[i] = component.ComponentProductID
	}
	byID, err := loadProductsByID(loadCtx, products, productIDs)
	if err != nil {
		return nil, err
	}
	for _, component := range required {
		product := byID[component.ComponentProductID]
		if product == nil {
			return nil, BundleCustomerError(apperror.Validation(fmt.Sprintf("dynamic bundle component %s was not found", component.ComponentProductID)))
		}
		if !allowInactive && product.Status != catalog.StatusActive {
			return nil, BundleCustomerError(apperror.Validation(fmt.Sprintf("dynamic bundle component %s is not active", component.ComponentProductID)))
		}
		if !product.Type.IsBundleComponentLeafType() {
			return nil, BundleCustomerError(apperror.Validation(fmt.Sprintf(
				"dynamic bundle component %s must be a simple, virtual, or downloadable product",
				component.ComponentProductID,
			)))
		}
		if catalog.LinkedParentBlocksPurchase(product, catalog.AssignmentContextFrom(ctx)) {
			return nil, apperror.Validation(catalog.RequiresLinkedParentNotPurchasableMessage)
		}
	}

	variantsByProduct, err := variants.ListByProductIDs(ctx, productIDs, ComponentVariantProbe)
	if err != nil {
		return nil, fmt.Errorf("bundle %s component variants: %w", bundleProductID, err)
	}
	leaves := make([]RequiredLeaf, len(required))
	for i, component := range required {
		found := variantsByProduct[component.ComponentProductID]
		if len(found) != 1 {
			return nil, BundleCustomerError(apperror.Validation(fmt.Sprintf(
				"dynamic bundle component %s must have exactly one variant",
				component.ComponentProductID,
			)))
		}
		leaves[i] = RequiredLeaf{
			Component: component,
			Product:   byID[component.ComponentProductID],
			Variant:   found[0],
		}
	}
	return leaves, nil
}

// RejectRequiredLinkedParentLeaves denies a bundle whose required child has
// RequiresLinkedParent set and assignment empty. Used by cart and checkout
// for every bundle type, not only dynamic explode. Optional components are
// ignored. A missing products or components repo is a no-op.
func RejectRequiredLinkedParentLeaves(
	ctx context.Context,
	products catalog.ProductRepository,
	components catalog.BundleComponentRepository,
	bundleProductID string,
	assignment catalog.AssignmentContext,
) error {
	if products == nil || components == nil {
		return nil
	}
	bundleProductID = strings.TrimSpace(bundleProductID)
	if bundleProductID == "" {
		return nil
	}
	rows, err := components.ListByBundleID(ctx, bundleProductID)
	if err != nil {
		return fmt.Errorf("bundle %s components: %w", bundleProductID, err)
	}
	ids := make([]string, 0)
	for _, component := range rows {
		if component.Required {
			ids = append(ids, component.ComponentProductID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	loadCtx := ctx
	if !catalog.IncludeNonActiveProducts(ctx) {
		loadCtx = catalog.WithIncludeNonActiveProducts(ctx)
	}
	byID, err := loadProductsByID(loadCtx, products, ids)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if catalog.LinkedParentBlocksPurchase(byID[id], assignment) {
			return apperror.Validation(catalog.RequiresLinkedParentNotPurchasableMessage)
		}
	}
	return nil
}

// LinkedParentChildProductIDs is the products IsEligible must accept for
// this cart/order line. A RequiresLinkedParent product is itself. A bundle
// contributes each required leaf that has the flag. Other types are empty.
func LinkedParentChildProductIDs(
	ctx context.Context,
	products catalog.ProductRepository,
	components catalog.BundleComponentRepository,
	p *catalog.Product,
) ([]string, error) {
	if p == nil {
		return nil, nil
	}
	if p.RequiresLinkedParent {
		return []string{p.ID}, nil
	}
	if p.Type != catalog.TypeBundle || products == nil || components == nil {
		return nil, nil
	}
	rows, err := components.ListByBundleID(ctx, p.ID)
	if err != nil {
		return nil, fmt.Errorf("bundle %s components: %w", p.ID, err)
	}
	ids := make([]string, 0)
	for _, component := range rows {
		if component.Required {
			ids = append(ids, component.ComponentProductID)
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	loadCtx := ctx
	if !catalog.IncludeNonActiveProducts(ctx) {
		loadCtx = catalog.WithIncludeNonActiveProducts(ctx)
	}
	byID, err := loadProductsByID(loadCtx, products, ids)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0)
	for _, id := range ids {
		if byID[id] != nil && byID[id].RequiresLinkedParent {
			out = append(out, id)
		}
	}
	return out, nil
}

// BundleCustomerError returns a validation error whose Message is
// catalog.BundleCannotBeReservedMessage. detail stays available via Unwrap
// for logs. A detail that already uses that message is returned unchanged
// so callers can wrap again without nesting the same customer string.
func BundleCustomerError(detail error) error {
	if detail == nil {
		return nil
	}
	var appErr *apperror.Error
	if errors.As(detail, &appErr) && appErr.Message == catalog.BundleCannotBeReservedMessage {
		return detail
	}
	return apperror.Wrap(apperror.CodeValidation, catalog.BundleCannotBeReservedMessage, detail)
}

func loadProductsByID(ctx context.Context, products catalog.ProductRepository, ids []string) (map[string]*catalog.Product, error) {
	if lister, ok := products.(productIDLister); ok {
		byID, err := lister.ListByIDs(ctx, ids)
		if err != nil {
			return nil, fmt.Errorf("bundle components: %w", err)
		}
		if byID == nil {
			return map[string]*catalog.Product{}, nil
		}
		return byID, nil
	}
	byID := make(map[string]*catalog.Product, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		product, err := products.FindByID(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("bundle component %s: %w", id, err)
		}
		if product != nil {
			byID[id] = product
		}
	}
	return byID, nil
}
