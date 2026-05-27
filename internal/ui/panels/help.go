package panels

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/chaitanyak/klens/internal/ui/styles"
)

// RenderHelpInline formats a list of key-binding hints in the unified k9s
// style used across the TUI: muted brackets around a bold-primary key,
// immediately followed by the *rest* of the description (the bracketed
// letter visually replaces the leading character of the word — e.g.
// `[y]aml`, `[e]dit`, `[d]elete`). Items joined by a single space.
func RenderHelpInline(items []HelpItem) string {
	parts := make([]string, 0, len(items))
	for _, h := range items {
		bracket := styles.HelpBracket.Render("[") + styles.HelpKey.Render(h.Key) + styles.HelpBracket.Render("]")
		if h.Desc == "" {
			parts = append(parts, bracket)
			continue
		}
		parts = append(parts, fmt.Sprintf("%s%s", bracket, styles.HelpDesc.Render(trimKeyPrefix(h.Key, h.Desc))))
	}
	return strings.Join(parts, " ")
}

// trimKeyPrefix returns desc with its first rune dropped when that rune
// matches the single-character key (case-insensitive). Multi-character
// keys (`enter`, `shift+f`, `↑↓`) leave desc untouched.
func trimKeyPrefix(key, desc string) string {
	if desc == "" {
		return desc
	}
	keyRunes := []rune(key)
	if len(keyRunes) != 1 {
		return desc
	}
	descRunes := []rune(desc)
	if unicode.ToLower(keyRunes[0]) != unicode.ToLower(descRunes[0]) {
		return desc
	}
	return string(descRunes[1:])
}
