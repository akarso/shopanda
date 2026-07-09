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
	slotContainerOpenPattern = regexp.MustCompile(`\{\{\s*slot_container\s+"([^"]+)"\s*\}\}`)
	slotContainerCloseTag    = "{{/slot_container}}"
	openTagPattern           = regexp.MustCompile(`(?is)^\s*<([a-zA-Z][\w-]*)(\s[^>]*)?>`)
)

// preprocessSlotContainers expands slot_container blocks into explicit slot markers.
// Nested slot_container blocks are supported via depth-aware matching.
func preprocessSlotContainers(source string) string {
	anchor, openStart, openEnd, ok := findSlotContainerOpen(source, 0)
	if !ok {
		return source
	}
	closeStart, closeEnd, ok := findMatchingSlotContainerClose(source, openEnd)
	if !ok {
		return source
	}

	inner := preprocessSlotContainers(source[openEnd:closeStart])
	expanded := expandSlotContainer(anchor, inner)
	return source[:openStart] + expanded + preprocessSlotContainers(source[closeEnd:])
}

func findSlotContainerOpen(source string, from int) (anchor string, openStart, openEnd int, ok bool) {
	rest := source[from:]
	idx := strings.Index(rest, "{{")
	if idx < 0 {
		return "", 0, 0, false
	}
	loc := slotContainerOpenPattern.FindStringSubmatchIndex(rest[idx:])
	if loc == nil || loc[0] != 0 {
		next := from + idx + 2
		if next >= len(source) {
			return "", 0, 0, false
		}
		return findSlotContainerOpen(source, next)
	}
	match := slotContainerOpenPattern.FindStringSubmatch(rest[idx:])
	openStart = from + idx
	openEnd = openStart + loc[1]
	return match[1], openStart, openEnd, true
}

func findMatchingSlotContainerClose(source string, from int) (closeStart, closeEnd int, ok bool) {
	depth := 1
	i := from
	for i < len(source) && depth > 0 {
		nextCloseRel := strings.Index(source[i:], slotContainerCloseTag)
		if nextCloseRel < 0 {
			return 0, 0, false
		}
		nextClose := i + nextCloseRel

		nextOpenRel := strings.Index(source[i:nextClose], "{{")
		if nextOpenRel < 0 {
			depth--
			if depth == 0 {
				closeStart = nextClose
				closeEnd = nextClose + len(slotContainerCloseTag)
				return closeStart, closeEnd, true
			}
			i = nextClose + len(slotContainerCloseTag)
			continue
		}
		nextOpen := i + nextOpenRel

		if isSlotContainerOpenAt(source, nextOpen) {
			_, _, openEnd, openOK := findSlotContainerOpen(source, nextOpen)
			if !openOK {
				return 0, 0, false
			}
			depth++
			i = openEnd
			continue
		}

		i = skipTemplateAction(source, nextOpen)
	}
	return 0, 0, false
}

func skipTemplateAction(source string, idx int) int {
	if idx < 0 || idx >= len(source) || !strings.HasPrefix(source[idx:], "{{") {
		return idx + 2
	}
	if strings.HasPrefix(source[idx:], slotContainerCloseTag) {
		return idx
	}
	end := strings.Index(source[idx:], "}}")
	if end < 0 {
		return len(source)
	}
	return idx + end + 2
}

func isSlotContainerOpenAt(source string, idx int) bool {
	if idx < 0 || idx >= len(source) {
		return false
	}
	loc := slotContainerOpenPattern.FindStringSubmatchIndex(source[idx:])
	return loc != nil && loc[0] == 0
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
