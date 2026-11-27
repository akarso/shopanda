package mail

import (
	"bytes"
	"fmt"
	htmltemplate "html/template"
	"sync"
	texttemplate "text/template"
)

// NamedParts holds a plain-text subject template and an HTML body template.
type NamedParts struct {
	subject *texttemplate.Template
	body    *htmltemplate.Template
}

// Templates is a registry of named email templates.
type Templates struct {
	mu    sync.RWMutex
	tmpls map[string]*NamedParts
}

// NewTemplates creates an empty template registry.
func NewTemplates() *Templates {
	return &Templates{tmpls: make(map[string]*NamedParts)}
}

// Register adds a named template. Subject uses text/template; body uses
// html/template. Panics if name is empty or subject/body fails to parse.
// Prefer TryRegister when loading untrusted file templates (PR-1210).
func (t *Templates) Register(name, subject, body string) {
	if err := t.TryRegister(name, subject, body); err != nil {
		panic(err.Error())
	}
}

// ParseNamed parses subject (text/template) + body (html/template) without
// touching a registry.
func ParseNamed(name, subject, body string) (*NamedParts, error) {
	if name == "" {
		return nil, fmt.Errorf("mail.Templates.Register: name must not be empty")
	}
	subj, err := texttemplate.New(name + ".subject").Parse(subject)
	if err != nil {
		return nil, fmt.Errorf("mail: parse subject %q: %w", name, err)
	}
	bodyTmpl, err := htmltemplate.New(name).Parse(body)
	if err != nil {
		return nil, fmt.Errorf("mail: parse body %q: %w", name, err)
	}
	return &NamedParts{subject: subj, body: bodyTmpl}, nil
}

// TryRegister adds a named template and returns a parse error instead of
// panicking. Subject and body are both parsed before the registry is updated.
func (t *Templates) TryRegister(name, subject, body string) error {
	parts, err := ParseNamed(name, subject, body)
	if err != nil {
		return err
	}
	return t.PutParsed(map[string]*NamedParts{name: parts})
}

// PutParsed stores already-parsed NamedParts under one lock (LoadDir atomic apply).
// Rejects empty names and nil templates without mutating the registry.
func (t *Templates) PutParsed(entries map[string]*NamedParts) error {
	if len(entries) == 0 {
		return nil
	}
	for name, parts := range entries {
		if name == "" {
			return fmt.Errorf("mail: PutParsed empty name")
		}
		if parts == nil || parts.subject == nil || parts.body == nil {
			return fmt.Errorf("mail: PutParsed nil template %q", name)
		}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for name, parts := range entries {
		t.tmpls[name] = parts
	}
	return nil
}

// Render builds a Message by executing the named template with the given data.
func (t *Templates) Render(name, to string, data interface{}) (Message, error) {
	t.mu.RLock()
	parts, ok := t.tmpls[name]
	t.mu.RUnlock()
	if !ok {
		return Message{}, fmt.Errorf("mail: unknown template %q", name)
	}
	return executeNamed(parts, name, to, data)
}

func executeNamed(parts *NamedParts, name, to string, data interface{}) (Message, error) {
	if parts == nil || parts.subject == nil || parts.body == nil {
		return Message{}, fmt.Errorf("mail: nil template parts for %q", name)
	}
	var subBuf bytes.Buffer
	if err := parts.subject.Execute(&subBuf, data); err != nil {
		return Message{}, fmt.Errorf("mail: render subject %q: %w", name, err)
	}
	var bodyBuf bytes.Buffer
	if err := parts.body.Execute(&bodyBuf, data); err != nil {
		return Message{}, fmt.Errorf("mail: render body %q: %w", name, err)
	}
	return Message{
		To:      to,
		Subject: subBuf.String(),
		Body:    bodyBuf.String(),
	}, nil
}
