package theme_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/domain/theme"
)

func TestLoad_PartialNotPageTemplate(t *testing.T) {
	e, err := theme.Load("testdata/parent_partials")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if e.HasTemplate("_header") {
		t.Error("_header partial should not appear as a page template")
	}
	if e.HasTemplate("_footer") {
		t.Error("_footer partial should not appear as a page template")
	}
}

func TestLoad_ChildOverridesPartialOnly(t *testing.T) {
	e, err := theme.Load("testdata/child_partial_footer")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	var buf bytes.Buffer
	if err := e.Render(&buf, "product", struct{ Name string }{Name: "Widget"}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "PARENT-HEADER") {
		t.Fatalf("expected inherited parent header in output:\n%s", out)
	}
	if !strings.Contains(out, "CHILD-FOOTER") {
		t.Fatalf("expected child footer override in output:\n%s", out)
	}
	if strings.Contains(out, "PARENT-FOOTER") {
		t.Fatalf("parent footer should be overridden:\n%s", out)
	}
	if !strings.Contains(out, "<h1>Widget</h1>") {
		t.Fatalf("expected inherited page content:\n%s", out)
	}
}
