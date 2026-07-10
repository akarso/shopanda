package theme

import (
	"fmt"
	"html/template"
	"io"
	"os"
	"path/filepath"
	"sort"
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

// NewEngine builds an engine from already-resolved inherited templates.
func NewEngine(resolved ResolvedTemplates, meta Theme, opts ...Option) (*Engine, error) {
	var cfg loadOptions
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	layoutFile := resolved.LayoutFile

	funcMap := slotFuncMap(cfg.slots)
	pages := make(map[string]*template.Template, len(resolved.PageFiles))
	for name, pf := range resolved.PageFiles {
		t, err := parsePageTemplate(layoutFile, resolved.PartialFiles, pf, resolved.Layout, funcMap)
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

func parsePageTemplate(layoutFile string, partialFiles map[string]string, pageFile string, layout LayoutConfig, funcMap template.FuncMap) (*template.Template, error) {
	pageSource, err := os.ReadFile(pageFile)
	if err != nil {
		return nil, err
	}
	pageSource = []byte(preprocessTemplateSource(string(pageSource), layout))

	if layoutFile == "" {
		name := filepath.Base(pageFile)
		return template.New(name).Funcs(funcMap).Parse(string(pageSource))
	}

	layoutSource, err := os.ReadFile(layoutFile)
	if err != nil {
		return nil, err
	}
	layoutSource = []byte(preprocessTemplateSource(string(layoutSource), layout))

	layoutName := filepath.Base(layoutFile)
	pageName := filepath.Base(pageFile)
	root := template.New(layoutName).Funcs(funcMap)
	if _, err := root.Parse(string(layoutSource)); err != nil {
		return nil, err
	}
	if err := parsePartialTemplates(root, partialFiles, layout); err != nil {
		return nil, err
	}
	if _, err := root.New(pageName).Parse(string(pageSource)); err != nil {
		return nil, err
	}
	return root, nil
}

func parsePartialTemplates(root *template.Template, partialFiles map[string]string, layout LayoutConfig) error {
	if len(partialFiles) == 0 {
		return nil
	}
	names := make([]string, 0, len(partialFiles))
	for name := range partialFiles {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		source, err := os.ReadFile(partialFiles[name])
		if err != nil {
			return fmt.Errorf("theme: read partial %s: %w", name, err)
		}
		source = []byte(preprocessTemplateSource(string(source), layout))
		if _, err := root.New(name + ".html").Parse(string(source)); err != nil {
			return fmt.Errorf("theme: parse partial %s: %w", name, err)
		}
	}
	return nil
}

