package extension

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/akarso/shopanda/internal/application/hooks"
	domainext "github.com/akarso/shopanda/internal/domain/extension"
	"github.com/akarso/shopanda/internal/platform/ssrf"
	"github.com/akarso/shopanda/pkg/extapi"
)

// imageHostLookup resolves image URL hostnames for SSRF checks. Tests may
// replace it via SetImageHostLookupForTest; production uses the system resolver.
var imageHostLookup ssrf.LookupIPFunc = ssrf.DefaultLookupIP

const imageHostLookupTimeout = 2 * time.Second

// SetImageHostLookupForTest replaces DNS lookup used by image URL checks.
// Restores the previous lookup when the test ends.
func SetImageHostLookupForTest(t interface{ Cleanup(func()) }, fn ssrf.LookupIPFunc) {
	if t == nil {
		panic("extension: SetImageHostLookupForTest requires a test")
	}
	prev := imageHostLookup
	imageHostLookup = fn
	t.Cleanup(func() { imageHostLookup = prev })
}

// SoftFieldError reports a per-field APIValue failure skipped by
// PublicOrderItemAPIByVariant. Nil is fine (silent skip).
type SoftFieldError func(fieldCode string, err error)

// SoftHookError reports a skip-and-log enrich-handler failure. Nil is fine.
type SoftHookError func(registrant string, err error)

// SoftDisplaySkip reports a dropped enrich display row (bad kind/URL/cap).
// Nil is fine (silent skip).
type SoftDisplaySkip func(fieldCode, reason string)

// Caps for plugin-appended order.line.display fields (per line).
const (
	MaxOrderLineDisplayFields = 32
	MaxDisplayFieldCodeLen    = 128
	MaxDisplayFieldLabelLen   = 256
	MaxDisplayFieldValueLen   = 2048
	MaxDisplayFieldAltLen     = 256
)

// PublicOrderItemAPIByVariant loads public order_item extension values
// (includePrivate=false) keyed by variant id. Per-field APIValue failures are
// skipped (best-effort); onFieldErr is called with the field code when set.
// ListByOrderItems errors still return. Nil api or empty variantIDs yield (nil, nil).
func PublicOrderItemAPIByVariant(ctx context.Context, api ValueAPI, orderID string, variantIDs []string, onFieldErr SoftFieldError) (map[string][]APIValueItem, error) {
	if api == nil || len(variantIDs) == 0 {
		return nil, nil
	}
	storedByVariant, err := api.ListByOrderItems(ctx, orderID, variantIDs, false)
	if err != nil {
		return nil, fmt.Errorf("extension: list order item values: %w", err)
	}
	if len(storedByVariant) == 0 {
		return nil, nil
	}
	reg := api.Registry()
	out := make(map[string][]APIValueItem, len(storedByVariant))
	for variantID, stored := range storedByVariant {
		if len(stored) == 0 {
			continue
		}
		items := apiValuesForTargetSoft(reg, stored, onFieldErr)
		if len(items) == 0 {
			continue
		}
		out[variantID] = items
	}
	return out, nil
}

// apiValuesForTargetSoft is like APIValuesForTarget but skips unknown fields
// and per-field decode errors so one corrupt value cannot block email/SSR.
func apiValuesForTargetSoft(registry *Registry, values []domainext.Value, onFieldErr SoftFieldError) []APIValueItem {
	out := make([]APIValueItem, 0, len(values))
	for _, value := range values {
		if registry == nil {
			continue
		}
		field, ok := registry.Get(value.FieldCode)
		if !ok {
			continue
		}
		apiValue, err := domainext.APIValue(field, value.Payload)
		if err != nil {
			if onFieldErr != nil {
				onFieldErr(value.FieldCode, err)
			}
			continue
		}
		if apiValue == nil {
			continue
		}
		out = append(out, APIValueItem{
			FieldCode: value.FieldCode,
			Label:     field.Label,
			Type:      string(field.Type),
			Value:     apiValue,
		})
	}
	return out
}

