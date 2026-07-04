package extension

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// FieldType is the typed payload kind for an extension field.
type FieldType string

const (
	FieldTypeString   FieldType = "string"
	FieldTypeInt      FieldType = "int"
	FieldTypeBool     FieldType = "bool"
	FieldTypeEnum     FieldType = "enum"
	FieldTypeJSON     FieldType = "json"
	FieldTypeMoney    FieldType = "money"
	FieldTypeDate     FieldType = "date"
	FieldTypeDateTime FieldType = "datetime"
)

// IsValid reports whether t is a supported field type.
func (t FieldType) IsValid() bool {
	switch t {
	case FieldTypeString, FieldTypeInt, FieldTypeBool, FieldTypeEnum,
		FieldTypeJSON, FieldTypeMoney, FieldTypeDate, FieldTypeDateTime:
		return true
	}
	return false
}

// StorageMode declares how field values are persisted and propagated.
type StorageMode string

const (
	StorageStored   StorageMode = "stored"
	StorageComputed StorageMode = "computed"
	StorageSnapshot StorageMode = "snapshot"
)

// IsValid reports whether m is a supported storage mode.
func (m StorageMode) IsValid() bool {
	switch m {
	case StorageStored, StorageComputed, StorageSnapshot:
		return true
	}
	return false
}

// Visibility controls default admin and API exposure.
type Visibility string

const (
	VisibilityPublic  Visibility = "public"
	VisibilityPrivate Visibility = "private"
)

// IsValid reports whether v is a supported visibility value.
func (v Visibility) IsValid() bool {
	switch v {
	case VisibilityPublic, VisibilityPrivate:
		return true
	}
	return false
}

// Access declares role-based read/write constraints.
type Access struct {
	ReadRoles       []string
	WriteRoles      []string
	PluginOnlyWrite bool
}

// Validation holds static constraints applied at write time.
type Validation struct {
	Required bool
	Min      *int64
	Max      *int64
	Regex    string
	Options  []string
}

// EnumOptions builds validation constraints for an enum field.
func EnumOptions(options ...string) Validation {
	cp := make([]string, len(options))
	copy(cp, options)
	return Validation{Options: cp}
}

// FieldDef is the input shape for registering an extension field.
type FieldDef struct {
	Code        string
	Label       string
	Description string
	Type        FieldType
	Scope       TargetType
	StorageMode StorageMode
	Visibility  Visibility
	Access      Access
	Validation  Validation
}

// ExtensionField is a validated extension field definition.
type ExtensionField struct {
	Code        string
	Label       string
	Description string
	Type        FieldType
	Scope       TargetType
	StorageMode StorageMode
	Visibility  Visibility
	Access      Access
	Validation  Validation
}

var codePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$`)

// NewExtensionField validates def and returns a domain field definition.
func NewExtensionField(def FieldDef) (ExtensionField, error) {
	code := strings.TrimSpace(def.Code)
	if code == "" {
		return ExtensionField{}, errors.New("extension field code must not be empty")
	}
	if !codePattern.MatchString(code) {
		return ExtensionField{}, fmt.Errorf("extension field code %q must be namespaced (e.g. acme.gift.wrap_level)", code)
	}

	label := strings.TrimSpace(def.Label)
	if label == "" {
		return ExtensionField{}, errors.New("extension field label must not be empty")
	}

	if !def.Type.IsValid() {
		return ExtensionField{}, fmt.Errorf("extension field type %q is invalid", def.Type)
	}
	if !def.Scope.IsValid() {
		return ExtensionField{}, fmt.Errorf("extension field scope %q is invalid", def.Scope)
	}

	storage := def.StorageMode
	if storage == "" {
		if def.Scope.IsContext() {
			storage = StorageComputed
		} else {
			storage = StorageStored
		}
	}
	if !storage.IsValid() {
		return ExtensionField{}, fmt.Errorf("extension field storage_mode %q is invalid", storage)
	}

	switch storage {
	case StorageComputed:
		if !def.Scope.IsContext() {
			return ExtensionField{}, fmt.Errorf("extension field %q: storage_mode computed requires a context scope", code)
		}
	case StorageStored, StorageSnapshot:
		if !def.Scope.IsEntity() {
			return ExtensionField{}, fmt.Errorf("extension field %q: storage_mode %s requires an entity scope", code, storage)
		}
	}

	visibility := def.Visibility
	if visibility == "" {
		visibility = VisibilityPublic
	}
	if !visibility.IsValid() {
		return ExtensionField{}, fmt.Errorf("extension field visibility %q is invalid", visibility)
	}

	if def.Type == FieldTypeEnum {
		if len(def.Validation.Options) == 0 {
			return ExtensionField{}, fmt.Errorf("extension field %q: enum type requires validation options", code)
		}
		for i, opt := range def.Validation.Options {
			if strings.TrimSpace(opt) == "" {
				return ExtensionField{}, fmt.Errorf("extension field %q: enum option at index %d must not be empty", code, i)
			}
		}
	}

	validation := def.Validation
	if validation.Options != nil {
		opts := make([]string, len(validation.Options))
		copy(opts, validation.Options)
		validation.Options = opts
	}

	access := def.Access
	if access.ReadRoles != nil {
		roles := make([]string, len(access.ReadRoles))
		copy(roles, access.ReadRoles)
		access.ReadRoles = roles
	}
	if access.WriteRoles != nil {
		roles := make([]string, len(access.WriteRoles))
		copy(roles, access.WriteRoles)
		access.WriteRoles = roles
	}

	return ExtensionField{
		Code:        code,
		Label:       label,
		Description: strings.TrimSpace(def.Description),
		Type:        def.Type,
		Scope:       def.Scope,
		StorageMode: storage,
		Visibility:  visibility,
		Access:      access,
		Validation:  validation,
	}, nil
}
