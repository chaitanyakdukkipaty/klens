package widgets

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/pmezard/go-difflib/difflib"
	appstyles "github.com/chaitanyak/klens/internal/ui/styles"
)

var (
	addLine    = lipgloss.NewStyle().Foreground(appstyles.ColorRunning)
	removeLine = lipgloss.NewStyle().Foreground(appstyles.ColorFailed)
	ctxLine    = lipgloss.NewStyle().Foreground(appstyles.ColorDiffContext)
	hunkHeader = lipgloss.NewStyle().Foreground(appstyles.ColorSucceeded).Bold(true)
)

// DiffView renders a colored unified diff between original and modified YAML.
func DiffView(original, modified string, width int) string {
	if original == modified {
		return appstyles.Muted.Render("  No changes")
	}

	diff := difflib.UnifiedDiff{
		A:        difflib.SplitLines(original),
		B:        difflib.SplitLines(modified),
		FromFile: "original",
		ToFile:   "modified",
		Context:  3,
	}
	text, err := difflib.GetUnifiedDiffString(diff)
	if err != nil {
		return fmt.Sprintf("diff error: %v", err)
	}

	var lines []string
	for _, line := range strings.Split(text, "\n") {
		switch {
		case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"):
			lines = append(lines, appstyles.Muted.Render(line))
		case strings.HasPrefix(line, "@@"):
			lines = append(lines, hunkHeader.Render(line))
		case strings.HasPrefix(line, "+"):
			lines = append(lines, addLine.Render(line))
		case strings.HasPrefix(line, "-"):
			lines = append(lines, removeLine.Render(line))
		default:
			lines = append(lines, ctxLine.Render(line))
		}
	}
	return strings.Join(lines, "\n")
}
