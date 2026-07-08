package theme_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/domain/theme"
)

func TestLoad_ChildInheritsParentTemplate(t *testing.T) {
	e, err := theme.Load("testdata/child_inherit_only")
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
	e, err := theme.Load("testdata/child_override")
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
	_, err := theme.Load("testdata/child_bad_parent")
	if err == nil {
		t.Fatal("expected error for missing parent theme")
	}
	if !strings.Contains(err.Error(), "parent") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestLoad_CircularParentChain(t *testing.T) {
	_, err := theme.Load("testdata/circular_a")
	if err == nil {
		t.Fatal("expected error for circular parent chain")
	}
	if !strings.Contains(err.Error(), "circular") {
		t.Fatalf("error = %q", err.Error())
	}
}
