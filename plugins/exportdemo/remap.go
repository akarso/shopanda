package exportdemo

import (
	"strings"

	"github.com/akarso/shopanda/pkg/extapi"
)

type productRemapRule struct {
	from string
	to   string
}

var defaultProductExportRemapRules = []productRemapRule{
	{from: "sku", to: "matnr"},
	{from: "name", to: "maktx"},
	{from: "description", to: "maktx2"},
	{from: "slug", to: "ext_slug"},
}

// RemapProductRow maps core product export columns to SAP-style ERP columns.
func RemapProductRow(ctx *extapi.ExportRowContext) {
	if ctx == nil || ctx.Row == nil {
		return
	}
	for _, rule := range defaultProductExportRemapRules {
		fromVal := strings.TrimSpace(ctx.Row[rule.from])
		if fromVal == "" {
			continue
		}
		if strings.TrimSpace(ctx.Row[rule.to]) == "" {
			ctx.Row[rule.to] = fromVal
		}
		delete(ctx.Row, rule.from)
	}
}
