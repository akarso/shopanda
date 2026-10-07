package theme

import (
	"html/template"
	"strings"
	"testing"
)

func TestFragmentFuncMap_StubWhenNotConfigured(t *testing.T) {
	fn := fragmentFuncMap(nil)["fragment"].(FragmentFunc)
	got := string(fn("csrf"))
	if !strings.Contains(got, "not configured") {
		t.Fatalf("nil fragment helper = %q", got)
	}
}

func TestFragmentFuncMap_UsesInjectedHelper(t *testing.T) {
	var called string
	fn := fragmentFuncMap(func(args ...interface{}) template.HTML {
		called = args[0].(string)
		return template.HTML(`<span hx-get="/fragments/csrf"></span>`)
	})["fragment"].(FragmentFunc)
	got := string(fn("csrf"))
	if called != "csrf" {
		t.Fatalf("called with %q", called)
	}
	if !strings.Contains(got, `hx-get="/fragments/csrf"`) {
		t.Fatalf("injected helper = %q", got)
	}
}
