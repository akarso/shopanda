package importdemo

import (
	"strings"
	"unicode"

	"github.com/akarso/shopanda/pkg/extapi"
)

const (
	// ValidationCodeMissingSKU is returned when ERP material number is absent after remap.
	ValidationCodeMissingSKU = "importdemo.missing_sku"
)

type productRemapRule struct {
	from string
	to   string
}

var defaultProductRemapRules = []productRemapRule{
	{from: "matnr", to: "sku"},
	{from: "maktx", to: "name"},
	{from: "maktx2", to: "description"},
	{from: "ext_slug", to: "slug"},
}

// RemapProductRow maps SAP-style ERP columns to core product import columns.
func RemapProductRow(ctx *extapi.ImportRowContext) {
	if ctx == nil || ctx.Row == nil {
		return
	}
	for _, rule := range defaultProductRemapRules {
		fromVal := strings.TrimSpace(ctx.Row[rule.from])
		if fromVal == "" {
			continue
		}
		if strings.TrimSpace(ctx.Row[rule.to]) == "" {
			ctx.Row[rule.to] = fromVal
		}
		delete(ctx.Row, rule.from)
	}
	if strings.TrimSpace(ctx.Row["slug"]) == "" {
		if sku := strings.TrimSpace(ctx.Row["sku"]); sku != "" {
			ctx.Row["slug"] = slugFromSKU(sku)
		}
	}
	if strings.TrimSpace(ctx.Row["sku"]) == "" {
		ctx.AppendError(ValidationCodeMissingSKU, "MATNR (sku) is required")
	}
}

func slugFromSKU(sku string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(sku)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}
