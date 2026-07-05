package extension_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/domain/extension"
)

func TestPayloadFromInput_RejectsFractionalFloat64ForIntField(t *testing.T) {
	field, err := extension.NewExtensionField(extension.FieldDef{
		Code:        "acme.count",
		Label:       "Count",
		Type:        extension.FieldTypeInt,
		Scope:       extension.TargetProduct,
		StorageMode: extension.StorageStored,
	})
	if err != nil {
		t.Fatalf("NewExtensionField: %v", err)
	}

	_, err = extension.PayloadFromInput(field, 1.5)
	if err == nil || !extension.IsValidationError(err) {
		t.Fatalf("PayloadFromInput(1.5) err = %v, want validation error", err)
	}

	payload, err := extension.PayloadFromInput(field, float64(2))
	if err != nil {
		t.Fatalf("PayloadFromInput(2) err = %v", err)
	}
	if payload.IntValue == nil || *payload.IntValue != 2 {
		t.Fatalf("payload = %+v, want int 2", payload)
	}
}
