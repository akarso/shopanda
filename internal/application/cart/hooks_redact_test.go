package cart

import (
	"testing"

	domainext "github.com/akarso/shopanda/internal/domain/extension"
)

type stubPluginOnly struct {
	codes  map[string]bool
	owners map[string]string
}

func (s stubPluginOnly) IsPluginOnlyWrite(code string) bool { return s.codes[code] }
func (s stubPluginOnly) FieldOwner(code string) string      { return s.owners[code] }

func TestExtensionInputsForHook_RedactsPluginOnlyUnlessTrusted(t *testing.T) {
	inputs := []domainext.ValueInput{
		{FieldCode: "acme.locked", Value: "secret"},
		{FieldCode: "acme.public", Value: "open"},
	}
	checker := stubPluginOnly{codes: map[string]bool{"acme.locked": true}}

	untrusted := extensionInputsForHook(inputs, nil, "plugin.other", checker)
	if len(untrusted) != 2 {
		t.Fatalf("len = %d", len(untrusted))
	}
	if _, ok := untrusted[0]["value"]; ok {
		t.Fatal("plugin-only value must be redacted without trust")
	}
	if untrusted[1]["value"] != "open" {
		t.Fatalf("public value = %v", untrusted[1]["value"])
	}

	// Unowned plugin-only (empty FieldOwner): redacted for every handler,
	// even when the write path trusted the code.
	trusted := extensionInputsForHook(inputs, []string{"acme.locked"}, "plugin.other", checker)
	if _, ok := trusted[0]["value"]; ok {
		t.Fatal("unowned plugin-only value must stay redacted for every handler")
	}
}

func TestPluginWriteCodesForRegistrant_FiltersUnownedAndNonOwner(t *testing.T) {
	checker := stubPluginOnly{
		codes:  map[string]bool{"acme.locked": true, "acme.unowned": true, "acme.public": false},
		owners: map[string]string{"acme.locked": "acme"},
	}
	trusted := []string{"acme.locked", "acme.unowned", "acme.open"}

	owner := pluginWriteCodesForRegistrant(trusted, "acme", checker)
	if len(owner) != 2 || owner[0] != "acme.locked" || owner[1] != "acme.open" {
		t.Fatalf("owner codes = %v, want [acme.locked acme.open]", owner)
	}
	other := pluginWriteCodesForRegistrant(trusted, "other", checker)
	if len(other) != 1 || other[0] != "acme.open" {
		t.Fatalf("other codes = %v, want [acme.open]", other)
	}
}

func TestExtensionInputsForHook_OwnedFieldOnlyVisibleToOwner(t *testing.T) {
	inputs := []domainext.ValueInput{
		{FieldCode: "acme.locked", Value: "secret"},
	}
	checker := stubPluginOnly{
		codes:  map[string]bool{"acme.locked": true},
		owners: map[string]string{"acme.locked": "acme"},
	}

	// Trusted write path (plugin_write_codes) but a different registrant's
	// handler must still not see the value.
	other := extensionInputsForHook(inputs, []string{"acme.locked"}, "other-plugin", checker)
	if _, ok := other[0]["value"]; ok {
		t.Fatal("owned plugin-only value must be redacted for a non-owner registrant")
	}
	if other[0]["field_code"] != "acme.locked" {
		t.Fatalf("field_code = %v, want always present", other[0]["field_code"])
	}

	owner := extensionInputsForHook(inputs, []string{"acme.locked"}, "acme", checker)
	if owner[0]["value"] != "secret" {
		t.Fatalf("owner value = %v, want secret", owner[0]["value"])
	}

	// Without the write trust at all, even the owner does not see it.
	untrusted := extensionInputsForHook(inputs, nil, "acme", checker)
	if _, ok := untrusted[0]["value"]; ok {
		t.Fatal("owned plugin-only value must still require write trust")
	}
}
