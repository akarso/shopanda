package catalog_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/domain/catalog"
)

// --- Attribute ---

func TestNewAttribute_OK(t *testing.T) {
	a, err := catalog.NewAttribute("color", "Color", catalog.AttributeTypeText)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.Code != "color" {
		t.Errorf("code = %q, want %q", a.Code, "color")
	}
	if a.Label != "Color" {
		t.Errorf("label = %q, want %q", a.Label, "Color")
	}
	if a.Type != catalog.AttributeTypeText {
		t.Errorf("type = %q, want %q", a.Type, catalog.AttributeTypeText)
	}
}

func TestNewAttribute_EmptyCode(t *testing.T) {
	_, err := catalog.NewAttribute("", "Color", catalog.AttributeTypeText)
	if err == nil {
		t.Fatal("expected error for empty code")
	}
}

func TestNewAttribute_EmptyLabel(t *testing.T) {
	_, err := catalog.NewAttribute("color", "", catalog.AttributeTypeText)
	if err == nil {
		t.Fatal("expected error for empty label")
	}
}

func TestNewAttribute_InvalidType(t *testing.T) {
	_, err := catalog.NewAttribute("color", "Color", "invalid")
	if err == nil {
		t.Fatal("expected error for invalid type")
	}
}

func TestAttributeType_IsValid(t *testing.T) {
	valid := []catalog.AttributeType{
		catalog.AttributeTypeText,
		catalog.AttributeTypeNumber,
		catalog.AttributeTypeBoolean,
		catalog.AttributeTypeSelect,
	}
	for _, v := range valid {
		if !v.IsValid() {
			t.Errorf("%q should be valid", v)
		}
	}
	if catalog.AttributeType("unknown").IsValid() {
		t.Error("unknown should be invalid")
	}
}

func TestAttribute_Validate_Text(t *testing.T) {
	a, _ := catalog.NewAttribute("name", "Name", catalog.AttributeTypeText)

	if err := a.Validate("hello"); err != nil {
		t.Errorf("valid string: %v", err)
	}
	if err := a.Validate(42); err == nil {
		t.Error("expected error for non-string value")
	}
}

func TestAttribute_Validate_Number(t *testing.T) {
	a, _ := catalog.NewAttribute("weight", "Weight", catalog.AttributeTypeNumber)

	if err := a.Validate(42); err != nil {
		t.Errorf("valid int: %v", err)
	}
	if err := a.Validate(3.14); err != nil {
		t.Errorf("valid float64: %v", err)
	}
	if err := a.Validate(int64(99)); err != nil {
		t.Errorf("valid int64: %v", err)
	}
	if err := a.Validate("not a number"); err == nil {
		t.Error("expected error for string value")
	}
}

func TestAttribute_Validate_Boolean(t *testing.T) {
	a, _ := catalog.NewAttribute("active", "Active", catalog.AttributeTypeBoolean)

	if err := a.Validate(true); err != nil {
		t.Errorf("valid bool: %v", err)
	}
	if err := a.Validate("yes"); err == nil {
		t.Error("expected error for non-boolean value")
	}
}

func TestAttribute_Validate_Select(t *testing.T) {
	a, _ := catalog.NewAttribute("color", "Color", catalog.AttributeTypeSelect)
	a.Options = []string{"red", "blue", "green"}

	if err := a.Validate("red"); err != nil {
		t.Errorf("valid option: %v", err)
	}
	if err := a.Validate("yellow"); err == nil {
		t.Error("expected error for invalid option")
	}
	if err := a.Validate(123); err == nil {
		t.Error("expected error for non-string value")
	}
}

func TestAttribute_Validate_Required(t *testing.T) {
	a, _ := catalog.NewAttribute("name", "Name", catalog.AttributeTypeText)
	a.Required = true

	if err := a.Validate(nil); err == nil {
		t.Error("expected error for nil value on required attribute")
	}
}

func TestAttribute_Validate_OptionalNil(t *testing.T) {
	a, _ := catalog.NewAttribute("note", "Note", catalog.AttributeTypeText)

	if err := a.Validate(nil); err != nil {
		t.Errorf("nil on optional should be ok: %v", err)
	}
}

// --- AttributeGroup ---

