package cms_test

import (
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
