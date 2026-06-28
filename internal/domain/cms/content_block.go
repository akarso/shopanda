package cms

import (
	"fmt"
	"strings"
	"time"
)

// BlockType identifies a reusable content block shape.
type BlockType string

const (
	BlockTypeHero             BlockType = "hero"
	BlockTypeRichText         BlockType = "rich_text"
	BlockTypeProductCarousel BlockType = "product_carousel"
)

// TargetType identifies where a block is placed.
type TargetType string

const (
	TargetTypePage   TargetType = "page"
	TargetTypeLayout TargetType = "layout"
)

var validLayoutTargets = map[string]struct{}{
	"home": {},
}

// ValidBlockType reports whether t is supported.
func ValidBlockType(t BlockType) bool {
	switch t {
	case BlockTypeHero, BlockTypeRichText, BlockTypeProductCarousel:
		return true
	default:
		return false
	}
}

// ValidTargetType reports whether t is supported.
func ValidTargetType(t TargetType) bool {
	switch t {
	case TargetTypePage, TargetTypeLayout:
		return true
	default:
		return false
	}
}

// ValidLayoutTarget reports whether key is a supported layout target.
func ValidLayoutTarget(key string) bool {
	_, ok := validLayoutTargets[strings.TrimSpace(key)]
	return ok
}

// ContentBlock is a reusable CMS block definition.
type ContentBlock struct {
	id        string
	title     string
	blockType BlockType
	config    map[string]interface{}
	isActive  bool
	createdAt time.Time
	updatedAt time.Time
}

// NewContentBlock creates a validated ContentBlock.
func NewContentBlock(id, title string, blockType BlockType, config map[string]interface{}) (*ContentBlock, error) {
	if id == "" {
		return nil, fmt.Errorf("content block: empty id")
	}
	if strings.TrimSpace(title) == "" {
		return nil, fmt.Errorf("content block: empty title")
	}
	if !ValidBlockType(blockType) {
		return nil, fmt.Errorf("content block: invalid type %q", blockType)
	}
	normalized, err := NormalizeBlockConfig(blockType, config)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	return &ContentBlock{
		id:        id,
		title:     strings.TrimSpace(title),
		blockType: blockType,
		config:    normalized,
		isActive:  true,
		createdAt: now,
		updatedAt: now,
	}, nil
}

// NewContentBlockFromDB reconstructs a ContentBlock from stored data.
func NewContentBlockFromDB(
	id, title string,
	blockType BlockType,
	config map[string]interface{},
	isActive bool,
	createdAt, updatedAt time.Time,
) *ContentBlock {
	if config == nil {
		config = map[string]interface{}{}
	}
	return &ContentBlock{
		id:        id,
		title:     title,
		blockType: blockType,
		config:    config,
		isActive:  isActive,
		createdAt: createdAt,
		updatedAt: updatedAt,
	}
}

func (b *ContentBlock) ID() string                      { return b.id }
func (b *ContentBlock) Title() string                   { return b.title }
func (b *ContentBlock) BlockType() BlockType            { return b.blockType }
func (b *ContentBlock) Config() map[string]interface{}  { return b.config }
func (b *ContentBlock) IsActive() bool                  { return b.isActive }
func (b *ContentBlock) CreatedAt() time.Time            { return b.createdAt }
func (b *ContentBlock) UpdatedAt() time.Time            { return b.updatedAt }

// SetTitle updates the block title.
func (b *ContentBlock) SetTitle(title string) error {
	if strings.TrimSpace(title) == "" {
		return fmt.Errorf("content block: empty title")
	}
	b.title = strings.TrimSpace(title)
	b.updatedAt = time.Now().UTC()
	return nil
}

// SetConfig updates and validates block config.
func (b *ContentBlock) SetConfig(config map[string]interface{}) error {
	normalized, err := NormalizeBlockConfig(b.blockType, config)
	if err != nil {
		return err
	}
	b.config = normalized
	b.updatedAt = time.Now().UTC()
	return nil
}

// SetActive sets the active state.
func (b *ContentBlock) SetActive(active bool) {
	b.isActive = active
	b.updatedAt = time.Now().UTC()
}

// NormalizeBlockConfig validates and normalizes config for a block type.
func NormalizeBlockConfig(blockType BlockType, config map[string]interface{}) (map[string]interface{}, error) {
	if config == nil {
		config = map[string]interface{}{}
	}
	switch blockType {
	case BlockTypeHero:
		headline := strings.TrimSpace(stringField(config, "headline"))
		if headline == "" {
			return nil, fmt.Errorf("content block: hero requires headline")
		}
		return map[string]interface{}{
			"headline":    headline,
			"subheadline": strings.TrimSpace(stringField(config, "subheadline")),
			"cta_label":   strings.TrimSpace(stringField(config, "cta_label")),
			"cta_url":     strings.TrimSpace(stringField(config, "cta_url")),
			"image_url":   strings.TrimSpace(stringField(config, "image_url")),
		}, nil
	case BlockTypeRichText:
		body := strings.TrimSpace(stringField(config, "body"))
		if body == "" {
			return nil, fmt.Errorf("content block: rich_text requires body")
		}
		return map[string]interface{}{"body": body}, nil
	case BlockTypeProductCarousel:
		return map[string]interface{}{
			"title":       strings.TrimSpace(stringField(config, "title")),
			"product_ids": stringSliceField(config, "product_ids"),
		}, nil
	default:
		return nil, fmt.Errorf("content block: invalid type %q", blockType)
	}
}

func stringField(config map[string]interface{}, key string) string {
	value, ok := config[key]
	if !ok || value == nil {
		return ""
	}
	switch v := value.(type) {
	case string:
		return v
	default:
		return fmt.Sprint(v)
	}
}

func stringSliceField(config map[string]interface{}, key string) []string {
	value, ok := config[key]
	if !ok || value == nil {
		return []string{}
	}
	switch v := value.(type) {
	case []string:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if trimmed := strings.TrimSpace(item); trimmed != "" {
				out = append(out, trimmed)
			}
		}
		return out
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if trimmed := strings.TrimSpace(fmt.Sprint(item)); trimmed != "" {
				out = append(out, trimmed)
			}
		}
		return out
	default:
		return []string{}
	}
}
