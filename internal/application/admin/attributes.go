package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/config"
	"github.com/akarso/shopanda/internal/domain/search"
)

const (
	configKeyAttributes      = "catalog.attributes"
	configKeyAttributeGroups = "catalog.attribute_groups"
)

// AttributeStore manages attribute and attribute-group definitions in config storage.
type AttributeStore struct {
	config config.Repository
	mu     sync.Mutex
}

type validationError struct {
	msg string
}

func (e *validationError) Error() string {
	return e.msg
}

func validationErr(format string, args ...interface{}) error {
	return &validationError{msg: fmt.Sprintf(format, args...)}
}

// IsValidationError reports whether err is a client-facing attribute validation error.
func IsValidationError(err error) bool {
	var ve *validationError
	return errors.As(err, &ve)
}

// NewAttributeStore creates an AttributeStore backed by the config repository.
func NewAttributeStore(cfg config.Repository) *AttributeStore {
	if cfg == nil {
		panic("AttributeStore: config repository must not be nil")
	}
	return &AttributeStore{config: cfg}
}

func decodeConfigAttributes(val interface{}) ([]catalog.Attribute, error) {
	if val == nil {
		return nil, nil
	}
	raw, err := json.Marshal(val)
	if err != nil {
		return nil, fmt.Errorf("decode attributes: marshal config value: %w", err)
	}
	var attrs []catalog.Attribute
	if err := json.Unmarshal(raw, &attrs); err != nil {
		return nil, fmt.Errorf("decode attributes: %w", err)
	}
	return attrs, nil
}

func decodeConfigGroups(val interface{}) ([]catalog.AttributeGroup, error) {
	if val == nil {
		return nil, nil
	}
	raw, err := json.Marshal(val)
	if err != nil {
		return nil, fmt.Errorf("decode attribute groups: marshal config value: %w", err)
	}
	var groups []catalog.AttributeGroup
	if err := json.Unmarshal(raw, &groups); err != nil {
		return nil, fmt.Errorf("decode attribute groups: %w", err)
	}
	return groups, nil
}

func (s *AttributeStore) loadAttributes(ctx context.Context) ([]catalog.Attribute, error) {
	val, err := s.config.Get(ctx, configKeyAttributes)
	if err != nil {
		return nil, fmt.Errorf("load attributes: %w", err)
	}
	return decodeConfigAttributes(val)
}

func (s *AttributeStore) loadGroups(ctx context.Context) ([]catalog.AttributeGroup, error) {
	val, err := s.config.Get(ctx, configKeyAttributeGroups)
	if err != nil {
		return nil, fmt.Errorf("load attribute groups: %w", err)
	}
	return decodeConfigGroups(val)
}

func (s *AttributeStore) saveAttributes(ctx context.Context, attrs []catalog.Attribute) error {
	sort.Slice(attrs, func(i, j int) bool { return attrs[i].Code < attrs[j].Code })
	if err := s.config.Set(ctx, configKeyAttributes, attrs); err != nil {
		return fmt.Errorf("save attributes: %w", err)
	}
	return nil
}

func (s *AttributeStore) saveGroups(ctx context.Context, groups []catalog.AttributeGroup) error {
	sort.Slice(groups, func(i, j int) bool { return groups[i].Code < groups[j].Code })
	if err := s.config.Set(ctx, configKeyAttributeGroups, groups); err != nil {
		return fmt.Errorf("save attribute groups: %w", err)
	}
	return nil
}

func (s *AttributeStore) saveAttributesAndGroups(ctx context.Context, attrs []catalog.Attribute, groups []catalog.AttributeGroup) error {
	sort.Slice(attrs, func(i, j int) bool { return attrs[i].Code < attrs[j].Code })
	sort.Slice(groups, func(i, j int) bool { return groups[i].Code < groups[j].Code })
	return s.config.SetMany(ctx, map[string]interface{}{
		configKeyAttributes:      attrs,
		configKeyAttributeGroups: groups,
	})
}

