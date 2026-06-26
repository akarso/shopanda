package seed

import (
	"context"
	"strings"

	adminApp "github.com/akarso/shopanda/internal/application/admin"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/legal"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
)

// WeeeAttributesSeeder registers WEEE compliance attribute definitions and
// optionally seeds demo WEEE metadata on the wireless-mouse product.
type WeeeAttributesSeeder struct{}

func (s *WeeeAttributesSeeder) Name() string { return "weee-attributes" }

func (s *WeeeAttributesSeeder) Seed(ctx context.Context, deps Deps) error {
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
			Code:    legal.AttrWeeeCategory,
			Label:   "WEEE category",
			Type:    catalog.AttributeTypeSelect,
			Options: legal.WeeeCategoryOptions(),
		},
		{
			Code:  legal.AttrWeeeProducerRegistration,
			Label: "Producer registration number",
			Type:  catalog.AttributeTypeText,
		},
		{
			Code:  legal.AttrWeeeTakeBackInfo,
			Label: "Take-back / recycling info",
			Type:  catalog.AttributeTypeText,
		},
		{
			Code:  legal.AttrWeeeSymbolVisible,
			Label: "Show WEEE symbol on product page",
			Type:  catalog.AttributeTypeBoolean,
		},
	}
	for _, attr := range definitions {
		if _, ok := known[attr.Code]; ok {
			deps.Logger.Info("seed.weee_attribute.skip", map[string]interface{}{
				"code": attr.Code,
			})
			continue
		}
		if err := store.CreateAttribute(ctx, attr); err != nil {
			return err
		}
		deps.Logger.Info("seed.weee_attribute.created", map[string]interface{}{
			"code": attr.Code,
		})
	}

	groups, err := store.ListGroups(ctx)
	if err != nil {
		return err
	}
	groupExists := false
	for _, g := range groups {
		if g.Code == legal.AttributeGroupWeee {
			groupExists = true
			break
		}
	}
	if !groupExists {
		group, err := catalog.NewAttributeGroup(legal.AttributeGroupWeee, "WEEE compliance")
		if err != nil {
			return err
		}
		group.Attributes = []string{
			legal.AttrWeeeCategory,
			legal.AttrWeeeProducerRegistration,
			legal.AttrWeeeTakeBackInfo,
			legal.AttrWeeeSymbolVisible,
		}
		if err := store.CreateGroup(ctx, group); err != nil {
			return err
		}
		deps.Logger.Info("seed.weee_attribute_group.created", map[string]interface{}{
			"code": legal.AttributeGroupWeee,
		})
	} else {
		deps.Logger.Info("seed.weee_attribute_group.skip", map[string]interface{}{
			"code": legal.AttributeGroupWeee,
		})
	}

	return s.seedDemoProduct(ctx, deps)
}

func (s *WeeeAttributesSeeder) seedDemoProduct(ctx context.Context, deps Deps) error {
	prodRepo, err := postgres.NewProductRepo(deps.DB)
	if err != nil {
		return err
	}
	p, err := prodRepo.FindBySlug(ctx, "wireless-mouse")
	if err != nil || p == nil {
		return err
	}
	if v, ok := p.Attributes[legal.AttrWeeeCategory]; ok && strings.TrimSpace(catalogString(v)) != "" {
		return nil
	}
	if p.Attributes == nil {
		p.Attributes = make(map[string]interface{})
	}
	p.Attributes[legal.AttrWeeeCategory] = "small_it_telecom"
	p.Attributes[legal.AttrWeeeTakeBackInfo] = "Do not dispose of with household waste. Return to a collection point or retailer."
	p.Attributes[legal.AttrWeeeSymbolVisible] = true
	if err := prodRepo.Update(ctx, p); err != nil {
		return err
	}
	deps.Logger.Info("seed.weee_demo_product.updated", map[string]interface{}{
		"slug": "wireless-mouse",
	})
	return nil
}

func catalogString(v interface{}) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
