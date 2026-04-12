package routing

import "fmt"

// URLRewrite maps a URL path to a typed entity.
type URLRewrite struct {
	path     string
	typ      string
	entityID string
}

// NewURLRewrite creates a validated URLRewrite.
func NewURLRewrite(path, typ, entityID string) (*URLRewrite, error) {
	if path == "" {
		return nil, fmt.Errorf("url rewrite: empty path")
	}
	if typ == "" {
		return nil, fmt.Errorf("url rewrite: empty type")
	}
	if entityID == "" {
		return nil, fmt.Errorf("url rewrite: empty entity_id")
	}
	return &URLRewrite{path: path, typ: typ, entityID: entityID}, nil
}

// NewURLRewriteFromDB reconstructs a URLRewrite from stored data.
func NewURLRewriteFromDB(path, typ, entityID string) *URLRewrite {
	return &URLRewrite{path: path, typ: typ, entityID: entityID}
}

func (u *URLRewrite) Path() string     { return u.path }
func (u *URLRewrite) Type() string     { return u.typ }
func (u *URLRewrite) EntityID() string { return u.entityID }