// ExtensionMapFromAPIItems flattens API items to field_code → value for
// product email overrides ({{index .Extensions "code"}}). Duplicate codes
// keep the last value.
func ExtensionMapFromAPIItems(items []APIValueItem) map[string]interface{} {
	if len(items) == 0 {
		return nil
	}
	out := make(map[string]interface{}, len(items))
	for _, item := range items {
		out[item.FieldCode] = item.Value
	}
	return out
}

// CloneExtensionMap returns a deep copy of a public extension map (nested
// maps/slices included) so hook handlers cannot mutate the template/SSR map
// or a later handler's view via OrderLineDisplayExtensions().
func CloneExtensionMap(in map[string]interface{}) map[string]interface{} {
	return deepCopyExtensionMap(in)
}

func deepCopyExtensionMap(in map[string]interface{}) map[string]interface{} {
	if in == nil {
		return nil
	}
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		out[k] = deepCopyExtensionValue(v)
	}
	return out
}

func deepCopyExtensionValue(v interface{}) interface{} {
	switch x := v.(type) {
	case map[string]interface{}:
		return deepCopyExtensionMap(x)
	case []interface{}:
		out := make([]interface{}, len(x))
		for i, e := range x {
			out[i] = deepCopyExtensionValue(e)
		}
		return out
	default:
		return v
	}
}

// ApplyEnrichedOverrides writes normalized enrich rows (index >= baseLen) into
// dst, overwriting the same codes. Returns nil when the result is empty.
func ApplyEnrichedOverrides(dst map[string]interface{}, rows []ExtensionDisplayRow, baseLen int) map[string]interface{} {
	if baseLen < 0 {
		baseLen = 0
	}
	if dst == nil && baseLen >= len(rows) {
		return nil
	}
	if dst == nil {
		dst = map[string]interface{}{}
	}
	for i := baseLen; i < len(rows); i++ {
		dst[rows[i].FieldCode] = rows[i].Value
	}
	if len(dst) == 0 {
		return nil
	}
	return dst
}

// Display kinds for ExtensionDisplayRow (mirror pkg/extapi).
const (
	DisplayKindText  = extapi.DisplayKindText
	DisplayKindImage = extapi.DisplayKindImage
)

// ExtensionDisplayRow is a shopper-facing label/value pair for default email
// and SSR lists (stable registration order; JSON composites omitted).
// Kind is "text" (default) or "image" (Value = https URL, Alt = alt text).
type ExtensionDisplayRow struct {
	FieldCode string
	Label     string
	Kind      string
	Value     string
	Alt       string
}

