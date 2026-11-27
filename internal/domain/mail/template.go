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
// Panics if name is empty or body/subject fails to parse.
// Prefer TryRegister when loading untrusted file templates (PR-1210).
func (t *Templates) Register(name, subject, body string) {
	if err := t.TryRegister(name, subject, body); err != nil {
		panic(err.Error())
	}
}

// ParseNamed parses subject + body without touching a registry.
func ParseNamed(name, subject, body string) (*template.Template, error) {
	if name == "" {
		return nil, fmt.Errorf("mail.Templates.Register: name must not be empty")
	}
	tmpl, err := template.New(name).Parse(body)
	if err != nil {
		return nil, fmt.Errorf("mail: parse body %q: %w", name, err)
	}
	if _, err := tmpl.New(name + ".subject").Parse(subject); err != nil {
		return nil, fmt.Errorf("mail: parse subject %q: %w", name, err)
	}
	return tmpl, nil
}

// TryRegister adds a named template and returns a parse error instead of
// panicking. Subject and body are both parsed before the registry is updated.
func (t *Templates) TryRegister(name, subject, body string) error {
	tmpl, err := ParseNamed(name, subject, body)
	if err != nil {
		return err
	}
	t.PutAll(map[string]*template.Template{name: tmpl})
	return nil
}

// PutAll stores already-parsed templates under one lock (LoadDir atomic apply).
func (t *Templates) PutAll(entries map[string]*template.Template) {
	if len(entries) == 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for name, tmpl := range entries {
		t.tmpls[name] = tmpl
	}
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
