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

var defaultHelp = []HelpItem{
	{Key: "↑↓/jk", Desc: "navigate"},
	{Key: "enter", Desc: "focus"},
	{Key: "/", Desc: "filter"},
	{Key: "y", Desc: "yaml"},
	{Key: "l", Desc: "logs"},
	{Key: "t", Desc: "topology"},
	{Key: "m", Desc: "metrics"},
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
