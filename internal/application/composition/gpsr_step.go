package composition

import (
	"fmt"

	domainlegal "github.com/akarso/shopanda/internal/domain/legal"
)

// GpsrStep adds a gpsr_safety_disclosure block when GPSR is enabled for the
// store and the product (or store config) carries safety metadata.
type GpsrStep struct {
	config domainlegal.ConfigGetter
}

// NewGpsrStep creates a GpsrStep.
func NewGpsrStep(config domainlegal.ConfigGetter) *GpsrStep {
	return &GpsrStep{config: config}
}

func (s *GpsrStep) Name() string { return "gpsr_safety_disclosure" }

func (s *GpsrStep) Apply(ctx *ProductContext) error {
	if ctx == nil || ctx.Product == nil {
		return nil
	}

	enabled, err := domainlegal.GpsrEnabled(ctx.Ctx, s.config, ctx.StoreID)
	if err != nil {
		return fmt.Errorf("gpsr disclosure: %w", err)
	}
	if !enabled {
		return nil
	}

	storeName, err := domainlegal.StoreGpsrManufacturerName(ctx.Ctx, s.config, ctx.StoreID)
	if err != nil {
		return fmt.Errorf("gpsr disclosure: %w", err)
	}
	storeContact, err := domainlegal.StoreGpsrManufacturerContact(ctx.Ctx, s.config, ctx.StoreID)
	if err != nil {
		return fmt.Errorf("gpsr disclosure: %w", err)
	}

	info := domainlegal.ParseGpsrFromProduct(ctx.Product.Attributes).WithStoreManufacturer(storeName, storeContact)
	if !info.HasDisclosure() {
		return nil
	}

	ctx.Blocks = append(ctx.Blocks, Block{
		Type: "gpsr_safety_disclosure",
		Data: info.BlockData(),
	})
	return nil
}