func validateAttributeDefinition(attr catalog.Attribute) error {
	code := strings.TrimSpace(attr.Code)
	label := strings.TrimSpace(attr.Label)
	if code == "" {
		return validationErr("attribute code must not be empty")
	}
	if label == "" {
		return validationErr("attribute label must not be empty")
	}
	if !attr.Type.IsValid() {
		return validationErr("attribute type is invalid")
	}
	if attr.Type == catalog.AttributeTypeSelect && len(attr.Options) == 0 {
		return validationErr("select attribute must have at least one option")
	}
	if search.ReservedFacetKey(code) {
		return validationErr("attribute code %q is reserved for category facets", code)
	}
	return nil
}

func normalizeAttribute(attr catalog.Attribute) catalog.Attribute {
	attr.Code = strings.TrimSpace(attr.Code)
	attr.Label = strings.TrimSpace(attr.Label)
	if attr.Options != nil {
		opts := make([]string, 0, len(attr.Options))
		for _, o := range attr.Options {
			o = strings.TrimSpace(o)
			if o != "" {
				opts = append(opts, o)
			}
		}
		attr.Options = opts
	}
	return attr
}

func normalizeGroup(group catalog.AttributeGroup) catalog.AttributeGroup {
	group.Code = strings.TrimSpace(group.Code)
	group.Label = strings.TrimSpace(group.Label)
	if group.Attributes == nil {
		group.Attributes = []string{}
	}
	cleaned := make([]string, 0, len(group.Attributes))
	seen := make(map[string]struct{}, len(group.Attributes))
	for _, code := range group.Attributes {
		code = strings.TrimSpace(code)
		if code == "" {
			continue
		}
		if _, ok := seen[code]; ok {
			continue
		}
		seen[code] = struct{}{}
		cleaned = append(cleaned, code)
	}
	group.Attributes = cleaned
	return group
}

func findAttributeIndex(attrs []catalog.Attribute, code string) int {
	for i := range attrs {
		if attrs[i].Code == code {
			return i
		}
	}
	return -1
}

func findGroupIndex(groups []catalog.AttributeGroup, code string) int {
	for i := range groups {
		if groups[i].Code == code {
			return i
		}
	}
	return -1
}

func validateGroupReferences(attrs []catalog.Attribute, group catalog.AttributeGroup) error {
	known := make(map[string]struct{}, len(attrs))
	for _, a := range attrs {
		known[a.Code] = struct{}{}
	}
	for _, code := range group.Attributes {
		if _, ok := known[code]; !ok {
			return validationErr("attribute %q not registered", code)
		}
	}
	return nil
}

// ListAttributes returns all attributes, optionally filtered by group code.
func (s *AttributeStore) ListAttributes(ctx context.Context, groupCode string) ([]catalog.Attribute, error) {
	attrs, err := s.loadAttributes(ctx)
	if err != nil {
		return nil, err
	}
	groupCode = strings.TrimSpace(groupCode)
	if groupCode == "" {
		sort.Slice(attrs, func(i, j int) bool { return attrs[i].Code < attrs[j].Code })
		return attrs, nil
	}
	groups, err := s.loadGroups(ctx)
	if err != nil {
		return nil, err
	}
	idx := findGroupIndex(groups, groupCode)
	if idx < 0 {
		return nil, fmt.Errorf("attribute group %q not found", groupCode)
	}
	allowed := make(map[string]struct{}, len(groups[idx].Attributes))
	for _, code := range groups[idx].Attributes {
		allowed[code] = struct{}{}
	}
	filtered := make([]catalog.Attribute, 0)
	for _, a := range attrs {
		if _, ok := allowed[a.Code]; ok {
			filtered = append(filtered, a)
		}
	}
	sort.Slice(filtered, func(i, j int) bool { return filtered[i].Code < filtered[j].Code })
	return filtered, nil
}

