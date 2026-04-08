package mail

import (
	"bytes"
	"fmt"
	"html/template"
	"sync"
)

// Templates is a registry of named email templates.
type Templates struct {
	mu    sync.RWMutex
	tmpls map[string]*template.Template
}

// NewTemplates creates an empty template registry.
func NewTemplates() *Templates {
	return &Templates{tmpls: make(map[string]*template.Template)}
}

// Register adds a named template. The body is parsed as html/template.
// Panics if name is empty or body fails to parse.
func (t *Templates) Register(name, subject, body string) {
	if name == "" {
		panic("mail.Templates.Register: name must not be empty")
	}
	tmpl, err := template.New(name).Parse(body)
	if err != nil {
		panic(fmt.Sprintf("mail.Templates.Register(%q): %v", name, err))
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.tmpls[name] = tmpl
	// Store subject template alongside using a sub-template.
	_ = template.Must(tmpl.New(name + ".subject").Parse(subject))
}

// Render builds a Message by executing the named template with the given data.
func (t *Templates) Render(name, to string, data interface{}) (Message, error) {
	t.mu.RLock()
	tmpl, ok := t.tmpls[name]
	t.mu.RUnlock()
	if !ok {
		return Message{}, fmt.Errorf("mail: unknown template %q", name)
	}

	var subBuf bytes.Buffer
	subTmpl := tmpl.Lookup(name + ".subject")
	if subTmpl == nil {
		return Message{}, fmt.Errorf("mail: subject template missing for %q", name)
	}
	if err := subTmpl.Execute(&subBuf, data); err != nil {
		return Message{}, fmt.Errorf("mail: render subject %q: %w", name, err)
	}

	var bodyBuf bytes.Buffer
	if err := tmpl.Execute(&bodyBuf, data); err != nil {
		return Message{}, fmt.Errorf("mail: render body %q: %w", name, err)
	}

	return Message{
		To:      to,
		Subject: subBuf.String(),
		Body:    bodyBuf.String(),
	}, nil
}
