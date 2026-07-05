package extension_test

import (
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/domain/extension"
)

func validProductFieldDef() extension.FieldDef {
	return extension.FieldDef{
		Code:        "acme.gift.wrap_level",
		Label:       "Gift wrap",
		Type:        extension.FieldTypeEnum,
		Scope:       extension.TargetProduct,
		StorageMode: extension.StorageStored,
		Visibility:  extension.VisibilityPublic,
		Validation:  extension.EnumOptions("none", "standard", "premium"),
	}
}

func TestNewExtensionField_ValidProductField(t *testing.T) {
	field, err := extension.NewExtensionField(validProductFieldDef())
	if err != nil {
		t.Fatalf("NewExtensionField: %v", err)
	}
	if field.Code != "acme.gift.wrap_level" {
		t.Errorf("Code = %q", field.Code)
	}
	if field.StorageMode != extension.StorageStored {
		t.Errorf("StorageMode = %q, want stored", field.StorageMode)
	}
}

func TestNewExtensionField_RejectsUnnamespacedCode(t *testing.T) {
	def := validProductFieldDef()
	def.Code = "wrap_level"
	_, err := extension.NewExtensionField(def)
	if err == nil || !strings.Contains(err.Error(), "namespaced") {
		t.Fatalf("expected namespaced code error, got %v", err)
	}
}

func TestNewExtensionField_RejectsSingleSegmentCode(t *testing.T) {
	def := validProductFieldDef()
	def.Code = "acme"
	_, err := extension.NewExtensionField(def)
	if err == nil {
		t.Fatal("expected error for single-segment code")
	}
}

func TestNewExtensionField_RejectsInvalidType(t *testing.T) {
	def := validProductFieldDef()
	def.Type = "text"
	_, err := extension.NewExtensionField(def)
	if err == nil {
		t.Fatal("expected invalid type error")
	}
}

func TestNewExtensionField_RejectsInvalidScope(t *testing.T) {
	def := validProductFieldDef()
	def.Scope = "warehouse"
	_, err := extension.NewExtensionField(def)
	if err == nil {
		t.Fatal("expected invalid scope error")
	}
}

func TestNewExtensionField_ComputedRequiresContextScope(t *testing.T) {
	def := validProductFieldDef()
	def.StorageMode = extension.StorageComputed
	_, err := extension.NewExtensionField(def)
	if err == nil || !strings.Contains(err.Error(), "context scope") {
		t.Fatalf("expected context scope error, got %v", err)
	}
}

func TestNewExtensionField_ComputedDefaultsOnContextScope(t *testing.T) {
	field, err := extension.NewExtensionField(extension.FieldDef{
		Code:  "acme.pdp.badge",
		Label: "Badge",
		Type:  extension.FieldTypeString,
		Scope: extension.TargetPDP,
	})
	if err != nil {
		t.Fatalf("NewExtensionField: %v", err)
	}
	if field.StorageMode != extension.StorageComputed {
		t.Errorf("StorageMode = %q, want computed", field.StorageMode)
	}
}

func TestNewExtensionField_StoredRequiresEntityScope(t *testing.T) {
	_, err := extension.NewExtensionField(extension.FieldDef{
		Code:        "acme.pdp.note",
		Label:       "Note",
		Type:        extension.FieldTypeString,
		Scope:       extension.TargetPDP,
		StorageMode: extension.StorageStored,
	})
	if err == nil || !strings.Contains(err.Error(), "entity scope") {
		t.Fatalf("expected entity scope error, got %v", err)
	}
}

func TestNewExtensionField_EnumRequiresOptions(t *testing.T) {
	def := validProductFieldDef()
	def.Validation = extension.Validation{}
	_, err := extension.NewExtensionField(def)
	if err == nil || !strings.Contains(err.Error(), "enum") {
		t.Fatalf("expected enum options error, got %v", err)
	}
}

func TestNewExtensionField_DefaultVisibilityPublic(t *testing.T) {
	def := validProductFieldDef()
	def.Visibility = ""
	field, err := extension.NewExtensionField(def)
	if err != nil {
		t.Fatalf("NewExtensionField: %v", err)
	}
	if field.Visibility != extension.VisibilityPublic {
		t.Errorf("Visibility = %q, want public", field.Visibility)
	}
}

func TestNewExtensionField_ClonesValidationPointers(t *testing.T) {
	min := int64(1)
	max := int64(10)
	def := validProductFieldDef()
	def.Type = extension.FieldTypeInt
	def.Validation = extension.Validation{Min: &min, Max: &max}

	field, err := extension.NewExtensionField(def)
	if err != nil {
		t.Fatalf("NewExtensionField: %v", err)
	}

	*def.Validation.Min = 99
	*def.Validation.Max = 999

	if field.Validation.Min == nil || *field.Validation.Min != 1 {
		t.Errorf("Min = %v, want 1", field.Validation.Min)
	}
	if field.Validation.Max == nil || *field.Validation.Max != 10 {
		t.Errorf("Max = %v, want 10", field.Validation.Max)
	}
}

func TestTargetType_IsValid(t *testing.T) {
	if !extension.TargetCartItem.IsValid() {
		t.Error("cart_item should be valid")
	}
	if extension.TargetType("unknown").IsValid() {
		t.Error("unknown should be invalid")
	}
}