// GetAttribute returns the attribute with the given code.
func (s *AttributeStore) GetAttribute(ctx context.Context, code string) (catalog.Attribute, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return catalog.Attribute{}, validationErr("attribute code must not be empty")
	}
	attrs, err := s.loadAttributes(ctx)
	if err != nil {
		return catalog.Attribute{}, err
	}
	idx := findAttributeIndex(attrs, code)
	if idx < 0 {
		return catalog.Attribute{}, fmt.Errorf("attribute %q not found", code)
	}
	return attrs[idx], nil
}

// CreateAttribute adds a new attribute definition.
func (s *AttributeStore) CreateAttribute(ctx context.Context, attr catalog.Attribute) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	attr = normalizeAttribute(attr)
	if err := validateAttributeDefinition(attr); err != nil {
		return err
	}
	if _, err := catalog.NewAttribute(attr.Code, attr.Label, attr.Type); err != nil {
		return validationErr("%s", err.Error())
	}
	attrs, err := s.loadAttributes(ctx)
	if err != nil {
		return err
	}
	if findAttributeIndex(attrs, attr.Code) >= 0 {
		return fmt.Errorf("attribute code %q already exists", attr.Code)
	}
	attrs = append(attrs, attr)
	return s.saveAttributes(ctx, attrs)
}

// UpdateAttribute replaces an existing attribute definition.
func (s *AttributeStore) UpdateAttribute(ctx context.Context, code string, attr catalog.Attribute) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	code = strings.TrimSpace(code)
	if code == "" {
		return validationErr("attribute code must not be empty")
	}
	attr = normalizeAttribute(attr)
	attr.Code = code
	if err := validateAttributeDefinition(attr); err != nil {
		return err
	}
	if _, err := catalog.NewAttribute(attr.Code, attr.Label, attr.Type); err != nil {
		return validationErr("%s", err.Error())
	}
	attrs, err := s.loadAttributes(ctx)
	if err != nil {
		return err
	}
	idx := findAttributeIndex(attrs, code)
	if idx < 0 {
		return fmt.Errorf("attribute %q not found", code)
	}
	attrs[idx] = attr
	return s.saveAttributes(ctx, attrs)
}

// DeleteAttribute removes an attribute and drops it from all groups.
func (s *AttributeStore) DeleteAttribute(ctx context.Context, code string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	code = strings.TrimSpace(code)
	if code == "" {
		return validationErr("attribute code must not be empty")
	}
	attrs, err := s.loadAttributes(ctx)
	if err != nil {
		return err
	}
	idx := findAttributeIndex(attrs, code)
	if idx < 0 {
		return fmt.Errorf("attribute %q not found", code)
	}
	attrs = append(attrs[:idx], attrs[idx+1:]...)

	groups, err := s.loadGroups(ctx)
	if err != nil {
		return err
	}
	groupsChanged := false
	for i := range groups {
		if groups[i].HasAttribute(code) {
			groups[i].RemoveAttribute(code)
			groupsChanged = true
		}
	}
	if groupsChanged {
		return s.saveAttributesAndGroups(ctx, attrs, groups)
	}
	return s.saveAttributes(ctx, attrs)
}

// ListGroups returns all attribute groups.
func (s *AttributeStore) ListGroups(ctx context.Context) ([]catalog.AttributeGroup, error) {
	groups, err := s.loadGroups(ctx)
	if err != nil {
		return nil, err
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].Code < groups[j].Code })
	return groups, nil
}

// GetGroup returns the attribute group with the given code.
func (s *AttributeStore) GetGroup(ctx context.Context, code string) (catalog.AttributeGroup, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return catalog.AttributeGroup{}, validationErr("attribute group code must not be empty")
	}
	groups, err := s.loadGroups(ctx)
	if err != nil {
		return catalog.AttributeGroup{}, err
	}
	idx := findGroupIndex(groups, code)
	if idx < 0 {
		return catalog.AttributeGroup{}, fmt.Errorf("attribute group %q not found", code)
	}
	return groups[idx], nil
}

