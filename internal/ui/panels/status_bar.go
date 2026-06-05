package panels

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/chaitanyak/klens/internal/ui/styles"
)

type StatusBar struct {
	width      int
	message    string // transient message (errors, info)
	help       []HelpItem
	activeKind string
}

type HelpItem struct {
	Key  string
	Desc string
}

// defaultHelp is the pre-cluster-ready footer. View-mode actions (y/l/x/m/d)
// are not listed here or in any footer — the tab bar shows them with their
// keys.
var defaultHelp = []HelpItem{
	{Key: "↑↓/jk", Desc: "navigate"},
	{Key: "enter", Desc: "focus"},
	{Key: "/", Desc: "filter"},
	{Key: "q", Desc: "quit"},
}

func NewStatusBar(width int) StatusBar {
	return StatusBar{width: width, help: defaultHelp}
}

func (s StatusBar) SetWidth(w int) StatusBar {
	s.width = w
	return s
}

func (s StatusBar) SetMessage(msg string) StatusBar {
	s.message = msg
	return s
}

// Message returns the current transient status-bar message. Empty when no
// message is set. Exposed so tests can assert on the message text without
// rendering the bar.
func (s StatusBar) Message() string { return s.message }

func (s StatusBar) SetHelp(items []HelpItem) StatusBar {
	s.help = items
	return s
}

func (s StatusBar) SetActiveKind(kind string) StatusBar {
	s.activeKind = kind
	return s
}

func (s StatusBar) View() string {
	if s.message != "" {
		msg := styles.Warning.Render("  " + s.message)
		padding := max(0, s.width-lipgloss.Width(msg))
		return styles.StatusBar.Width(s.width).Render(msg + strings.Repeat(" ", padding))
	}

	help := s.help
	if s.activeKind == "Pod" {
		hint := HelpItem{Key: "space", Desc: "mark"}
		extended := make([]HelpItem, 0, len(help)+1)
		extended = append(extended, help[0])
		extended = append(extended, hint)
		extended = append(extended, help[1:]...)
		help = extended
	}

	// styles.StatusBar has Padding(0, 1), so the content area is s.width-2.
	inner := max(1, s.width-2)
	line := RenderHelpInlineCentered(help, inner)
	return styles.StatusBar.Width(s.width).Render(line)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
