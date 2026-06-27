package legal

import (
	"context"
	"fmt"
	"strings"
)

// Config keys for EPR packaging compliance (store-scoped via ScopedConfigKey).
const (
	EprEnabledConfigKey              = "legal.epr_enabled"
	EprSchemeRegistrationConfigKey   = "legal.epr_scheme_registration_id"
)

// Product attribute codes for EPR packaging metadata.
const (
	AttrEprPackagingMaterial      = "epr_packaging_material"
	AttrEprPackagingWeightG       = "epr_packaging_weight_g"
	AttrEprRecyclable             = "epr_recyclable"
	AttrEprRecycledContentPct     = "epr_recycled_content_pct"
	AttrEprSchemeRegistrationID   = "epr_scheme_registration_id"
)

// AttributeGroupEpr is the admin attribute group code for EPR fields.
const AttributeGroupEpr = "epr"

// EprMaterialLabels maps stored select values to human-readable labels.
var EprMaterialLabels = map[string]string{
	"plastic":         "Plastic",
	"paper_cardboard": "Paper and cardboard",
	"glass":           "Glass",
	"metal":           "Metal",
	"wood":            "Wood",
	"composite":       "Composite",
	"other":           "Other",
}

// EprMaterialOptions returns select options for epr_packaging_material.
func EprMaterialOptions() []string {
	return []string{
		"plastic",
		"paper_cardboard",
		"glass",
		"metal",
		"wood",
		"composite",
		"other",
	}
}

// EprPackaging holds parsed EPR packaging facts for a product/variant row.
type EprPackaging struct {
	Material             string
	MaterialLabel        string
	WeightG              float64
	Recyclable           bool
	RecycledContentPct   float64
	SchemeRegistrationID string
}

// EprEnabled reports whether EPR packaging tracking is enabled for a store.
// Missing config defaults to false (opt-in).
func EprEnabled(ctx context.Context, repo ConfigGetter, storeID string) (bool, error) {
	if repo == nil {
		return false, nil
	}
	if storeID != "" {
		v, err := repo.Get(ctx, ScopedConfigKey(storeID, EprEnabledConfigKey))
		if err != nil {
			return false, fmt.Errorf("epr config: store scope: %w", err)
		}
		if v != nil {
			return truthy(v), nil
		}
	}
	v, err := repo.Get(ctx, EprEnabledConfigKey)
	if err != nil {
		return false, fmt.Errorf("epr config: global: %w", err)
	}
	if v == nil {
		return false, nil
	}
	return truthy(v), nil
}

// StoreSchemeRegistrationID returns the store-level EPR scheme membership ID.
func StoreSchemeRegistrationID(ctx context.Context, repo ConfigGetter, storeID string) (string, error) {
	if repo == nil {
		return "", nil
	}
	var raw interface{}
	var err error
	if storeID != "" {
		raw, err = repo.Get(ctx, ScopedConfigKey(storeID, EprSchemeRegistrationConfigKey))
		if err != nil {
			return "", fmt.Errorf("epr scheme registration: store scope: %w", err)
		}
	}
	if raw == nil {
		raw, err = repo.Get(ctx, EprSchemeRegistrationConfigKey)
		if err != nil {
			return "", fmt.Errorf("epr scheme registration: global: %w", err)
		}
	}
	return stringValue(raw), nil
}

// ParseEprPackaging merges product- and variant-level EPR attributes.
// Variant values override product values when both are set (same precedence as
// material, recyclability, and recycled content). Numeric zero means unset for
// packaging weight in HasData and CSV output.
func ParseEprPackaging(productAttrs, variantAttrs map[string]interface{}) EprPackaging {
	material := stringValue(firstAttr(productAttrs, variantAttrs, AttrEprPackagingMaterial))

	return EprPackaging{
		Material:             material,
		MaterialLabel:        EprMaterialLabel(material),
		WeightG:              numberValue(firstAttr(productAttrs, variantAttrs, AttrEprPackagingWeightG)),
		Recyclable:           boolValue(firstAttr(productAttrs, variantAttrs, AttrEprRecyclable)),
		RecycledContentPct:   numberValue(firstAttr(productAttrs, variantAttrs, AttrEprRecycledContentPct)),
		SchemeRegistrationID: stringValue(firstAttr(productAttrs, variantAttrs, AttrEprSchemeRegistrationID)),
	}
}

// WithStoreSchemeID fills product-level scheme registration from store config when empty.
func (p EprPackaging) WithStoreSchemeID(storeSchemeID string) EprPackaging {
	if strings.TrimSpace(p.SchemeRegistrationID) == "" {
		p.SchemeRegistrationID = strings.TrimSpace(storeSchemeID)
	}
	return p
}

// HasData reports whether any EPR packaging field is populated.
func (p EprPackaging) HasData() bool {
	if strings.TrimSpace(p.Material) != "" {
		return true
	}
	if p.WeightG > 0 {
		return true
	}
	if p.Recyclable {
		return true
	}
	if p.RecycledContentPct > 0 {
		return true
	}
	return strings.TrimSpace(p.SchemeRegistrationID) != ""
}

// EprMaterialLabel returns the display label for a material code.
func EprMaterialLabel(code string) string {
	code = strings.TrimSpace(code)
	if code == "" {
		return ""
	}
	if label, ok := EprMaterialLabels[code]; ok {
		return label
	}
	return code
}

func attrValue(attrs map[string]interface{}, key string) interface{} {
	if len(attrs) == 0 {
		return nil
	}
	return attrs[key]
}

func firstAttr(productAttrs, variantAttrs map[string]interface{}, key string) interface{} {
	if v := attrValue(variantAttrs, key); v != nil && !isEmptyValue(v) {
		return v
	}
	return attrValue(productAttrs, key)
}

func isEmptyValue(v interface{}) bool {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t) == ""
	case nil:
		return true
	default:
		return false
	}
}

func numberValue(v interface{}) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case float32:
		return float64(t)
	case int:
		return float64(t)
	case int64:
		return float64(t)
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return 0
		}
		var f float64
		if _, err := fmt.Sscanf(s, "%f", &f); err == nil {
			return f
		}
		return 0
	default:
		return 0
	}
}
