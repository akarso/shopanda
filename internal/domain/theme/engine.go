package theme

import (
	"fmt"
	"html/template"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Engine loads and renders theme templates.
// Each page template is parsed together with layout.html (if present) so that
// templates like "title" and "content" are scoped per page.
type Engine struct {
	theme Theme
	pages map[string]*template.Template
}

// Load reads theme.yaml and parses all .html templates from the theme directory.
// If a layout.html exists, every other template is parsed together with it,
// allowing each page to define blocks consumed by the layout.
//
// The expected structure is:
//
//	<dir>/theme.yaml
//	<dir>/templates/layout.html   (optional)
//	<dir>/templates/*.html        (page templates)
func Load(dir string) (*Engine, error) {
	meta, err := loadThemeYAML(filepath.Join(dir, "theme.yaml"))
	if err != nil {
		return nil, fmt.Errorf("theme: load metadata: %w", err)
	}

	templatesDir := filepath.Join(dir, "templates")
	pattern := filepath.Join(templatesDir, "*.html")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("theme: glob templates: %w", err)
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("theme: no templates found in %s", pattern)
	}

	// Separate layout from page templates.
	var layoutFile string
	var pageFiles []string
	for _, m := range matches {
		base := filepath.Base(m)
		if base == "layout.html" {
			layoutFile = m
		} else {
			pageFiles = append(pageFiles, m)
		}
	}
	if len(pageFiles) == 0 {
		return nil, fmt.Errorf("theme: no page templates found (only layout.html)")
	}

	pages := make(map[string]*template.Template, len(pageFiles))
	for _, pf := range pageFiles {
		name := strings.TrimSuffix(filepath.Base(pf), filepath.Ext(pf))
		var t *template.Template
		if layoutFile != "" {
			t, err = template.ParseFiles(layoutFile, pf)
		} else {
			t, err = template.ParseFiles(pf)
		}
		if err != nil {
			return nil, fmt.Errorf("theme: parse %s: %w", filepath.Base(pf), err)
		}
		pages[name] = t
	}

	return &Engine{theme: meta, pages: pages}, nil
}

// Theme returns the loaded theme metadata.
func (e *Engine) Theme() Theme {
	return e.theme
}

// Render executes the named page template and writes the result to w.
// The name is the template filename without extension (e.g. "product").
func (e *Engine) Render(w io.Writer, name string, data interface{}) error {
	t, ok := e.pages[name]
	if !ok {
		return fmt.Errorf("theme: template %q not found", name)
	}
	return t.Execute(w, data)
}

// HasTemplate reports whether a page template with the given name is loaded.
func (e *Engine) HasTemplate(name string) bool {
	_, ok := e.pages[name]
	return ok
}

func loadThemeYAML(path string) (Theme, error) {
	f, err := os.Open(path)
	if err != nil {
		return Theme{}, err
	}
	defer f.Close()

	var t Theme
	if err := yaml.NewDecoder(f).Decode(&t); err != nil {
		return Theme{}, err
	}
	if t.Name == "" {
		return Theme{}, fmt.Errorf("theme: name is required in theme.yaml")
	}
	return t, nil
}
