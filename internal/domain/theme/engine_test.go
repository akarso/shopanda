package theme_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/domain/theme"
)

func TestLoad_Valid(t *testing.T) {
	e, err := theme.Load("testdata/valid")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if e.Theme().Name != "testtheme" {
		t.Errorf("theme name = %q, want %q", e.Theme().Name, "testtheme")
	}
	if e.Theme().Version != "0.1.0" {
		t.Errorf("theme version = %q, want %q", e.Theme().Version, "0.1.0")
	}
	if !e.HasTemplate("product") {
		t.Error("expected product template to be loaded")
	}
	if !e.HasTemplate("listing") {
		t.Error("expected listing template to be loaded")
	}
	if e.HasTemplate("layout") {
		t.Error("layout should not appear as a page template")
	}
}

func TestLoad_MissingDir(t *testing.T) {
	_, err := theme.Load("testdata/nonexistent")
	if err == nil {
		t.Fatal("expected error for missing directory")
	}
}

func TestLoad_NoTemplates(t *testing.T) {
	_, err := theme.Load("testdata/no_templates")
	if err == nil {
		t.Fatal("expected error when no .html templates exist")
	}
	if !strings.Contains(err.Error(), "no templates found") {
		t.Errorf("error = %q, want it to mention no templates found", err.Error())
	}
}

func TestLoad_NoName(t *testing.T) {
	_, err := theme.Load("testdata/no_name")
	if err == nil {
		t.Fatal("expected error when theme.yaml has no name")
	}
	if !strings.Contains(err.Error(), "name is required") {
		t.Errorf("error = %q, want it to mention name is required", err.Error())
	}
}

func TestRender_WithLayout(t *testing.T) {
	e, err := theme.Load("testdata/valid")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	data := struct {
		Name        string
		Description string
	}{Name: "Widget", Description: "A fine widget"}

	var buf bytes.Buffer
	if err := e.Render(&buf, "product", data); err != nil {
		t.Fatalf("Render: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "<title>Widget</title>") {
		t.Errorf("output missing <title>Widget</title>:\n%s", out)
	}
	if !strings.Contains(out, "<h1>Widget</h1>") {
		t.Errorf("output missing <h1>Widget</h1>:\n%s", out)
	}
	if !strings.Contains(out, "A fine widget") {
		t.Errorf("output missing description:\n%s", out)
	}
	if !strings.Contains(out, "<!DOCTYPE html>") {
		t.Errorf("output missing layout doctype:\n%s", out)
	}
}

func TestRender_ListingWithLayout(t *testing.T) {
	e, err := theme.Load("testdata/valid")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	data := struct {
		Items []string
	}{Items: []string{"A", "B"}}

	var buf bytes.Buffer
	if err := e.Render(&buf, "listing", data); err != nil {
		t.Fatalf("Render: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "<title>Listing</title>") {
		t.Errorf("output missing <title>Listing</title>:\n%s", out)
	}
	if !strings.Contains(out, "<li>A</li>") {
		t.Errorf("output missing <li>A</li>:\n%s", out)
	}
	if !strings.Contains(out, "<li>B</li>") {
		t.Errorf("output missing <li>B</li>:\n%s", out)
	}
}

func TestRender_WithoutLayout(t *testing.T) {
	e, err := theme.Load("testdata/no_layout")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	data := struct{ Name string }{Name: "Plain"}
	var buf bytes.Buffer
	if err := e.Render(&buf, "simple", data); err != nil {
		t.Fatalf("Render: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "<h1>Plain</h1>") {
		t.Errorf("output missing <h1>Plain</h1>:\n%s", out)
	}
	// Should NOT contain layout doctype.
	if strings.Contains(out, "<!DOCTYPE html>") {
		t.Errorf("output should not contain layout doctype:\n%s", out)
	}
}

func TestRender_NotFound(t *testing.T) {
	e, err := theme.Load("testdata/valid")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	var buf bytes.Buffer
	err = e.Render(&buf, "nonexistent", nil)
	if err == nil {
		t.Fatal("expected error for missing template")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want it to mention not found", err.Error())
	}
}
