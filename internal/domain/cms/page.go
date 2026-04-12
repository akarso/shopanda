package cms

import (
	"fmt"
	"strings"
	"time"
)

// Page is a CMS content page.
type Page struct {
	id        string
	slug      string
	title     string
	content   string
	isActive  bool
	createdAt time.Time
	updatedAt time.Time
}

// NewPage creates a validated Page.
func NewPage(id, slug, title, content string) (*Page, error) {
	if id == "" {
		return nil, fmt.Errorf("page: empty id")
	}
	if slug == "" {
		return nil, fmt.Errorf("page: empty slug")
	}
	if strings.Contains(slug, "/") {
		return nil, fmt.Errorf("page: invalid slug: contains '/'")
	}
	if title == "" {
		return nil, fmt.Errorf("page: empty title")
	}
	now := time.Now().UTC()
	return &Page{
		id:        id,
		slug:      slug,
		title:     title,
		content:   content,
		isActive:  true,
		createdAt: now,
		updatedAt: now,
	}, nil
}

// NewPageFromDB reconstructs a Page from stored data.
func NewPageFromDB(id, slug, title, content string, isActive bool, createdAt, updatedAt time.Time) *Page {
	return &Page{
		id:        id,
		slug:      slug,
		title:     title,
		content:   content,
		isActive:  isActive,
		createdAt: createdAt,
		updatedAt: updatedAt,
	}
}

func (p *Page) ID() string           { return p.id }
func (p *Page) Slug() string         { return p.slug }
func (p *Page) Title() string        { return p.title }
func (p *Page) Content() string      { return p.content }
func (p *Page) IsActive() bool       { return p.isActive }
func (p *Page) CreatedAt() time.Time { return p.createdAt }
func (p *Page) UpdatedAt() time.Time { return p.updatedAt }

// SetSlug updates the slug.
func (p *Page) SetSlug(slug string) error {
	if slug == "" {
		return fmt.Errorf("page: empty slug")
	}
	if strings.Contains(slug, "/") {
		return fmt.Errorf("page: invalid slug: contains '/'")
	}
	p.slug = slug
	p.updatedAt = time.Now().UTC()
	return nil
}

// SetTitle updates the title.
func (p *Page) SetTitle(title string) error {
	if title == "" {
		return fmt.Errorf("page: empty title")
	}
	p.title = title
	p.updatedAt = time.Now().UTC()
	return nil
}

// SetContent updates the content.
func (p *Page) SetContent(content string) {
	p.content = content
	p.updatedAt = time.Now().UTC()
}

// SetActive sets the active state.
func (p *Page) SetActive(active bool) {
	p.isActive = active
	p.updatedAt = time.Now().UTC()
}
