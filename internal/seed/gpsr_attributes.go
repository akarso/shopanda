package seed

import (
	"context"
	"strings"

	adminApp "github.com/akarso/shopanda/internal/application/admin"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/legal"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
)

// GpsrAttributesSeeder registers GPSR safety attribute definitions and
// optionally seeds demo safety metadata on the cotton-tshirt product.
type GpsrAttributesSeeder struct{}

func (s *GpsrAttributesSeeder) Name() string { return "gpsr-attributes" }

func (s *GpsrAttributesSeeder) Seed(ctx context.Context, deps Deps) error {
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
		{Code: legal.AttrGpsrManufacturerName, Label: "Manufacturer name", Type: catalog.AttributeTypeText},
		{Code: legal.AttrGpsrManufacturerContact, Label: "Manufacturer EU contact", Type: catalog.AttributeTypeText},
		{Code: legal.AttrGpsrImporterName, Label: "Importer name", Type: catalog.AttributeTypeText},
		{Code: legal.AttrGpsrImporterContact, Label: "Importer EU contact", Type: catalog.AttributeTypeText},
		{Code: legal.AttrGpsrProductIdentifier, Label: "Product identifier (GTIN/EAN)", Type: catalog.AttributeTypeText},
		{Code: legal.AttrGpsrSafetyWarnings, Label: "Safety warnings", Type: catalog.AttributeTypeText},
		{
			Code:    legal.AttrGpsrAgeRestriction,
			Label:   "Age restriction",
			Type:    catalog.AttributeTypeSelect,
			Options: legal.GpsrAgeRestrictionOptions(),
		},
		{
			Code:    legal.AttrGpsrConformityMark,
			Label:   "Conformity marking",
			Type:    catalog.AttributeTypeSelect,
			Options: legal.GpsrConformityMarkOptions(),
		},
	}
	for _, attr := range definitions {
		if _, ok := known[attr.Code]; ok {
			deps.Logger.Info("seed.gpsr_attribute.skip", map[string]interface{}{
				"code": attr.Code,
			})
			continue
		}
		if err := store.CreateAttribute(ctx, attr); err != nil {
			return err
		}
		deps.Logger.Info("seed.gpsr_attribute.created", map[string]interface{}{
			"code": attr.Code,
		})
	}

	groups, err := store.ListGroups(ctx)
	if err != nil {
		return err
	}
	groupExists := false
	for _, g := range groups {
		if g.Code == legal.AttributeGroupGpsr {
			groupExists = true
			break
		}
	}
	if !groupExists {
		group, err := catalog.NewAttributeGroup(legal.AttributeGroupGpsr, "GPSR product safety")
		if err != nil {
			return err
		}
		group.Attributes = []string{
			legal.AttrGpsrManufacturerName,
			legal.AttrGpsrManufacturerContact,
			legal.AttrGpsrImporterName,
			legal.AttrGpsrImporterContact,
			legal.AttrGpsrProductIdentifier,
			legal.AttrGpsrSafetyWarnings,
			legal.AttrGpsrAgeRestriction,
			legal.AttrGpsrConformityMark,
		}
		if err := store.CreateGroup(ctx, group); err != nil {
			return err
		}
		deps.Logger.Info("seed.gpsr_attribute_group.created", map[string]interface{}{
			"code": legal.AttributeGroupGpsr,
		})
	} else {
		deps.Logger.Info("seed.gpsr_attribute_group.skip", map[string]interface{}{
			"code": legal.AttributeGroupGpsr,
		})
	}

	return s.seedDemoProduct(ctx, deps)
}

func (s *GpsrAttributesSeeder) seedDemoProduct(ctx context.Context, deps Deps) error {
	if !deps.DemoData {
		deps.Logger.Info("seed.gpsr_demo_product.skip", map[string]interface{}{
			"reason": "demo data disabled (pass --demo-seed to enable)",
		})
		return nil
	}

	prodRepo, err := postgres.NewProductRepo(deps.DB)
	if err != nil {
		return err
	}
	p, err := prodRepo.FindBySlug(ctx, "cotton-tshirt")
	if err != nil {
		return err
	}
	if p == nil {
		deps.Logger.Info("seed.gpsr_demo_product.skip", map[string]interface{}{
			"slug":   "cotton-tshirt",
			"reason": "product not found",
		})
		return nil
	}
	if v, ok := p.Attributes[legal.AttrGpsrSafetyWarnings]; ok && strings.TrimSpace(catalogString(v)) != "" {
		return nil
	}
	if p.Attributes == nil {
		p.Attributes = make(map[string]interface{})
	}
	p.Attributes[legal.AttrGpsrManufacturerName] = "Demo Apparel GmbH"
	p.Attributes[legal.AttrGpsrManufacturerContact] = "product-safety@demo.example"
	p.Attributes[legal.AttrGpsrSafetyWarnings] = "Keep away from fire. Wash before first use."
	p.Attributes[legal.AttrGpsrConformityMark] = "ce"
	if err := prodRepo.Update(ctx, p); err != nil {
		return err
	}
	deps.Logger.Info("seed.gpsr_demo_product.updated", map[string]interface{}{
		"slug": "cotton-tshirt",
	})
	return nil
}
