package panels

import (
	"fmt"
	"strings"

	"github.com/chaitanyak/klens/internal/ui/styles"
)

// RenderHelpInline formats a list of key-binding hints in the unified style
// used across the TUI: bold-primary key followed by muted description, items
// joined by a muted middle dot. Mirrors the status bar's rendering so the eye
// can pick out keys consistently in any panel's help footer.
func RenderHelpInline(items []HelpItem) string {
	parts := make([]string, 0, len(items))
	for _, h := range items {
		if h.Desc == "" {
			parts = append(parts, styles.HelpKey.Render(h.Key))
			continue
		}
		parts = append(parts, fmt.Sprintf("%s %s",
			styles.HelpKey.Render(h.Key),
			styles.HelpDesc.Render(h.Desc),
		))
	}
	return strings.Join(parts, styles.HelpDesc.Render("  ·  "))
}
