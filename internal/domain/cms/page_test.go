package cms_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/domain/cms"
)

func TestNewPage_Valid(t *testing.T) {
	p, err := cms.NewPage("id-1", "about-us", "About Us", "<p>Hello</p>")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.ID() != "id-1" {
		t.Errorf("id = %q, want %q", p.ID(), "id-1")
	}
	if p.Slug() != "about-us" {
		t.Errorf("slug = %q, want %q", p.Slug(), "about-us")
	}
	if p.Title() != "About Us" {
		t.Errorf("title = %q, want %q", p.Title(), "About Us")
	}
	if p.Content() != "<p>Hello</p>" {
		t.Errorf("content = %q, want %q", p.Content(), "<p>Hello</p>")
	}
	if !p.IsActive() {
		t.Error("expected is_active = true")
	}
	if p.CreatedAt().IsZero() {
		t.Error("expected non-zero created_at")
	}
}

func TestNewPage_EmptyID(t *testing.T) {
	_, err := cms.NewPage("", "slug", "Title", "")
	if err == nil {
		t.Fatal("expected error for empty id")
	}
}

func TestNewPage_EmptySlug(t *testing.T) {
	_, err := cms.NewPage("id", "", "Title", "")
	if err == nil {
		t.Fatal("expected error for empty slug")
	}
}

func TestNewPage_EmptyTitle(t *testing.T) {
	_, err := cms.NewPage("id", "slug", "", "")
	if err == nil {
		t.Fatal("expected error for empty title")
	}
}

func TestPage_SetSlug(t *testing.T) {
	p, _ := cms.NewPage("id", "old", "Title", "")
	if err := p.SetSlug("new"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Slug() != "new" {
		t.Errorf("slug = %q, want %q", p.Slug(), "new")
	}
}

func TestPage_SetSlug_Empty(t *testing.T) {
	p, _ := cms.NewPage("id", "slug", "Title", "")
	if err := p.SetSlug(""); err == nil {
		t.Fatal("expected error for empty slug")
	}
}

func TestPage_SetTitle(t *testing.T) {
	p, _ := cms.NewPage("id", "slug", "Old", "")
	if err := p.SetTitle("New"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Title() != "New" {
		t.Errorf("title = %q, want %q", p.Title(), "New")
	}
}

func TestPage_SetTitle_Empty(t *testing.T) {
	p, _ := cms.NewPage("id", "slug", "Title", "")
	if err := p.SetTitle(""); err == nil {
		t.Fatal("expected error for empty title")
	}
}

func TestPage_SetContent(t *testing.T) {
	p, _ := cms.NewPage("id", "slug", "Title", "")
	p.SetContent("<p>New</p>")
	if p.Content() != "<p>New</p>" {
		t.Errorf("content = %q, want %q", p.Content(), "<p>New</p>")
	}
}

func TestPage_SetActive(t *testing.T) {
	p, _ := cms.NewPage("id", "slug", "Title", "")
	if !p.IsActive() {
		t.Error("expected default is_active = true")
	}
	p.SetActive(false)
	if p.IsActive() {
		t.Error("expected is_active = false after SetActive(false)")
	}
}
