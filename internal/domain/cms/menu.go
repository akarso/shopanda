package cms

import (
	"fmt"
	"strings"
	"time"
)

// LinkType identifies how a menu item target is resolved.
type LinkType string

const (
	LinkTypeURL      LinkType = "url"
	LinkTypeCategory LinkType = "category"
	LinkTypePage     LinkType = "page"
)

var validMenuCodes = map[string]struct{}{
	"header": {},
	"footer": {},
}

// ValidMenuCode reports whether code is a supported menu identifier.
func ValidMenuCode(code string) bool {
	_, ok := validMenuCodes[strings.TrimSpace(code)]
	return ok
}

// ValidLinkType reports whether t is a supported link type.
func ValidLinkType(t LinkType) bool {
	switch t {
	case LinkTypeURL, LinkTypeCategory, LinkTypePage:
		return true
	default:
		return false
	}
}

// Menu is a named navigation container (e.g. header, footer).
type Menu struct {
	id        string
	code      string
	title     string
	isActive  bool
	createdAt time.Time
	updatedAt time.Time
}

// NewMenu creates a validated Menu.
func NewMenu(id, code, title string) (*Menu, error) {
	if id == "" {
		return nil, fmt.Errorf("menu: empty id")
	}
	code = strings.TrimSpace(code)
	if !ValidMenuCode(code) {
		return nil, fmt.Errorf("menu: invalid code %q", code)
	}
	if strings.TrimSpace(title) == "" {
		return nil, fmt.Errorf("menu: empty title")
	}
	now := time.Now().UTC()
	return &Menu{
		id:        id,
		code:      code,
		title:     strings.TrimSpace(title),
		isActive:  true,
		createdAt: now,
		updatedAt: now,
	}, nil
}

// NewMenuFromDB reconstructs a Menu from stored data.
func NewMenuFromDB(id, code, title string, isActive bool, createdAt, updatedAt time.Time) *Menu {
	return &Menu{
		id:        id,
		code:      code,
		title:     title,
		isActive:  isActive,
		createdAt: createdAt,
		updatedAt: updatedAt,
	}
}

func (m *Menu) ID() string           { return m.id }
func (m *Menu) Code() string         { return m.code }
func (m *Menu) Title() string        { return m.title }
func (m *Menu) IsActive() bool       { return m.isActive }
func (m *Menu) CreatedAt() time.Time { return m.createdAt }
func (m *Menu) UpdatedAt() time.Time { return m.updatedAt }

// SetTitle updates the menu title.
func (m *Menu) SetTitle(title string) error {
	if strings.TrimSpace(title) == "" {
		return fmt.Errorf("menu: empty title")
	}
	m.title = strings.TrimSpace(title)
	m.updatedAt = time.Now().UTC()
	return nil
}

// SetActive sets the active state.
func (m *Menu) SetActive(active bool) {
	m.isActive = active
	m.updatedAt = time.Now().UTC()
}

// MenuItem is a single navigation entry within a menu.
type MenuItem struct {
	id         string
	menuID     string
	parentID   string
	label      string
	linkType   LinkType
	linkTarget string
	position   int
	isActive   bool
	createdAt  time.Time
	updatedAt  time.Time
}

// NewMenuItem creates a validated MenuItem.
func NewMenuItem(id, menuID, parentID, label string, linkType LinkType, linkTarget string, position int) (*MenuItem, error) {
	if id == "" {
		return nil, fmt.Errorf("menu item: empty id")
	}
	if menuID == "" {
		return nil, fmt.Errorf("menu item: empty menu id")
	}
	if strings.TrimSpace(label) == "" {
		return nil, fmt.Errorf("menu item: empty label")
	}
	if !ValidLinkType(linkType) {
		return nil, fmt.Errorf("menu item: invalid link type %q", linkType)
	}
	linkTarget = strings.TrimSpace(linkTarget)
	switch linkType {
	case LinkTypeURL:
		if linkTarget == "" {
			return nil, fmt.Errorf("menu item: url link requires target")
		}
	case LinkTypeCategory, LinkTypePage:
		if linkTarget == "" {
			return nil, fmt.Errorf("menu item: %s link requires target id", linkType)
		}
	}
	now := time.Now().UTC()
	return &MenuItem{
		id:         id,
		menuID:     menuID,
		parentID:   strings.TrimSpace(parentID),
		label:      strings.TrimSpace(label),
		linkType:   linkType,
		linkTarget: linkTarget,
		position:   position,
		isActive:   true,
		createdAt:  now,
		updatedAt:  now,
	}, nil
}

// NewMenuItemFromDB reconstructs a MenuItem from stored data.
func NewMenuItemFromDB(
	id, menuID, parentID, label string,
	linkType LinkType,
	linkTarget string,
	position int,
	isActive bool,
	createdAt, updatedAt time.Time,
) *MenuItem {
	return &MenuItem{
		id:         id,
		menuID:     menuID,
		parentID:   parentID,
		label:      label,
		linkType:   linkType,
		linkTarget: linkTarget,
		position:   position,
		isActive:   isActive,
		createdAt:  createdAt,
		updatedAt:  updatedAt,
	}
}

func (i *MenuItem) ID() string           { return i.id }
func (i *MenuItem) MenuID() string       { return i.menuID }
func (i *MenuItem) ParentID() string     { return i.parentID }
func (i *MenuItem) Label() string        { return i.label }
func (i *MenuItem) LinkType() LinkType   { return i.linkType }
func (i *MenuItem) LinkTarget() string   { return i.linkTarget }
func (i *MenuItem) Position() int        { return i.position }
func (i *MenuItem) IsActive() bool       { return i.isActive }
func (i *MenuItem) CreatedAt() time.Time { return i.createdAt }
func (i *MenuItem) UpdatedAt() time.Time { return i.updatedAt }

// MenuWithItems is a menu and its flat item list.
type MenuWithItems struct {
	Menu  *Menu
	Items []*MenuItem
}

// ValidateMenuItems checks parent references and cycles within a flat item batch.
func ValidateMenuItems(items []*MenuItem) error {
	if len(items) == 0 {
		return nil
	}
	ids := make(map[string]struct{}, len(items))
	for _, item := range items {
		if item == nil {
			return fmt.Errorf("menu items: nil item")
		}
		if _, exists := ids[item.ID()]; exists {
			return fmt.Errorf("menu items: duplicate id %q", item.ID())
		}
		ids[item.ID()] = struct{}{}
	}
	for _, item := range items {
		parentID := item.ParentID()
		if parentID == "" {
			continue
		}
		if parentID == item.ID() {
			return fmt.Errorf("menu items: item %q cannot be its own parent", item.ID())
		}
		if _, ok := ids[parentID]; !ok {
			return fmt.Errorf("menu items: unknown parent %q for item %q", parentID, item.ID())
		}
	}
	// Cycle detection via DFS from each root.
	children := make(map[string][]string, len(items))
	for _, item := range items {
		if item.ParentID() != "" {
			children[item.ParentID()] = append(children[item.ParentID()], item.ID())
		}
	}
	visited := make(map[string]struct{}, len(items))
	stack := make(map[string]struct{}, len(items))
	var visit func(id string) error
	visit = func(id string) error {
		if _, ok := stack[id]; ok {
			return fmt.Errorf("menu items: cycle detected at %q", id)
		}
		if _, ok := visited[id]; ok {
			return nil
		}
		stack[id] = struct{}{}
		for _, childID := range children[id] {
			if err := visit(childID); err != nil {
				return err
			}
		}
		delete(stack, id)
		visited[id] = struct{}{}
		return nil
	}
	for id := range ids {
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}
