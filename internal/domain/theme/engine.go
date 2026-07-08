package theme

import (
	"fmt"
	"html/template"
	"io"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Option configures theme loading.
type Option func(*loadOptions)

type loadOptions struct {
	slots SlotSource
}

// WithSlotSource enables slot template markers backed by source.
func WithSlotSource(source SlotSource) Option {
	return func(o *loadOptions) {
		o.slots = source
	}
}

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
//	<dir>/theme.yaml              (optional parent: relative/absolute path)
//	<dir>/templates/layout.html   (optional)
//	<dir>/templates/*.html        (page templates; child overrides parent by filename)
func Load(dir string, opts ...Option) (*Engine, error) {
	var cfg loadOptions
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	meta, err := loadThemeYAML(filepath.Join(dir, "theme.yaml"))
	if err != nil {
		return nil, fmt.Errorf("theme: load metadata: %w", err)
	}

	resolved, err := resolveThemeTemplates(dir, make(map[string]struct{}))
	if err != nil {
		return nil, err
	}

	layoutFile := resolved.layoutFile

	funcMap := slotFuncMap(cfg.slots)
	pages := make(map[string]*template.Template, len(resolved.pageFiles))
	for name, pf := range resolved.pageFiles {
		t, err := parsePageTemplate(layoutFile, pf, funcMap)
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

func parsePageTemplate(layoutFile, pageFile string, funcMap template.FuncMap) (*template.Template, error) {
	pageSource, err := os.ReadFile(pageFile)
	if err != nil {
		return nil, err
	}
	pageSource = []byte(preprocessSlotContainers(string(pageSource)))

	if layoutFile == "" {
		name := filepath.Base(pageFile)
		return template.New(name).Funcs(funcMap).Parse(string(pageSource))
	}

	layoutSource, err := os.ReadFile(layoutFile)
	if err != nil {
		return nil, err
	}
	layoutSource = []byte(preprocessSlotContainers(string(layoutSource)))

	layoutName := filepath.Base(layoutFile)
	pageName := filepath.Base(pageFile)
	root := template.New(layoutName).Funcs(funcMap)
	if _, err := root.Parse(string(layoutSource)); err != nil {
		return nil, err
	}
	if _, err := root.New(pageName).Parse(string(pageSource)); err != nil {
		return nil, err
	}
	return root, nil
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
