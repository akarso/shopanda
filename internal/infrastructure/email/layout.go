package email

import (
	"bytes"
	"fmt"
	"html/template"
	"os"
	"path/filepath"

	"github.com/akarso/shopanda/internal/domain/mail"
)

// layoutData is passed to the layout template.
type layoutData struct {
	StoreName    string
	StoreURL     string
	LogoURL      string
	StoreAddress string
	Body         template.HTML
}

// LayoutRenderer wraps rendered template bodies in a shared layout.
type LayoutRenderer struct {
	layout *template.Template
}

// NewLayoutRenderer parses _layout.html from dir.
// If the layout file does not exist, Wrap returns the body unchanged.
func NewLayoutRenderer(dir string) (*LayoutRenderer, error) {
	path := filepath.Join(dir, "_layout.html")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &LayoutRenderer{}, nil
		}
		return nil, fmt.Errorf("email: read layout: %w", err)
	}

	tmpl, err := template.New("layout").Parse(string(data))
	if err != nil {
		return nil, fmt.Errorf("email: parse layout: %w", err)
	}

	return &LayoutRenderer{layout: tmpl}, nil
}

// Wrap applies the layout to the rendered body HTML.
// If no layout was loaded, returns body unchanged.
func (lr *LayoutRenderer) Wrap(body string, ed mail.EmailData) (string, error) {
	if lr.layout == nil {
		return body, nil
	}

	ld := layoutData{
		StoreName:    ed.StoreName,
		StoreURL:     ed.StoreURL,
		LogoURL:      ed.LogoURL,
		StoreAddress: ed.StoreAddress,
		Body:         template.HTML(body),
	}

	var buf bytes.Buffer
	if err := lr.layout.Execute(&buf, ld); err != nil {
		return "", fmt.Errorf("email: render layout: %w", err)
	}
	return buf.String(), nil
}
