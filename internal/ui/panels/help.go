package panels

import (
	"fmt"
	"strings"
	"unicode"

	"charm.land/lipgloss/v2"
	"github.com/chaitanyak/klens/internal/ui/styles"
)

// RenderHelpInline formats a list of key-binding hints in the unified k9s
// style used across the TUI: muted brackets around a bold-primary key,
// immediately followed by the *rest* of the description (the bracketed
// letter visually replaces the leading character of the word — e.g.
// `[y]aml`, `[e]dit`, `[d]elete`, `[ctrl+r]efresh`, `[ctrl+c/q]uit`).
// Items joined by a single space.
func RenderHelpInline(items []HelpItem) string {
	return strings.Join(renderHelpParts(items), " ")
}

// RenderHelpInlineCentered renders the same hints as RenderHelpInline but
// joins items with a fixed inter-item gap and centers the resulting block
// inside `width`, so wide terminals show symmetric empty space on either
// side rather than enormous gaps between items. Falls back to single-space
// joining (and no centering) when the line wouldn't fit anyway.
func RenderHelpInlineCentered(items []HelpItem, width int) string {
	parts := renderHelpParts(items)
	if len(parts) == 0 {
		return ""
	}
	const gap = "   " // 3 spaces between items
	line := strings.Join(parts, gap)
	if width <= 0 {
		return line
	}
	lineW := lipgloss.Width(line)
	if lineW >= width {
		// No room to center — also no room for the wide gap; try a tighter join.
		tight := strings.Join(parts, " ")
		if lipgloss.Width(tight) >= width {
			return tight
		}
		line = tight
		lineW = lipgloss.Width(line)
	}
	leftPad := (width - lineW) / 2
	if leftPad <= 0 {
		return line
	}
	return strings.Repeat(" ", leftPad) + line
}

// renderHelpParts returns each HelpItem as a fully styled string, ready to be
// joined by the caller with whatever separator it wants.
func renderHelpParts(items []HelpItem) []string {
	parts := make([]string, 0, len(items))
	for _, h := range items {
		bracket := styles.HelpBracket.Render("[") + styles.HelpKey.Render(h.Key) + styles.HelpBracket.Render("]")
		if h.Desc == "" {
			parts = append(parts, bracket)
			continue
		}
		parts = append(parts, fmt.Sprintf("%s%s", bracket, styles.HelpDesc.Render(trimKeyPrefix(h.Key, h.Desc))))
	}
	return parts
}

// trimKeyPrefix returns desc with its first rune dropped when any letter
// rune in key matches it (case-insensitive). Handles compound keys like
// `ctrl+r` + `refresh` → `efresh`, `ctrl+c/q` + `quit` → `uit`. Keys with
// no letter rune matching desc's first letter leave desc untouched.
func trimKeyPrefix(key, desc string) string {
	if desc == "" {
		return desc
	}
	descRunes := []rune(desc)
	first := unicode.ToLower(descRunes[0])
	if !unicode.IsLetter(first) {
		return desc
	}
	for _, r := range key {
		if unicode.IsLetter(r) && unicode.ToLower(r) == first {
			return string(descRunes[1:])
		}
	}
	return desc
}