// ExtensionDisplayRows builds labeled rows in registry registration order for
// TargetOrderItem. Skips nil values and JSON objects/arrays (not safe for
// default html/template bodies). Bools use Yes/No. Kind is always text.
func ExtensionDisplayRows(registry *Registry, items []APIValueItem) []ExtensionDisplayRow {
	if len(items) == 0 {
		return nil
	}
	byCode := make(map[string]APIValueItem, len(items))
	for _, item := range items {
		byCode[item.FieldCode] = item
	}
	var ordered []APIValueItem
	if registry != nil {
		for _, field := range PublicFieldsForScope(registry, domainext.TargetOrderItem) {
			if item, ok := byCode[field.Code]; ok {
				ordered = append(ordered, item)
				delete(byCode, field.Code)
			}
		}
	}
	for _, item := range items {
		if _, ok := byCode[item.FieldCode]; ok {
			ordered = append(ordered, item)
			delete(byCode, item.FieldCode)
		}
	}
	out := make([]ExtensionDisplayRow, 0, len(ordered))
	for _, item := range ordered {
		text, ok := FormatExtensionDisplayValue(item.Type, item.Value)
		if !ok {
			continue
		}
		label := item.Label
		if label == "" {
			label = item.FieldCode
		}
		out = append(out, ExtensionDisplayRow{
			FieldCode: item.FieldCode,
			Label:     label,
			Kind:      DisplayKindText,
			Value:     text,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// EnrichOrderLineDisplayRows runs order.line.display and appends normalized
// plugin fields after base (stored) rows. Pass a clone of the public extension
// map (CloneExtensionMap) — the hook must not share the template map.
// Handler errors are skip-and-log via onErr; dropped rows via onSkip.
// Image fields require public https URLs. Nil hooks is a no-op.
func EnrichOrderLineDisplayRows(
	ctx context.Context,
	hookReg *hooks.Registry,
	orderID, variantID string,
	extensions map[string]interface{},
	base []ExtensionDisplayRow,
	onErr SoftHookError,
	onSkip SoftDisplaySkip,
) []ExtensionDisplayRow {
	if hookReg == nil {
		return base
	}
	hookCtx := hooks.NewContext(hooks.HookOrderLineDisplay)
	hookCtx.Set("order_id", orderID)
	hookCtx.Set("variant_id", variantID)
	if extensions != nil {
		hookCtx.Set("extensions", extensions)
	}
	fields := []extapi.OrderLineDisplayField{}
	hookCtx.Set("fields", &fields)

	hookReg.InvokeContinueOnError(ctx, hookCtx, onErr)

	// Re-read like ValidationIssuesFromContext — Append may Set a new pointer.
	appended := hooks.OrderLineDisplayFieldsFromContext(hookCtx)
	if len(appended) == 0 {
		return base
	}

	out := append([]ExtensionDisplayRow(nil), base...)
	accepted := 0
	for _, f := range appended {
		if accepted >= MaxOrderLineDisplayFields {
			if onSkip != nil {
				onSkip(strings.TrimSpace(f.Code), "field_cap")
			}
			continue
		}
		row, reason := normalizeDisplayRow(ctx, ExtensionDisplayRow{
			FieldCode: f.Code,
			Label:     f.Label,
			Kind:      f.Kind,
			Value:     f.Value,
			Alt:       f.Alt,
		})
		if reason != "" {
			if onSkip != nil {
				code := strings.TrimSpace(f.Code)
				if code == "" {
					code = "(empty)"
				}
				onSkip(code, reason)
			}
			continue
		}
		out = append(out, row)
		accepted++
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// NormalizeDisplayRow validates Kind/Value for template rendering.
// Image values must be absolute https URLs to a public host (DNS-resolved).
func NormalizeDisplayRow(row ExtensionDisplayRow) (ExtensionDisplayRow, bool) {
	out, reason := normalizeDisplayRow(context.Background(), row)
	return out, reason == ""
}

func normalizeDisplayRow(ctx context.Context, row ExtensionDisplayRow) (ExtensionDisplayRow, string) {
	code := strings.TrimSpace(row.FieldCode)
	if code == "" {
		return ExtensionDisplayRow{}, "empty_code"
	}
	if len(code) > MaxDisplayFieldCodeLen {
		return ExtensionDisplayRow{}, "code_too_long"
	}

	kind := strings.TrimSpace(row.Kind)
	if kind == "" {
		kind = DisplayKindText
	}

	label := strings.TrimSpace(row.Label)
	if label == "" {
		label = code
	}
	if len(label) > MaxDisplayFieldLabelLen {
		return ExtensionDisplayRow{}, "label_too_long"
	}

	alt := strings.TrimSpace(row.Alt)
	if len(alt) > MaxDisplayFieldAltLen {
		return ExtensionDisplayRow{}, "alt_too_long"
	}

	switch kind {
	case DisplayKindText:
		value := strings.TrimSpace(row.Value)
		if value == "" {
			return ExtensionDisplayRow{}, "empty_value"
		}
		if len(value) > MaxDisplayFieldValueLen {
			return ExtensionDisplayRow{}, "value_too_long"
		}
		return ExtensionDisplayRow{
			FieldCode: code,
			Label:     label,
			Kind:      DisplayKindText,
			Value:     value,
		}, ""
	case DisplayKindImage:
		value := strings.TrimSpace(row.Value)
		if len(value) > MaxDisplayFieldValueLen {
			return ExtensionDisplayRow{}, "value_too_long"
		}
		normalized, ok := normalizeHTTPSImageURL(ctx, value)
		if !ok {
			return ExtensionDisplayRow{}, "image_url_rejected"
		}
		return ExtensionDisplayRow{
			FieldCode: code,
			Label:     label,
			Kind:      DisplayKindImage,
			Value:     normalized,
			Alt:       alt,
		}, ""
	default:
		return ExtensionDisplayRow{}, "unknown_kind"
	}
}

// normalizeHTTPSImageURL accepts https URLs (scheme case-insensitive), rejects
// credentials / non-public literals via ssrf.ValidateURL, and resolves
// hostnames so names that point at loopback (nip.io, localhost.localdomain)
// cannot become <img src>. On success returns a URL with scheme https.
func normalizeHTTPSImageURL(ctx context.Context, raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || u == nil {
		return "", false
	}
	if !strings.EqualFold(u.Scheme, "https") || u.Host == "" {
		return "", false
	}
	// Reject credentials (not useful for <img> and risky in email).
	if u.User != nil {
		return "", false
	}
	host := u.Hostname()
	if host == "" {
		return "", false
	}
	// Zone IDs (fe80::1%eth0) bypass net.ParseIP inside ValidateURL — reject.
	if strings.Contains(host, "%") {
		return "", false
	}
	// Trailing-dot FQDN form (localhost.) still resolves to loopback in browsers.
	canonicalHost := strings.TrimRight(strings.ToLower(host), ".")
	if canonicalHost == "localhost" || strings.HasSuffix(canonicalHost, ".localhost") {
		return "", false
	}

	check := *u
	check.Scheme = "https" // ValidateURL requires exact lowercase scheme
	check.User = nil
	switch {
	case u.Port() != "":
		if strings.Contains(canonicalHost, ":") {
			check.Host = "[" + canonicalHost + "]:" + u.Port()
		} else {
			check.Host = canonicalHost + ":" + u.Port()
		}
	case strings.Contains(canonicalHost, ":"):
		check.Host = "[" + canonicalHost + "]"
	default:
		check.Host = canonicalHost
	}
	if err := ssrf.ValidateURL(check.String()); err != nil {
		return "", false
	}

	// Literals are fully checked by ValidateURL. Hostnames must resolve to
	// public addresses — nip.io / localdomain tricks fail closed here.
	if net.ParseIP(canonicalHost) == nil {
		lookup := imageHostLookup
		if lookup == nil {
			lookup = ssrf.DefaultLookupIP
		}
		lookupCtx := ctx
		if lookupCtx == nil {
			lookupCtx = context.Background()
		}
		lookupCtx, cancel := context.WithTimeout(lookupCtx, imageHostLookupTimeout)
		defer cancel()
		ips, err := lookup(lookupCtx, canonicalHost)
		if err != nil {
			return "", false
		}
		if _, err := ssrf.FilterAllowedIPs(ips); err != nil {
			return "", false
		}
	}
	return check.String(), true
}

// DisplayRowsToTemplateList maps rows to template-friendly maps (email
// ExtensionsList / shared shape).
func DisplayRowsToTemplateList(rows []ExtensionDisplayRow) []map[string]interface{} {
	if len(rows) == 0 {
		return nil
	}
	list := make([]map[string]interface{}, len(rows))
	for i, row := range rows {
		list[i] = map[string]interface{}{
			"FieldCode": row.FieldCode,
			"Label":     row.Label,
			"Kind":      row.Kind,
			"Value":     row.Value,
			"Alt":       row.Alt,
		}
	}
	return list
}

// FormatExtensionDisplayValue turns an API value into shopper-facing text.
// ok is false for nil and JSON composites (map/slice) that must not appear in
// default email/SSR bodies.
func FormatExtensionDisplayValue(fieldType string, value interface{}) (string, bool) {
	if value == nil {
		return "", false
	}
	switch value.(type) {
	case map[string]interface{}, []interface{}:
		return "", false
	}
	if fieldType == string(domainext.FieldTypeJSON) {
		switch value.(type) {
		case string, bool, int, int64, float64:
			// scalar JSON decode — still printable
		default:
			return "", false
		}
	}
	switch v := value.(type) {
	case bool:
		if v {
			return "Yes", true
		}
		return "No", true
	case string:
		if v == "" {
			return "", false
		}
		return v, true
	default:
		return fmt.Sprint(v), true
	}
}
