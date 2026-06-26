package composition

import (
	"fmt"

	domainlegal "github.com/akarso/shopanda/internal/domain/legal"
)

// WeeeStep adds a weee_disclosure block when WEEE is enabled for the store and
// the product (or store config) carries compliance metadata.
type WeeeStep struct {
	config domainlegal.ConfigGetter
}

// NewWeeeStep creates a WeeeStep.
func NewWeeeStep(config domainlegal.ConfigGetter) *WeeeStep {
	return &WeeeStep{config: config}
}

func (s *WeeeStep) Name() string { return "weee_disclosure" }

func (s *WeeeStep) Apply(ctx *ProductContext) error {
	if ctx == nil || ctx.Product == nil {
		return nil
	}

	enabled, err := domainlegal.WeeeEnabled(ctx.Ctx, s.config, ctx.StoreID)
	if err != nil {
		return fmt.Errorf("weee disclosure: %w", err)
	}
	if !enabled {
		return nil
	}

	storeReg, err := domainlegal.StoreProducerRegistration(ctx.Ctx, s.config, ctx.StoreID)
	if err != nil {
		return fmt.Errorf("weee disclosure: %w", err)
	}

	info := domainlegal.ParseWeeeFromProduct(ctx.Product.Attributes).WithStoreRegistration(storeReg)
	if !info.HasDisclosure() {
		return nil
	}

	ctx.Blocks = append(ctx.Blocks, Block{
		Type: "weee_disclosure",
		Data: info.BlockData(),
	})
	return nil
}
