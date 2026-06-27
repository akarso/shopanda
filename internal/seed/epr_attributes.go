package seed

import (
	"context"
	"strings"

	adminApp "github.com/akarso/shopanda/internal/application/admin"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/legal"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
)

// EprAttributesSeeder registers EPR packaging attribute definitions and
// optionally seeds demo packaging metadata on the usb-c-cable product.
type EprAttributesSeeder struct{}

func (s *EprAttributesSeeder) Name() string { return "epr-attributes" }

func (s *EprAttributesSeeder) Seed(ctx context.Context, deps Deps) error {
	cfg := postgres.NewConfigRepo(deps.DB)
	store := adminApp.NewAttributeStore(cfg)

	existing, err := store.ListAttributes(ctx, "")
	if err != nil {
		return err
	}
	known := make(map[string]struct{}, len(existing))
	for _, a := range existing {
		known[a.Code] = struct{}{}
	}

	definitions := []catalog.Attribute{
		{
			Code:    legal.AttrEprPackagingMaterial,
			Label:   "Packaging material",
			Type:    catalog.AttributeTypeSelect,
			Options: legal.EprMaterialOptions(),
		},
		{
			Code:  legal.AttrEprPackagingWeightG,
			Label: "Packaging weight (g)",
			Type:  catalog.AttributeTypeNumber,
		},
		{
			Code:  legal.AttrEprRecyclable,
			Label: "Packaging recyclable",
			Type:  catalog.AttributeTypeBoolean,
		},
		{
			Code:  legal.AttrEprRecycledContentPct,
			Label: "Recycled content (%)",
			Type:  catalog.AttributeTypeNumber,
		},
		{
			Code:  legal.AttrEprSchemeRegistrationID,
			Label: "EPR scheme registration ID",
			Type:  catalog.AttributeTypeText,
		},
	}
	for _, attr := range definitions {
		if _, ok := known[attr.Code]; ok {
			deps.Logger.Info("seed.epr_attribute.skip", map[string]interface{}{
				"code": attr.Code,
			})
			continue
		}
		if err := store.CreateAttribute(ctx, attr); err != nil {
			return err
		}
		deps.Logger.Info("seed.epr_attribute.created", map[string]interface{}{
			"code": attr.Code,
		})
	}

	groups, err := store.ListGroups(ctx)
	if err != nil {
		return err
	}
	groupExists := false
	for _, g := range groups {
		if g.Code == legal.AttributeGroupEpr {
			groupExists = true
			break
		}
	}
	if !groupExists {
		group, err := catalog.NewAttributeGroup(legal.AttributeGroupEpr, "EPR packaging")
		if err != nil {
			return err
		}
		group.Attributes = []string{
			legal.AttrEprPackagingMaterial,
			legal.AttrEprPackagingWeightG,
			legal.AttrEprRecyclable,
			legal.AttrEprRecycledContentPct,
			legal.AttrEprSchemeRegistrationID,
		}
		if err := store.CreateGroup(ctx, group); err != nil {
			return err
		}
		deps.Logger.Info("seed.epr_attribute_group.created", map[string]interface{}{
			"code": legal.AttributeGroupEpr,
		})
	} else {
		deps.Logger.Info("seed.epr_attribute_group.skip", map[string]interface{}{
			"code": legal.AttributeGroupEpr,
		})
	}

	return s.seedDemoProduct(ctx, deps)
}

func (s *EprAttributesSeeder) seedDemoProduct(ctx context.Context, deps Deps) error {
	prodRepo, err := postgres.NewProductRepo(deps.DB)
	if err != nil {
		return err
	}
	variantRepo, err := postgres.NewVariantRepo(deps.DB)
	if err != nil {
		return err
	}

	p, err := prodRepo.FindBySlug(ctx, "usb-c-cable")
	if err != nil || p == nil {
		return err
	}
	if v, ok := p.Attributes[legal.AttrEprPackagingMaterial]; ok && strings.TrimSpace(catalogString(v)) != "" {
		return nil
	}
	if p.Attributes == nil {
		p.Attributes = make(map[string]interface{})
	}
	p.Attributes[legal.AttrEprPackagingMaterial] = "plastic"
	p.Attributes[legal.AttrEprRecyclable] = true
	p.Attributes[legal.AttrEprRecycledContentPct] = 20
	if err := prodRepo.Update(ctx, p); err != nil {
		return err
	}

	v, err := variantRepo.FindBySKU(ctx, "USBC-1M")
	if err != nil || v == nil {
		return err
	}
	if v.Attributes == nil {
		v.Attributes = make(map[string]interface{})
	}
	if number, ok := v.Attributes[legal.AttrEprPackagingWeightG]; ok && catalogString(number) != "" {
		return nil
	}
	v.Attributes[legal.AttrEprPackagingWeightG] = 18
	if err := variantRepo.Update(ctx, v); err != nil {
		return err
	}

	deps.Logger.Info("seed.epr_demo_product.updated", map[string]interface{}{
		"slug": "usb-c-cable",
		"sku":  "USBC-1M",
	})
	return nil
}