// CreateGroup adds a new attribute group.
func (s *AttributeStore) CreateGroup(ctx context.Context, group catalog.AttributeGroup) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	group = normalizeGroup(group)
	if group.Code == "" {
		return validationErr("attribute group code must not be empty")
	}
	if group.Label == "" {
		return validationErr("attribute group label must not be empty")
	}
	if _, err := catalog.NewAttributeGroup(group.Code, group.Label); err != nil {
		return validationErr("%s", err.Error())
	}
	attrs, err := s.loadAttributes(ctx)
	if err != nil {
		return err
	}
	if err := validateGroupReferences(attrs, group); err != nil {
		return err
	}
	groups, err := s.loadGroups(ctx)
	if err != nil {
		return err
	}
	if findGroupIndex(groups, group.Code) >= 0 {
		return fmt.Errorf("attribute group code %q already exists", group.Code)
	}
	groups = append(groups, group)
	return s.saveGroups(ctx, groups)
}

// UpdateGroup replaces an existing attribute group.
func (s *AttributeStore) UpdateGroup(ctx context.Context, code string, group catalog.AttributeGroup) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	code = strings.TrimSpace(code)
	if code == "" {
		return validationErr("attribute group code must not be empty")
	}
	group = normalizeGroup(group)
	group.Code = code
	if group.Label == "" {
		return validationErr("attribute group label must not be empty")
	}
	if _, err := catalog.NewAttributeGroup(group.Code, group.Label); err != nil {
		return validationErr("%s", err.Error())
	}
	attrs, err := s.loadAttributes(ctx)
	if err != nil {
		return err
	}
	if err := validateGroupReferences(attrs, group); err != nil {
		return err
	}
	groups, err := s.loadGroups(ctx)
	if err != nil {
		return err
	}
	idx := findGroupIndex(groups, code)
	if idx < 0 {
		return fmt.Errorf("attribute group %q not found", code)
	}
	groups[idx] = group
	return s.saveGroups(ctx, groups)
}

// DeleteGroup removes an attribute group definition.
func (s *AttributeStore) DeleteGroup(ctx context.Context, code string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	code = strings.TrimSpace(code)
	if code == "" {
		return validationErr("attribute group code must not be empty")
	}
	groups, err := s.loadGroups(ctx)
	if err != nil {
		return err
	}
	idx := findGroupIndex(groups, code)
	if idx < 0 {
		return fmt.Errorf("attribute group %q not found", code)
	}
	groups = append(groups[:idx], groups[idx+1:]...)
	return s.saveGroups(ctx, groups)
}

// GroupCodesForAttribute returns group codes that include the given attribute.
func (s *AttributeStore) GroupCodesForAttribute(ctx context.Context, attrCode string) ([]string, error) {
	groups, err := s.loadGroups(ctx)
	if err != nil {
		return nil, err
	}
	codes := make([]string, 0)
	for _, g := range groups {
		if g.HasAttribute(attrCode) {
			codes = append(codes, g.Code)
		}
	}
	sort.Strings(codes)
	return codes, nil
}

// ListLayeredNavAttributes returns attributes flagged for PLP layered navigation.
func (s *AttributeStore) ListLayeredNavAttributes(ctx context.Context) ([]catalog.Attribute, error) {
	attrs, err := s.loadAttributes(ctx)
	if err != nil {
		return nil, err
	}
	reg := catalog.NewAttributeRegistry()
	for _, attr := range attrs {
		reg.RegisterAttribute(attr)
	}
	layered := reg.AttributesForLayeredNav()
	sort.Slice(layered, func(i, j int) bool { return layered[i].Code < layered[j].Code })
	return layered, nil
}
