package legal

import (
	"context"
	"fmt"
	"strings"
)

// Config keys for GPSR storefront disclosure (store-scoped via ScopedConfigKey).
const (
	GpsrEnabledConfigKey              = "legal.gpsr_enabled"
	GpsrManufacturerNameConfigKey     = "legal.gpsr_manufacturer_name"
	GpsrManufacturerContactConfigKey  = "legal.gpsr_manufacturer_contact"
)

// Product attribute codes for GPSR safety metadata.
const (
	AttrGpsrManufacturerName    = "gpsr_manufacturer_name"
	AttrGpsrManufacturerContact = "gpsr_manufacturer_contact"
	AttrGpsrImporterName          = "gpsr_importer_name"
	AttrGpsrImporterContact       = "gpsr_importer_contact"
	AttrGpsrProductIdentifier   = "gpsr_product_identifier"
	AttrGpsrSafetyWarnings        = "gpsr_safety_warnings"
	AttrGpsrAgeRestriction        = "gpsr_age_restriction"
	AttrGpsrConformityMark        = "gpsr_conformity_mark"
)

// AttributeGroupGpsr is the admin attribute group code for GPSR fields.
const AttributeGroupGpsr = "gpsr"

// GpsrAgeRestrictionLabels maps stored select values to customer-facing labels.
var GpsrAgeRestrictionLabels = map[string]string{
	"3_plus":  "Not suitable for children under 3 years",
	"14_plus": "Not suitable for children under 14 years",
	"18_plus": "For adults only (18+)",
}

// GpsrConformityMarkLabels maps stored select values to customer-facing labels.
var GpsrConformityMarkLabels = map[string]string{
	"ce":       "CE marking",
	"ukca":     "UKCA marking",
	"ce_ukca":  "CE and UKCA marking",
}

// GpsrAgeRestrictionOptions returns select options for gpsr_age_restriction.
func GpsrAgeRestrictionOptions() []string {
	return []string{"3_plus", "14_plus", "18_plus"}
}

// GpsrConformityMarkOptions returns select options for gpsr_conformity_mark.
func GpsrConformityMarkOptions() []string {
	return []string{"ce", "ukca", "ce_ukca"}
}

// GpsrInfo holds parsed GPSR safety disclosure data for a product page.
type GpsrInfo struct {
	ManufacturerName       string
	ManufacturerContact    string
	ImporterName           string
	ImporterContact        string
	ProductIdentifier      string
	SafetyWarnings         string
	AgeRestriction         string
	AgeRestrictionLabel    string
	ConformityMark         string
	ConformityMarkLabel    string
}

// GpsrEnabled reports whether GPSR safety disclosure should run for a store.
// Missing config defaults to false (opt-in).
func GpsrEnabled(ctx context.Context, repo ConfigGetter, storeID string) (bool, error) {
	if repo == nil {
		return false, nil
	}
	if storeID != "" {
		v, err := repo.Get(ctx, ScopedConfigKey(storeID, GpsrEnabledConfigKey))
		if err != nil {
			return false, fmt.Errorf("gpsr config: store scope: %w", err)
		}
		if v != nil {
			return truthy(v), nil
		}
	}
	v, err := repo.Get(ctx, GpsrEnabledConfigKey)
	if err != nil {
		return false, fmt.Errorf("gpsr config: global: %w", err)
	}
	if v == nil {
		return false, nil
	}
	return truthy(v), nil
}

// StoreGpsrManufacturerName returns the store-level default manufacturer name.
func StoreGpsrManufacturerName(ctx context.Context, repo ConfigGetter, storeID string) (string, error) {
	return scopedStringConfig(ctx, repo, storeID, GpsrManufacturerNameConfigKey, "gpsr manufacturer name")
}

// StoreGpsrManufacturerContact returns the store-level default manufacturer EU contact.
func StoreGpsrManufacturerContact(ctx context.Context, repo ConfigGetter, storeID string) (string, error) {
	return scopedStringConfig(ctx, repo, storeID, GpsrManufacturerContactConfigKey, "gpsr manufacturer contact")
}