func TestNewAttributeGroup_OK(t *testing.T) {
	g, err := catalog.NewAttributeGroup("physical", "Physical")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if g.Code != "physical" {
		t.Errorf("code = %q, want %q", g.Code, "physical")
	}
	if len(g.Attributes) != 0 {
		t.Errorf("attributes should be empty, got %d", len(g.Attributes))
	}
}

func TestNewAttributeGroup_EmptyCode(t *testing.T) {
	_, err := catalog.NewAttributeGroup("", "Physical")
	if err == nil {
		t.Fatal("expected error for empty code")
	}
}

func TestNewAttributeGroup_EmptyLabel(t *testing.T) {
	_, err := catalog.NewAttributeGroup("physical", "")
	if err == nil {
		t.Fatal("expected error for empty label")
	}
}

func TestAttributeGroup_AddRemove(t *testing.T) {
	g, _ := catalog.NewAttributeGroup("apparel", "Apparel")

	g.AddAttribute("color")
	g.AddAttribute("size")
	if len(g.Attributes) != 2 {
		t.Fatalf("attributes len = %d, want 2", len(g.Attributes))
	}

	// duplicate add is no-op
	g.AddAttribute("color")
	if len(g.Attributes) != 2 {
		t.Errorf("duplicate add should be no-op, got %d", len(g.Attributes))
	}

	if !g.HasAttribute("color") {
		t.Error("expected HasAttribute(color) = true")
	}
	if g.HasAttribute("weight") {
		t.Error("expected HasAttribute(weight) = false")
	}

	g.RemoveAttribute("color")
	if g.HasAttribute("color") {
		t.Error("color should be removed")
	}
	if len(g.Attributes) != 1 {
		t.Errorf("attributes len = %d, want 1", len(g.Attributes))
	}

	// remove non-existent is no-op
	g.RemoveAttribute("nonexistent")
	if len(g.Attributes) != 1 {
		t.Errorf("remove non-existent should be no-op, got %d", len(g.Attributes))
	}
}

// --- AttributeRegistry ---

func TestAttributeRegistry_RegisterAndRetrieve(t *testing.T) {
	reg := catalog.NewAttributeRegistry()

	a, _ := catalog.NewAttribute("color", "Color", catalog.AttributeTypeSelect)
	a.Options = []string{"red", "blue"}
	reg.RegisterAttribute(a)

	got, ok := reg.Attribute("color")
	if !ok {
		t.Fatal("expected attribute to be found")
	}
	if got.Code != "color" {
		t.Errorf("code = %q, want %q", got.Code, "color")
	}
	if len(got.Options) != 2 {
		t.Errorf("options len = %d, want 2", len(got.Options))
	}

	// retrieval returns deep copy
	got.Options[0] = "mutated"
	original, _ := reg.Attribute("color")
	if original.Options[0] != "red" {
		t.Errorf("deep copy broken: got %q, want %q", original.Options[0], "red")
	}
}

func TestAttributeRegistry_NotFound(t *testing.T) {
	reg := catalog.NewAttributeRegistry()
	_, ok := reg.Attribute("missing")
	if ok {
		t.Error("expected not found")
	}
}

func TestAttributeRegistry_Attributes(t *testing.T) {
	reg := catalog.NewAttributeRegistry()
	a1, _ := catalog.NewAttribute("color", "Color", catalog.AttributeTypeText)
	a2, _ := catalog.NewAttribute("size", "Size", catalog.AttributeTypeText)
	reg.RegisterAttribute(a1)
	reg.RegisterAttribute(a2)

	all := reg.Attributes()
	if len(all) != 2 {
		t.Fatalf("attributes len = %d, want 2", len(all))
	}
}

func TestAttributeRegistry_RegisterGroup_OK(t *testing.T) {
	reg := catalog.NewAttributeRegistry()
	a, _ := catalog.NewAttribute("color", "Color", catalog.AttributeTypeText)
	reg.RegisterAttribute(a)

	g, _ := catalog.NewAttributeGroup("apparel", "Apparel")
	g.AddAttribute("color")

	if err := reg.RegisterGroup(g); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, ok := reg.Group("apparel")
	if !ok {
		t.Fatal("expected group to be found")
	}
	if len(got.Attributes) != 1 {
		t.Errorf("group attributes len = %d, want 1", len(got.Attributes))
	}
}

