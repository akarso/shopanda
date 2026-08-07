package legal

import (
	"context"
	"fmt"
	"strings"
)

// Config keys for WEEE storefront disclosure (store-scoped via ScopedConfigKey).
const (
	WeeeEnabledConfigKey              = "legal.weee_enabled"
	WeeeProducerRegistrationConfigKey = "legal.weee_producer_registration"
)

// Product attribute codes for WEEE compliance metadata.
const (
	AttrWeeeCategory             = "weee_category"
	AttrWeeeProducerRegistration = "weee_producer_registration"
	AttrWeeeTakeBackInfo         = "weee_take_back_info"
	AttrWeeeSymbolVisible        = "weee_symbol_visible"
)

// AttributeGroupWeee is the admin attribute group code for WEEE fields.
const AttributeGroupWeee = "weee"

// WeeeCategoryLabels maps stored select values to customer-facing labels.
var WeeeCategoryLabels = map[string]string{
	"large_household":      "Large household appliances",
	"small_it_telecom":     "Small IT and telecommunications equipment",
	"consumer_equipment":   "Consumer equipment",
	"lighting":             "Lighting equipment",
	"tools":                "Electrical and electronic tools",
	"toys_leisure_sports":  "Toys, leisure and sports equipment",
	"medical_devices":      "Medical devices",
	"monitoring_control":   "Monitoring and control instruments",
	"automatic_dispensers": "Automatic dispensers",
}

// WeeeCategoryOptions returns select options for the weee_category attribute.
func WeeeCategoryOptions() []string {
	return []string{
		"large_household",
		"small_it_telecom",
		"consumer_equipment",
		"lighting",
		"tools",
		"toys_leisure_sports",
		"medical_devices",
		"monitoring_control",
		"automatic_dispensers",
	}
}

// WeeeInfo holds parsed WEEE disclosure data for a product page.
type WeeeInfo struct {
	Category             string
	CategoryLabel        string
	ProducerRegistration string
	TakeBackInfo         string
	SymbolVisible        bool
}

// WeeeEnabled reports whether WEEE disclosure should run for a store.
// Missing config defaults to false so merchants opt in explicitly.
func WeeeEnabled(ctx context.Context, repo ConfigGetter, storeID string) (bool, error) {
	if repo == nil {
		return false, nil
	}
	if storeID != "" {
		v, err := repo.Get(ctx, ScopedConfigKey(storeID, WeeeEnabledConfigKey))
		if err != nil {
			return false, fmt.Errorf("weee config: store scope: %w", err)
		}
		if v != nil {
			return truthy(v), nil
		}
	}
	v, err := repo.Get(ctx, WeeeEnabledConfigKey)
	if err != nil {
		return false, fmt.Errorf("weee config: global: %w", err)
	}
	if v == nil {
		return false, nil
	}
	return truthy(v), nil
}

// StoreProducerRegistration returns the store-level producer registration number.
func StoreProducerRegistration(ctx context.Context, repo ConfigGetter, storeID string) (string, error) {
	if repo == nil {
		return "", nil
	}
	var raw interface{}
	var err error
	if storeID != "" {
		raw, err = repo.Get(ctx, ScopedConfigKey(storeID, WeeeProducerRegistrationConfigKey))
		if err != nil {
			return "", fmt.Errorf("weee producer registration: store scope: %w", err)
		}
	}
	if raw == nil {
		raw, err = repo.Get(ctx, WeeeProducerRegistrationConfigKey)
		if err != nil {
			return "", fmt.Errorf("weee producer registration: global: %w", err)
		}
	}
	return stringValue(raw), nil
}

// ParseWeeeFromProduct extracts WEEE fields from product attributes.
func ParseWeeeFromProduct(attrs map[string]interface{}) WeeeInfo {
	if len(attrs) == 0 {
		return WeeeInfo{}
	}
	category := stringValue(attrs[AttrWeeeCategory])
	info := WeeeInfo{
		Category:             category,
		CategoryLabel:        WeeeCategoryLabel(category),
		ProducerRegistration: stringValue(attrs[AttrWeeeProducerRegistration]),
		TakeBackInfo:         stringValue(attrs[AttrWeeeTakeBackInfo]),
		SymbolVisible:        boolValue(attrs[AttrWeeeSymbolVisible]),
	}
	return info
}

// WithStoreRegistration fills product-level producer registration from store config when empty.
func (w WeeeInfo) WithStoreRegistration(storeRegistration string) WeeeInfo {
	if strings.TrimSpace(w.ProducerRegistration) == "" {
		w.ProducerRegistration = strings.TrimSpace(storeRegistration)
	}
	return w
}

// HasDisclosure reports whether any customer-facing WEEE data is present.
func (w WeeeInfo) HasDisclosure() bool {
	if strings.TrimSpace(w.Category) != "" {
		return true
	}
	if strings.TrimSpace(w.ProducerRegistration) != "" {
		return true
	}
	if strings.TrimSpace(w.TakeBackInfo) != "" {
		return true
	}
	return false
}

// BlockData returns composition block payload for weee_disclosure.
func (w WeeeInfo) BlockData() map[string]interface{} {
	return map[string]interface{}{
		"category":              w.Category,
		"category_label":        w.CategoryLabel,
		"producer_registration": w.ProducerRegistration,
		"take_back_info":        w.TakeBackInfo,
		"symbol_visible":        w.SymbolVisible,
	}
}

// WeeeCategoryLabel returns the display label for a category code.
func WeeeCategoryLabel(code string) string {
	code = strings.TrimSpace(code)
	if code == "" {
		return ""
	}
	if label, ok := WeeeCategoryLabels[code]; ok {
		return label
	}
	return code
}

func stringValue(v interface{}) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case fmt.Stringer:
		return strings.TrimSpace(t.String())
	default:
		if v == nil {
			return ""
		}
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func boolValue(v interface{}) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		s := strings.TrimSpace(strings.ToLower(t))
		return s == "true" || s == "1" || s == "yes"
	case float64:
		return t != 0
	case int:
		return t != 0
	case int64:
		return t != 0
	default:
		return false
	}
}
