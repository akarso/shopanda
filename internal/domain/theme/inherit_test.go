package theme_test

import (
	"bytes"
	"strings"
	"testing"

	themeapp "github.com/akarso/shopanda/internal/application/theme"
)

func TestLoad_ChildInheritsParentTemplate(t *testing.T) {
	e, err := themeapp.Load("testdata/child_inherit_only")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if e.Theme().Name != "childinherit" {
		t.Fatalf("theme name = %q, want childinherit", e.Theme().Name)
	}
	if !e.HasTemplate("listing") {
		t.Fatal("expected inherited listing template from parent")
	}

	var buf bytes.Buffer
	if err := e.Render(&buf, "listing", struct {
		Items []string
	}{Items: []string{"X"}}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(buf.String(), "<li>X</li>") {
		t.Fatalf("output = %q", buf.String())
	}
}

func TestLoad_ChildOverrideWinsOverParent(t *testing.T) {
	e, err := themeapp.Load("testdata/child_override")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	var buf bytes.Buffer
	if err := e.Render(&buf, "product", struct {
		Name        string
		Description string
	}{Name: "Widget", Description: "desc"}); err != nil {
		t.Fatalf("Render product: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "CHILD Widget") {
		t.Fatalf("child override missing in output:\n%s", out)
	}

	buf.Reset()
	if err := e.Render(&buf, "listing", struct {
		Items []string
	}{Items: []string{"A"}}); err != nil {
		t.Fatalf("Render listing: %v", err)
	}
	if !strings.Contains(buf.String(), "<li>A</li>") {
		t.Fatalf("inherited listing missing:\n%s", buf.String())
	}
}

func TestLoad_InvalidParent(t *testing.T) {
	_, err := themeapp.Load("testdata/child_bad_parent")
	if err == nil {
		t.Fatal("expected error for missing parent theme")
	}
	if !strings.Contains(err.Error(), "parent") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestLoad_CircularParentChain(t *testing.T) {
	_, err := themeapp.Load("testdata/circular_a")
	if err == nil {
		t.Fatal("expected error for circular parent chain")
	}
	if !strings.Contains(err.Error(), "circular") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestLoad_ParentLayoutOnlyInheritsPagesFromChild(t *testing.T) {
	e, err := themeapp.Load("testdata/child_layout_inherit")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	var buf bytes.Buffer
	if err := e.Render(&buf, "product", struct{}{}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "layout-only-parent") {
		t.Fatalf("expected inherited parent layout in output:\n%s", out)
	}
	if !strings.Contains(out, "<h1>PAGE</h1>") {
		t.Fatalf("expected child page content in output:\n%s", out)
	}
}

func TestLoad_RejectsAbsoluteParentPath(t *testing.T) {
	_, err := themeapp.Load("testdata/child_abs_parent")
	if err == nil {
		t.Fatal("expected error for absolute parent path")
	}
	if !strings.Contains(err.Error(), "relative path") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestLoad_RejectsParentOutsideBoundary(t *testing.T) {
	_, err := themeapp.Load("testdata/child_escape_parent")
	if err == nil {
		t.Fatal("expected error for parent outside theme boundary")
	}
	if !strings.Contains(err.Error(), "outside allowed theme boundary") {
		t.Fatalf("error = %q", err.Error())
	}
}