func TestAttributeRegistry_RegisterGroup_UnknownAttribute(t *testing.T) {
	reg := catalog.NewAttributeRegistry()

	g, _ := catalog.NewAttributeGroup("bad", "Bad Group")
	g.AddAttribute("nonexistent")

	if err := reg.RegisterGroup(g); err == nil {
		t.Fatal("expected error for unknown attribute")
	}
}

func TestAttributeRegistry_GroupNotFound(t *testing.T) {
	reg := catalog.NewAttributeRegistry()
	_, ok := reg.Group("missing")
	if ok {
		t.Error("expected not found")
	}
}

func TestAttributeRegistry_Groups(t *testing.T) {
	reg := catalog.NewAttributeRegistry()
	a, _ := catalog.NewAttribute("weight", "Weight", catalog.AttributeTypeNumber)
	reg.RegisterAttribute(a)

	g, _ := catalog.NewAttributeGroup("physical", "Physical")
	g.AddAttribute("weight")
	_ = reg.RegisterGroup(g)

	all := reg.Groups()
	if len(all) != 1 {
		t.Fatalf("groups len = %d, want 1", len(all))
	}
}

func TestAttributeRegistry_GroupAttributes(t *testing.T) {
	reg := catalog.NewAttributeRegistry()
	a1, _ := catalog.NewAttribute("color", "Color", catalog.AttributeTypeText)
	a2, _ := catalog.NewAttribute("size", "Size", catalog.AttributeTypeSelect)
	a2.Options = []string{"S", "M", "L"}
	reg.RegisterAttribute(a1)
	reg.RegisterAttribute(a2)

	g, _ := catalog.NewAttributeGroup("apparel", "Apparel")
	g.AddAttribute("color")
	g.AddAttribute("size")
	_ = reg.RegisterGroup(g)

	attrs, err := reg.GroupAttributes("apparel")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(attrs) != 2 {
		t.Fatalf("attributes len = %d, want 2", len(attrs))
	}
}

func TestAttributeRegistry_GroupAttributes_NotFound(t *testing.T) {
	reg := catalog.NewAttributeRegistry()
	_, err := reg.GroupAttributes("missing")
	if err == nil {
		t.Fatal("expected error for missing group")
	}
}

func TestAttributeRegistry_ValidateAttributes_OK(t *testing.T) {
	reg := catalog.NewAttributeRegistry()
	a1, _ := catalog.NewAttribute("color", "Color", catalog.AttributeTypeText)
	a2, _ := catalog.NewAttribute("weight", "Weight", catalog.AttributeTypeNumber)
	reg.RegisterAttribute(a1)
	reg.RegisterAttribute(a2)

	g, _ := catalog.NewAttributeGroup("physical", "Physical")
	g.AddAttribute("color")
	g.AddAttribute("weight")
	_ = reg.RegisterGroup(g)

	errs := reg.ValidateAttributes("physical", map[string]interface{}{
		"color":  "red",
		"weight": 42,
	})
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
}

func TestAttributeRegistry_ValidateAttributes_TypeMismatch(t *testing.T) {
	reg := catalog.NewAttributeRegistry()
	a, _ := catalog.NewAttribute("weight", "Weight", catalog.AttributeTypeNumber)
	a.Required = true
	reg.RegisterAttribute(a)

	g, _ := catalog.NewAttributeGroup("physical", "Physical")
	g.AddAttribute("weight")
	_ = reg.RegisterGroup(g)

	errs := reg.ValidateAttributes("physical", map[string]interface{}{
		"weight": "not a number",
	})
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
}

func TestAttributeRegistry_ValidateAttributes_Required(t *testing.T) {
	reg := catalog.NewAttributeRegistry()
	a, _ := catalog.NewAttribute("name", "Name", catalog.AttributeTypeText)
	a.Required = true
	reg.RegisterAttribute(a)

	g, _ := catalog.NewAttributeGroup("basic", "Basic")
	g.AddAttribute("name")
	_ = reg.RegisterGroup(g)

	errs := reg.ValidateAttributes("basic", map[string]interface{}{})
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
}

