package apperror_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/platform/apperror"
)

func TestWithClientCode_RequiresPluginPrefix(t *testing.T) {
	err := apperror.Validation("nope").WithClientCode("unauthorized")
	if err.ClientCode != "" {
		t.Fatalf("ClientCode = %q, want empty for reserved/platform code", err.ClientCode)
	}
	err = apperror.Validation("ok").WithClientCode("plugin.design_unavailable")
	if err.ClientCode != "plugin.design_unavailable" {
		t.Fatalf("ClientCode = %q", err.ClientCode)
	}
	err = apperror.Validation("empty").WithClientCode("plugin.")
	if err.ClientCode != "" {
		t.Fatalf("ClientCode = %q, want empty for bare prefix", err.ClientCode)
	}
}

func TestWithDetails_ShallowCopy(t *testing.T) {
	src := map[string]interface{}{"retryable": true}
	err := apperror.Validation("x").WithDetails(src)
	src["retryable"] = false
	if err.Details["retryable"] != true {
		t.Fatalf("details mutated via caller map: %#v", err.Details)
	}
}
