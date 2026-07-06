package theme

import (
	"fmt"
	"html/template"
	"regexp"
	"strings"
)

// SlotSource renders plugin HTML for a named anchor and placement.
type SlotSource interface {
	Render(anchor, placement string, data interface{}) string
}

var (
	slotContainerPattern = regexp.MustCompile(`(?s)\{\{\s*slot_container\s+"([^"]+)"\s*\}\}(.*?)\{\{\s*/slot_container\s*\}\}`)
	openTagPattern       = regexp.MustCompile(`(?is)^\s*<([a-zA-Z][\w-]*)(\s[^>]*)?>`)
)

// preprocessSlotContainers expands slot_container blocks into explicit slot markers.
func preprocessSlotContainers(source string) string {
	return slotContainerPattern.ReplaceAllStringFunc(source, func(block string) string {
		m := slotContainerPattern.FindStringSubmatch(block)
		if len(m) != 3 {
			return block
		}
		anchor := m[1]
		inner := m[2]
		return expandSlotContainer(anchor, inner)
	})
}

func expandSlotContainer(anchor, inner string) string {
	if tag, attrs, content, ok := splitSingleElement(inner); ok {
		var b strings.Builder
		b.WriteString(fmt.Sprintf(`{{slot . %q "before"}}`, anchor))
		b.WriteString("\n<")
		b.WriteString(tag)
		b.WriteString(attrs)
		b.WriteString(">\n")
		b.WriteString(fmt.Sprintf(`{{slot . %q "prepend"}}`, anchor))
		b.WriteString("\n")
		b.WriteString(content)
		b.WriteString(fmt.Sprintf(`{{slot . %q "append"}}`, anchor))
		b.WriteString("\n</")
		b.WriteString(tag)
		b.WriteString(">\n")
		b.WriteString(fmt.Sprintf(`{{slot . %q "after"}}`, anchor))
		return b.String()
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf(`{{slot . %q "before"}}`, anchor))
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf(`{{slot . %q "prepend"}}`, anchor))
	b.WriteString("\n")
	b.WriteString(inner)
	b.WriteString(fmt.Sprintf(`{{slot . %q "append"}}`, anchor))
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf(`{{slot . %q "after"}}`, anchor))
	return b.String()
}

func splitSingleElement(inner string) (tag, attrs, content string, ok bool) {
	trimmed := strings.TrimSpace(inner)
	m := openTagPattern.FindStringSubmatch(trimmed)
	if m == nil {
		return "", "", "", false
	}
	tag = m[1]
	attrs = m[2]
	openEnd := strings.Index(trimmed, ">")
	if openEnd < 0 {
		return "", "", "", false
	}
	rest := trimmed[openEnd+1:]
	closeTag := "</" + tag + ">"
	closeIdx := strings.LastIndex(strings.ToLower(rest), strings.ToLower(closeTag))
	if closeIdx < 0 {
		return "", "", "", false
	}
	content = rest[:closeIdx]
	if strings.TrimSpace(rest[closeIdx+len(closeTag):]) != "" {
		return "", "", "", false
	}
	return tag, attrs, content, true
}

func slotFuncMap(source SlotSource) template.FuncMap {
	return template.FuncMap{
		"slot": func(data interface{}, anchor, placement string) template.HTML {
			if source == nil {
				return template.HTML("")
			}
			return template.HTML(source.Render(anchor, placement, data))
		},
	}
}
