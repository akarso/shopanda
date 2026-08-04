package importer

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"sort"
	"strings"

	importctx "github.com/akarso/shopanda/internal/application/importctx"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/config"
)

// AttributeResult holds the summary of an attribute import run.
type AttributeResult struct {
	Attributes int
	Groups     int
	Skipped    int
	Errors     []string
	RowErrors  []importctx.ImportError
}

// AttributeImporter imports attribute and group definitions from CSV.
type AttributeImporter struct {
	config   config.Repository
	rowHooks *RowHookRunner
}

// NewAttributeImporter creates an AttributeImporter.
func NewAttributeImporter(config config.Repository) *AttributeImporter {
	return &AttributeImporter{config: config}
}

// WithRowHooks wires import row hooks invoked after header validation and before persist.
func (imp *AttributeImporter) WithRowHooks(registry *importctx.Registry) *AttributeImporter {
	imp.rowHooks = NewRowHookRunner(registry)
	return imp
}

// Import reads CSV rows from r and persists attribute definitions.
//
// Required columns: code, label, type.
// Optional columns: required, options, use_in_advanced_search, use_in_layered_nav,
// use_in_promo_rules, group, group_label.
//
// Each row defines one attribute. When group and group_label are present,
// the attribute is also added to the named group.
func (imp *AttributeImporter) Import(ctx context.Context, r io.Reader) (*AttributeResult, error) {
	reader := csv.NewReader(r)
	reader.TrimLeadingSpace = true
	reader.FieldsPerRecord = -1 // allow rows with fewer fields than header

	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("attribute import: read header: %w", err)
	}

	colIdx := make(map[string]int, len(header))
	for i, h := range header {
		colIdx[strings.TrimSpace(strings.ToLower(h))] = i
	}

	_, hasCode := colIdx["code"]
	if !hasCode {
		return nil, fmt.Errorf("attribute import: CSV must have 'code' column")
	}
	_, hasLabel := colIdx["label"]
	if !hasLabel {
		return nil, fmt.Errorf("attribute import: CSV must have 'label' column")
	}
	_, hasType := colIdx["type"]
	if !hasType {
		return nil, fmt.Errorf("attribute import: CSV must have 'type' column")
	}

	_, hasRequired := colIdx["required"]
	_, hasOptions := colIdx["options"]
	_, hasAdvancedSearch := colIdx["use_in_advanced_search"]
	_, hasLayeredNav := colIdx["use_in_layered_nav"]
	_, hasPromoRules := colIdx["use_in_promo_rules"]
	_, hasGroup := colIdx["group"]
	_, hasGroupLabel := colIdx["group_label"]

	// Collect attributes and group memberships.
	attrs := make(map[string]catalog.Attribute) // code → Attribute
	type groupInfo struct {
		label string
		attrs []string // ordered attribute codes
	}
	groups := make(map[string]*groupInfo) // group code → info

	result := &AttributeResult{}
	lineNum := 1 // header is line 1

	for {
		lineNum++
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: %v", lineNum, err))
			result.Skipped++
			continue
		}

		rowMap := RecordToRow(record, colIdx)
		if imp.rowHooks != nil {
			var cont bool
			rowMap, cont = HandleRowHookOutcome(lineNum, imp.rowHooks.Invoke(ctx, importctx.EntityAttribute, lineNum, rowMap), &result.Skipped, &result.Errors, &result.RowErrors)
			if !cont {
				continue
			}
		}

		code := colValRow(rowMap, "code")
		if code == "" {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: empty code", lineNum))
			result.Skipped++
			continue
		}

		label := colValRow(rowMap, "label")
		if label == "" {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: empty label", lineNum))
			result.Skipped++
			continue
		}

		attrType := catalog.AttributeType(strings.ToLower(colValRow(rowMap, "type")))
		if !attrType.IsValid() {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: invalid type %q", lineNum, rowMap["type"]))
			result.Skipped++
			continue
		}

		attr, err := catalog.NewAttribute(code, label, attrType)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: %v", lineNum, err))
			result.Skipped++
			continue
		}

		if hasRequired {
			attr.Required = parseAttributeCSVBool(colValRow(rowMap, "required"))
		}

		if hasAdvancedSearch {
			attr.UseInAdvancedSearch = parseAttributeCSVBool(colValRow(rowMap, "use_in_advanced_search"))
		}
		if hasLayeredNav {
			attr.UseInLayeredNav = parseAttributeCSVBool(colValRow(rowMap, "use_in_layered_nav"))
		}
		if hasPromoRules {
			attr.UseInPromoRules = parseAttributeCSVBool(colValRow(rowMap, "use_in_promo_rules"))
		}

		if hasOptions {
			raw := colValRow(rowMap, "options")
			if raw != "" {
				opts := strings.Split(raw, ",")
				cleaned := make([]string, 0, len(opts))
				for _, o := range opts {
					o = strings.TrimSpace(o)
					if o != "" {
						cleaned = append(cleaned, o)
					}
				}
				attr.Options = cleaned
			}
		}

		attrs[code] = attr

		// Collect group membership.
		if hasGroup {
			gc := colValRow(rowMap, "group")
			if gc != "" {
				gi, exists := groups[gc]
				if !exists {
					gl := gc // default label = code
					if hasGroupLabel {
						v := colValRow(rowMap, "group_label")
						if v != "" {
							gl = v
						}
					}
					gi = &groupInfo{label: gl}
					groups[gc] = gi
				} else if hasGroupLabel {
					v := colValRow(rowMap, "group_label")
					if v != "" {
						gi.label = v
					}
				}
				// Add attribute to group if not already present.
				found := false
				for _, a := range gi.attrs {
					if a == code {
						found = true
						break
					}
				}
				if !found {
					gi.attrs = append(gi.attrs, code)
				}
			}
		}
	}

	// Build domain objects.
	attrSlice := make([]catalog.Attribute, 0, len(attrs))
	for _, a := range attrs {
		attrSlice = append(attrSlice, a)
	}
	sort.Slice(attrSlice, func(i, j int) bool { return attrSlice[i].Code < attrSlice[j].Code })

	groupSlice := make([]catalog.AttributeGroup, 0, len(groups))
	for code, gi := range groups {
		g, err := catalog.NewAttributeGroup(code, gi.label)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("group %q: %v", code, err))
			result.Skipped++
			continue
		}
		g.Attributes = gi.attrs
		groupSlice = append(groupSlice, g)
	}
	sort.Slice(groupSlice, func(i, j int) bool { return groupSlice[i].Code < groupSlice[j].Code })

	// Validate groups reference only known attributes.
	registry := catalog.NewAttributeRegistry()
	for _, a := range attrSlice {
		registry.RegisterAttribute(a)
	}
	validGroups := make([]catalog.AttributeGroup, 0, len(groupSlice))
	for _, g := range groupSlice {
		if err := registry.RegisterGroup(g); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("group %q: %v", g.Code, err))
			result.Skipped++
			continue
		}
		validGroups = append(validGroups, g)
	}

	// Persist to config store.
	if err := imp.config.Set(ctx, "catalog.attributes", attrSlice); err != nil {
		return nil, fmt.Errorf("attribute import: persist attributes: %w", err)
	}
	if err := imp.config.Set(ctx, "catalog.attribute_groups", validGroups); err != nil {
		return nil, fmt.Errorf("attribute import: persist groups: %w", err)
	}

	result.Attributes = len(attrSlice)
	result.Groups = len(validGroups)
	return result, nil
}

func parseAttributeCSVBool(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "true", "1", "yes":
		return true
	default:
		return false
	}
}
