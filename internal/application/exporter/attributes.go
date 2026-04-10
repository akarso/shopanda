package exporter

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/config"
)

// AttributeResult holds the summary of an attribute export run.
type AttributeResult struct {
	Entries int
}

// AttributeExporter writes attribute and group definitions to CSV.
type AttributeExporter struct {
	config config.Repository
}

// NewAttributeExporter creates an AttributeExporter.
func NewAttributeExporter(config config.Repository) *AttributeExporter {
	return &AttributeExporter{config: config}
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
	for _, a := range attrs {
		reqStr := "false"
		if a.Required {
			reqStr = "true"
		}
		optStr := strings.Join(a.Options, ",")

		safeOpt := sanitizeCSVCell(optStr)

		refs := attrGroups[a.Code]
		if len(refs) == 0 {
			row := []string{
				sanitizeCSVCell(a.Code),
				sanitizeCSVCell(a.Label),
				string(a.Type),
				reqStr,
				safeOpt,
				"",
				"",
			}
			if err := writer.Write(row); err != nil {
				return nil, fmt.Errorf("attribute export: write row: %w", err)
			}
			result.Entries++
		} else {
			for _, ref := range refs {
				row := []string{
					sanitizeCSVCell(a.Code),
					sanitizeCSVCell(a.Label),
					string(a.Type),
					reqStr,
					safeOpt,
					sanitizeCSVCell(ref.code),
					sanitizeCSVCell(ref.label),
				}
				if err := writer.Write(row); err != nil {
					return nil, fmt.Errorf("attribute export: write row: %w", err)
				}
				result.Entries++
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
