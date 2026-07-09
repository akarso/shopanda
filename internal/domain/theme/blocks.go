package theme

import (
	"regexp"
	"strings"
)

var (
	layoutBlocksOpenPattern  = regexp.MustCompile(`\{\{\s*layout_blocks\s+"([^"]+)"\s*\}\}`)
	layoutBlocksCloseTag     = "{{/layout_blocks}}"
	blockOpenPattern         = regexp.MustCompile(`\{\{\s*block\s+"([^"]+)"\s*\}\}`)
	blockCloseTag            = "{{/block}}"
)

type namedBlock struct {
	name    string
	content string
}

func preprocessTemplateSource(source string, layout LayoutConfig) string {
	source = preprocessLayoutBlocks(source, layout)
	source = preprocessSlotContainers(source)
	return source
}

func preprocessLayoutBlocks(source string, layout LayoutConfig) string {
	container, openStart, openEnd, ok := findLayoutBlocksOpen(source, 0)
	if !ok {
		return source
	}
	closeStart, closeEnd, ok := findMatchingLayoutBlocksClose(source, openEnd)
	if !ok {
		return source
	}

	inner := source[openEnd:closeStart]
	blocks := extractNamedBlocks(inner)
	order := make([]string, 0, len(blocks))
	for _, block := range blocks {
		order = append(order, block.name)
	}
	ordered := OrderedBlockNames(container, layout, order)

	var b strings.Builder
	for _, name := range ordered {
		for _, block := range blocks {
			if block.name == name {
				b.WriteString(block.content)
				break
			}
		}
	}

	expanded := b.String()
	rest := preprocessLayoutBlocks(source[closeEnd:], layout)
	return source[:openStart] + expanded + rest
}

func extractNamedBlocks(inner string) []namedBlock {
	var blocks []namedBlock
	rest := inner
	for {
		name, _, openEnd, ok := findBlockOpen(rest, 0)
		if !ok {
			break
		}
		closeStart, closeEnd, ok := findMatchingBlockClose(rest, openEnd)
		if !ok {
			break
		}
		blocks = append(blocks, namedBlock{
			name:    name,
			content: rest[openEnd:closeStart],
		})
		rest = rest[closeEnd:]
		if strings.TrimSpace(rest) == "" {
			break
		}
	}
	return blocks
}

func findLayoutBlocksOpen(source string, from int) (container string, openStart, openEnd int, ok bool) {
	rest := source[from:]
	idx := strings.Index(rest, "{{")
	if idx < 0 {
		return "", 0, 0, false
	}
	loc := layoutBlocksOpenPattern.FindStringSubmatchIndex(rest[idx:])
	if loc == nil || loc[0] != 0 {
		next := from + idx + 2
		if next >= len(source) {
			return "", 0, 0, false
		}
		return findLayoutBlocksOpen(source, next)
	}
	match := layoutBlocksOpenPattern.FindStringSubmatch(rest[idx:])
	openStart = from + idx
	openEnd = openStart + loc[1]
	return match[1], openStart, openEnd, true
}

func findMatchingLayoutBlocksClose(source string, from int) (closeStart, closeEnd int, ok bool) {
	depth := 1
	i := from
	for i < len(source) && depth > 0 {
		nextCloseRel := strings.Index(source[i:], layoutBlocksCloseTag)
		if nextCloseRel < 0 {
			return 0, 0, false
		}
		nextClose := i + nextCloseRel

		nextOpenRel := strings.Index(source[i:nextClose], "{{")
		if nextOpenRel < 0 {
			depth--
			if depth == 0 {
				closeStart = nextClose
				closeEnd = nextClose + len(layoutBlocksCloseTag)
				return closeStart, closeEnd, true
			}
			i = nextClose + len(layoutBlocksCloseTag)
			continue
		}
		nextOpen := i + nextOpenRel

		if isLayoutBlocksOpenAt(source, nextOpen) {
			_, _, openEnd, openOK := findLayoutBlocksOpen(source, nextOpen)
			if !openOK {
				return 0, 0, false
			}
			depth++
			i = openEnd
			continue
		}

		i = skipBlockScannerAction(source, nextOpen)
	}
	return 0, 0, false
}

func findBlockOpen(source string, from int) (name string, openStart, openEnd int, ok bool) {
	rest := source[from:]
	idx := strings.Index(rest, "{{")
	if idx < 0 {
		return "", 0, 0, false
	}
	loc := blockOpenPattern.FindStringSubmatchIndex(rest[idx:])
	if loc == nil || loc[0] != 0 {
		next := from + idx + 2
		if next >= len(source) {
			return "", 0, 0, false
		}
		return findBlockOpen(source, next)
	}
	match := blockOpenPattern.FindStringSubmatch(rest[idx:])
	openStart = from + idx
	openEnd = openStart + loc[1]
	return match[1], openStart, openEnd, true
}

func findMatchingBlockClose(source string, from int) (closeStart, closeEnd int, ok bool) {
	return findMatchingTaggedClose(source, from, blockCloseTag, isBlockOpenAt, findBlockOpen)
}

type taggedOpenFinder func(source string, from int) (string, int, int, bool)

func findMatchingTaggedClose(
	source string,
	from int,
	closeTag string,
	isOpenAt func(string, int) bool,
	findOpen taggedOpenFinder,
) (closeStart, closeEnd int, ok bool) {
	depth := 1
	i := from
	for i < len(source) && depth > 0 {
		nextCloseRel := strings.Index(source[i:], closeTag)
		if nextCloseRel < 0 {
			return 0, 0, false
		}
		nextClose := i + nextCloseRel

		nextOpenRel := strings.Index(source[i:nextClose], "{{")
		if nextOpenRel < 0 {
			depth--
			if depth == 0 {
				closeStart = nextClose
				closeEnd = nextClose + len(closeTag)
				return closeStart, closeEnd, true
			}
			i = nextClose + len(closeTag)
			continue
		}
		nextOpen := i + nextOpenRel

		if isOpenAt(source, nextOpen) {
			_, _, openEnd, openOK := findOpen(source, nextOpen)
			if !openOK {
				return 0, 0, false
			}
			depth++
			i = openEnd
			continue
		}

		i = skipBlockScannerAction(source, nextOpen)
	}
	return 0, 0, false
}

func skipBlockScannerAction(source string, idx int) int {
	if idx < 0 || idx >= len(source) || !strings.HasPrefix(source[idx:], "{{") {
		return idx + 2
	}
	for _, prefix := range []string{blockCloseTag, layoutBlocksCloseTag, slotContainerCloseTag} {
		if strings.HasPrefix(source[idx:], prefix) {
			return idx + len(prefix)
		}
	}
	end := strings.Index(source[idx:], "}}")
	if end < 0 {
		return len(source)
	}
	return idx + end + 2
}

func isLayoutBlocksOpenAt(source string, idx int) bool {
	if idx < 0 || idx >= len(source) {
		return false
	}
	loc := layoutBlocksOpenPattern.FindStringSubmatchIndex(source[idx:])
	return loc != nil && loc[0] == 0
}

func isBlockOpenAt(source string, idx int) bool {
	if idx < 0 || idx >= len(source) {
		return false
	}
	loc := blockOpenPattern.FindStringSubmatchIndex(source[idx:])
	return loc != nil && loc[0] == 0
}
