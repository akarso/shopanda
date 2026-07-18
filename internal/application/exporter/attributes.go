package exporter

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	exportctx "github.com/akarso/shopanda/internal/application/exportctx"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/config"
)

// AttributeResult holds the summary of an attribute export run.
type AttributeResult struct {
	Entries   int
	Skipped   int
	Errors    []string
	RowErrors []exportctx.ExportError
}

// AttributeExporter writes attribute and group definitions to CSV.
type AttributeExporter struct {
	config   config.Repository
	rowHooks *RowHookRunner
}

// NewAttributeExporter creates an AttributeExporter.
func NewAttributeExporter(config config.Repository) *AttributeExporter {
	return &AttributeExporter{config: config}
}

// WithRowHooks wires export row hooks invoked before CSV write.
func (exp *AttributeExporter) WithRowHooks(registry *exportctx.Registry) *AttributeExporter {
	exp.rowHooks = NewRowHookRunner(registry)
	return exp
}

// Export writes all attribute definitions to w in CSV format.
//
// CSV columns: code, label, type, required, options, group, group_label.
// An attribute belonging to multiple groups produces one row per group.
func (exp *AttributeExporter) Export(ctx context.Context, w io.Writer) (*AttributeResult, error) {
	attrs, err := exp.loadAttributes(ctx)
	if err != nil {
		return nil, err
	}
	groups, err := exp.loadGroups(ctx)
	if err != nil {
		return nil, err
	}

	// Build reverse mapping: attribute code → [(groupCode, groupLabel)].
	type groupRef struct {
		code  string
		label string
	}
	attrGroups := make(map[string][]groupRef)
	for _, g := range groups {
		for _, ac := range g.Attributes {
			attrGroups[ac] = append(attrGroups[ac], groupRef{code: g.Code, label: g.Label})
		}
	}

	writer := csv.NewWriter(w)
	if err := writer.Write([]string{"code", "label", "type", "required", "options", "group", "group_label"}); err != nil {
		return nil, fmt.Errorf("attribute export: write header: %w", err)
	}

	// Sort attributes by code for deterministic output.
	sort.Slice(attrs, func(i, j int) bool { return attrs[i].Code < attrs[j].Code })

	result := &AttributeResult{}
	rowIndex := 0
	header := []string{"code", "label", "type", "required", "options", "group", "group_label"}
	for _, a := range attrs {
		reqStr := "false"
		if a.Required {
			reqStr = "true"
		}
		optStr := strings.Join(a.Options, ",")

		refs := attrGroups[a.Code]
		writeRow := func(groupCode, groupLabel string) error {
			rowIndex++
			rowMap := map[string]string{
				"code":        a.Code,
				"label":       a.Label,
				"type":        string(a.Type),
				"required":    reqStr,
				"options":     optStr,
				"group":       groupCode,
				"group_label": groupLabel,
			}
			if exp.rowHooks != nil && exp.rowHooks.Enabled() {
				var cont bool
				rowMap, cont = HandleRowHookOutcome(rowIndex, exp.rowHooks.Invoke(ctx, exportctx.EntityAttribute, rowIndex, rowMap), &result.Skipped, &result.Errors, &result.RowErrors)
				if !cont {
					return nil
				}
			}
			for k, v := range rowMap {
				rowMap[k] = sanitizeCSVCell(v)
			}
			if err := writer.Write(RowToRecord(header, rowMap)); err != nil {
				return err
			}
			result.Entries++
			return nil
		}

		if len(refs) == 0 {
			if err := writeRow("", ""); err != nil {
				return nil, fmt.Errorf("attribute export: write row: %w", err)
			}
		} else {
			for _, ref := range refs {
				if err := writeRow(ref.code, ref.label); err != nil {
					return nil, fmt.Errorf("attribute export: write row: %w", err)
				}
			}
		}
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, fmt.Errorf("attribute export: flush csv: %w", err)
	}
	return result, nil
}

// loadAttributes reads attribute definitions from the config store.
func (exp *AttributeExporter) loadAttributes(ctx context.Context) ([]catalog.Attribute, error) {
	val, err := exp.config.Get(ctx, "catalog.attributes")
	if err != nil {
		return nil, fmt.Errorf("attribute export: read attributes: %w", err)
	}
	if val == nil {
		return nil, nil
	}
	return decodeAttributes(val)
}

// loadGroups reads group definitions from the config store.
func (exp *AttributeExporter) loadGroups(ctx context.Context) ([]catalog.AttributeGroup, error) {
	val, err := exp.config.Get(ctx, "catalog.attribute_groups")
	if err != nil {
		return nil, fmt.Errorf("attribute export: read groups: %w", err)
	}
	if val == nil {
		return nil, nil
	}
	return decodeGroups(val)
}

// decodeAttributes converts a config value (interface{}) to []catalog.Attribute.
// The value is typically deserialized from JSONB as []interface{} of map[string]interface{}.
func decodeAttributes(val interface{}) ([]catalog.Attribute, error) {
	raw, err := json.Marshal(val)
	if err != nil {
		return nil, fmt.Errorf("attribute export: marshal config value: %w", err)
	}
	var attrs []catalog.Attribute
	if err := json.Unmarshal(raw, &attrs); err != nil {
		return nil, fmt.Errorf("attribute export: decode attributes: %w", err)
	}
	return attrs, nil
}

// decodeGroups converts a config value (interface{}) to []catalog.AttributeGroup.
func decodeGroups(val interface{}) ([]catalog.AttributeGroup, error) {
	raw, err := json.Marshal(val)
	if err != nil {
		return nil, fmt.Errorf("attribute export: marshal config value: %w", err)
	}
	var groups []catalog.AttributeGroup
	if err := json.Unmarshal(raw, &groups); err != nil {
		return nil, fmt.Errorf("attribute export: decode groups: %w", err)
	}
	return groups, nil
}