func TestAttributeRegistry_ValidateAttributes_UnknownGroup(t *testing.T) {
	reg := catalog.NewAttributeRegistry()
	errs := reg.ValidateAttributes("missing", map[string]interface{}{})
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errs))
	}
}

func TestAttributeRegistry_DeepCopyGroup(t *testing.T) {
	reg := catalog.NewAttributeRegistry()
	a, _ := catalog.NewAttribute("color", "Color", catalog.AttributeTypeText)
	reg.RegisterAttribute(a)

	g, _ := catalog.NewAttributeGroup("apparel", "Apparel")
	g.AddAttribute("color")
	_ = reg.RegisterGroup(g)

	got, _ := reg.Group("apparel")
	got.Attributes = append(got.Attributes, "mutated")

	original, _ := reg.Group("apparel")
	if len(original.Attributes) != 1 {
		t.Errorf("deep copy broken: group has %d attributes, want 1", len(original.Attributes))
	}
}

func TestAttributeRegistry_ReplaceAttribute(t *testing.T) {
	reg := catalog.NewAttributeRegistry()
	a, _ := catalog.NewAttribute("color", "Color", catalog.AttributeTypeText)
	reg.RegisterAttribute(a)

	// Replace with different label
	a2, _ := catalog.NewAttribute("color", "Colour", catalog.AttributeTypeText)
	reg.RegisterAttribute(a2)

	got, _ := reg.Attribute("color")
	if got.Label != "Colour" {
		t.Errorf("label = %q, want %q", got.Label, "Colour")
	}
}

func TestAttribute_Validate_UnsupportedType(t *testing.T) {
	a := catalog.Attribute{Code: "bad", Label: "Bad", Type: "unknown"}
	if err := a.Validate("anything"); err == nil {
		t.Error("expected error for unsupported type")
	}
}

func TestAttribute_Validate_SelectEmptyOptions(t *testing.T) {
	a, _ := catalog.NewAttribute("color", "Color", catalog.AttributeTypeSelect)
	// Options left empty → should error
	if err := a.Validate("red"); err == nil {
		t.Error("expected error for select with no options defined")
	}
}

func TestAttributeRegistry_ValidateAttributes_UndeclaredKey(t *testing.T) {
	reg := catalog.NewAttributeRegistry()
	a, _ := catalog.NewAttribute("color", "Color", catalog.AttributeTypeText)
	reg.RegisterAttribute(a)

	g, _ := catalog.NewAttributeGroup("basic", "Basic")
	g.AddAttribute("color")
	_ = reg.RegisterGroup(g)

	errs := reg.ValidateAttributes("basic", map[string]interface{}{
		"color":   "red",
		"unknown": "val",
	})
	if len(errs) != 1 {
		t.Fatalf("expected 1 error for undeclared key, got %d: %v", len(errs), errs)
	}
}

func TestAttributeRegistry_DeepCopyAttribute(t *testing.T) {
	reg := catalog.NewAttributeRegistry()
	a, _ := catalog.NewAttribute("color", "Color", catalog.AttributeTypeSelect)
	a.Options = []string{"red", "blue"}
	reg.RegisterAttribute(a)

	got, _ := reg.Attribute("color")
	got.Options = append(got.Options, "mutated")

	original, _ := reg.Attribute("color")
	if len(original.Options) != 2 {
		t.Errorf("deep copy broken: options has %d items, want 2", len(original.Options))
	}
}

func TestAttributeRegistry_CloneZeroLengthSlice(t *testing.T) {
	reg := catalog.NewAttributeRegistry()
	a, _ := catalog.NewAttribute("name", "Name", catalog.AttributeTypeText)
	// Options is nil by default from constructor
	reg.RegisterAttribute(a)

	got, _ := reg.Attribute("name")
	if got.Options != nil {
		t.Error("expected nil Options for text attribute")
	}

	// Now test with zero-length-but-non-nil
	a2, _ := catalog.NewAttribute("tag", "Tag", catalog.AttributeTypeText)
	a2.Options = []string{}
	reg.RegisterAttribute(a2)

	got2, _ := reg.Attribute("tag")
	if got2.Options == nil {
		t.Error("expected non-nil zero-length Options to be preserved")
	}
	if len(got2.Options) != 0 {
		t.Errorf("expected empty options, got %d", len(got2.Options))
	}
}
