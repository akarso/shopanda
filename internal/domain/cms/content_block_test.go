package cms_test

import (
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/domain/cms"
)

func TestNewContentBlockHeroValidation(t *testing.T) {
	_, err := cms.NewContentBlock("block-1", "Hero", cms.BlockTypeHero, map[string]interface{}{})
	if err == nil {
		t.Fatal("expected hero headline required")
	}
	block, err := cms.NewContentBlock("block-1", "Hero", cms.BlockTypeHero, map[string]interface{}{
		"headline": "Welcome",
	})
	if err != nil || block.Config()["headline"] != "Welcome" {
		t.Fatalf("unexpected block: %v err=%v", block, err)
	}
}

func TestNewContentBlockRichTextValidation(t *testing.T) {
	_, err := cms.NewContentBlock("block-1", "Copy", cms.BlockTypeRichText, map[string]interface{}{})
	if err == nil {
		t.Fatal("expected rich_text body required")
	}
}

func TestValidLayoutTarget(t *testing.T) {
	if !cms.ValidLayoutTarget("home") {
		t.Fatal("expected home layout target")
	}
	if cms.ValidLayoutTarget("footer") {
		t.Fatal("expected unknown layout target invalid")
	}
}

func TestContentBlockConfigDefensiveCopy(t *testing.T) {
	input := map[string]interface{}{"headline": "Welcome"}
	block, err := cms.NewContentBlock("block-1", "Hero", cms.BlockTypeHero, input)
	if err != nil {
		t.Fatal(err)
	}
	input["headline"] = "Changed"
	if block.Config()["headline"] != "Welcome" {
		t.Fatal("expected internal config isolated from caller map")
	}
	config := block.Config()
	config["headline"] = "Mutated"
	if block.Config()["headline"] != "Welcome" {
		t.Fatal("expected Config() to return defensive copy")
	}
}

func TestSanitizeHTMLStripsScript(t *testing.T) {
	html := cms.SanitizeHTML(`<p>Hello</p><script>alert(1)</script>`)
	if strings.Contains(string(html), "<script") {
		t.Fatalf("script tag not stripped: %s", html)
	}
	if !strings.Contains(string(html), "<p>Hello</p>") {
		t.Fatalf("expected safe markup preserved: %s", html)
	}
}