func scopedStringConfig(ctx context.Context, repo ConfigGetter, storeID, key, label string) (string, error) {
	if repo == nil {
		return "", nil
	}
	var raw interface{}
	var err error
	if storeID != "" {
		raw, err = repo.Get(ctx, ScopedConfigKey(storeID, key))
		if err != nil {
			return "", fmt.Errorf("%s: store scope: %w", label, err)
		}
	}
	if raw == nil {
		raw, err = repo.Get(ctx, key)
		if err != nil {
			return "", fmt.Errorf("%s: global: %w", label, err)
		}
	}
	return stringValue(raw), nil
}

// ParseGpsrFromProduct extracts GPSR fields from product attributes.
func ParseGpsrFromProduct(attrs map[string]interface{}) GpsrInfo {
	if len(attrs) == 0 {
		return GpsrInfo{}
	}
	age := stringValue(attrs[AttrGpsrAgeRestriction])
	mark := stringValue(attrs[AttrGpsrConformityMark])
	return GpsrInfo{
		ManufacturerName:    stringValue(attrs[AttrGpsrManufacturerName]),
		ManufacturerContact: stringValue(attrs[AttrGpsrManufacturerContact]),
		ImporterName:        stringValue(attrs[AttrGpsrImporterName]),
		ImporterContact:     stringValue(attrs[AttrGpsrImporterContact]),
		ProductIdentifier:   stringValue(attrs[AttrGpsrProductIdentifier]),
		SafetyWarnings:      stringValue(attrs[AttrGpsrSafetyWarnings]),
		AgeRestriction:      age,
		AgeRestrictionLabel: GpsrAgeRestrictionLabel(age),
		ConformityMark:      mark,
		ConformityMarkLabel: GpsrConformityMarkLabel(mark),
	}
}

// WithStoreManufacturer fills product-level manufacturer fields from store config when empty.
func (g GpsrInfo) WithStoreManufacturer(name, contact string) GpsrInfo {
	if strings.TrimSpace(g.ManufacturerName) == "" {
		g.ManufacturerName = strings.TrimSpace(name)
	}
	if strings.TrimSpace(g.ManufacturerContact) == "" {
		g.ManufacturerContact = strings.TrimSpace(contact)
	}
	return g
}

// HasDisclosure reports whether any customer-facing GPSR data is present.
func (g GpsrInfo) HasDisclosure() bool {
	if strings.TrimSpace(g.ManufacturerName) != "" {
		return true
	}
	if strings.TrimSpace(g.ManufacturerContact) != "" {
		return true
	}
	if strings.TrimSpace(g.ImporterName) != "" {
		return true
	}
	if strings.TrimSpace(g.ImporterContact) != "" {
		return true
	}
	if strings.TrimSpace(g.ProductIdentifier) != "" {
		return true
	}
	if strings.TrimSpace(g.SafetyWarnings) != "" {
		return true
	}
	if hasMeaningfulSelect(g.AgeRestriction) {
		return true
	}
	return hasMeaningfulSelect(g.ConformityMark)
}

// BlockData returns composition block payload for gpsr_safety_disclosure.
func (g GpsrInfo) BlockData() map[string]interface{} {
	return map[string]interface{}{
		"manufacturer_name":       g.ManufacturerName,
		"manufacturer_contact":    g.ManufacturerContact,
		"importer_name":           g.ImporterName,
		"importer_contact":        g.ImporterContact,
		"product_identifier":      g.ProductIdentifier,
		"safety_warnings":         g.SafetyWarnings,
		"age_restriction":         g.AgeRestriction,
		"age_restriction_label":   g.AgeRestrictionLabel,
		"conformity_mark":         g.ConformityMark,
		"conformity_mark_label":   g.ConformityMarkLabel,
	}
}

// GpsrAgeRestrictionLabel returns the display label for an age restriction code.
func GpsrAgeRestrictionLabel(code string) string {
	code = strings.TrimSpace(code)
	if !hasMeaningfulSelect(code) {
		return ""
	}
	if label, ok := GpsrAgeRestrictionLabels[code]; ok {
		return label
	}
	return code
}

// GpsrConformityMarkLabel returns the display label for a conformity mark code.
func GpsrConformityMarkLabel(code string) string {
	code = strings.TrimSpace(code)
	if !hasMeaningfulSelect(code) {
		return ""
	}
	if label, ok := GpsrConformityMarkLabels[code]; ok {
		return label
	}
	return code
}

func hasMeaningfulSelect(code string) bool {
	code = strings.TrimSpace(strings.ToLower(code))
	return code != "" && code != "none"
}
